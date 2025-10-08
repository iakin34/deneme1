package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type UserState string

const (
	StateNone             UserState = "none"
	StateAwaitingKey      UserState = "awaiting_api_key"
	StateAwaitingSecret   UserState = "awaiting_secret"
	StateAwaitingPasskey  UserState = "awaiting_passkey"
	StateAwaitingMargin   UserState = "awaiting_margin"
	StateAwaitingLeverage UserState = "awaiting_leverage"
	StateComplete         UserState = "complete"
)

type UserData struct {
	UserID        int64     `json:"user_id"`
	Username      string    `json:"username"`
	BitgetAPIKey  string    `json:"bitget_api_key"`
	BitgetSecret  string    `json:"bitget_secret"`
	BitgetPasskey string    `json:"bitget_passkey"`
	MarginUSDT    float64   `json:"margin_usdt"`
	Leverage      int       `json:"leverage"`
	IsActive      bool      `json:"is_active"`
	State         UserState `json:"current_state"`
	CreatedAt     string    `json:"created_at"`
	UpdatedAt     string    `json:"updated_at"`
}

type PositionInfo struct {
	UserID       int64     `json:"user_id"`
	Symbol       string    `json:"symbol"`
	OrderID      string    `json:"order_id"`
	OpenPrice    float64   `json:"open_price"`
	Size         float64   `json:"size"`
	MarginUSDT   float64   `json:"margin_usdt"`
	Leverage     int       `json:"leverage"`
	OpenTime     time.Time `json:"open_time"`
	LastReminder time.Time `json:"last_reminder"`
}

var (
	activePositions = make(map[string]*PositionInfo)
	positionsMutex  sync.RWMutex
)

const positionsFile = "active_positions.json"

type BotDatabase struct {
	Users map[int64]*UserData `json:"users"`
	mutex sync.RWMutex
}

type TelegramBot struct {
	bot                 *tgbotapi.BotAPI
	database            *BotDatabase
	dbFile              string
	encryptionKey       []byte
	lastProcessedSymbol string
}

func generateEncryptionKey() ([]byte, error) {
	envKey := os.Getenv("BOT_ENCRYPTION_KEY")
	if envKey == "" {
		return nil, fmt.Errorf("BOT_ENCRYPTION_KEY environment variable is required for secure credential storage")
	}

	if key, err := base64.StdEncoding.DecodeString(envKey); err == nil && len(key) == 32 {
		return key, nil
	}

	hash := sha256.Sum256([]byte(envKey))
	return hash[:], nil
}

func (tb *TelegramBot) encryptSensitiveData(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}

	block, err := aes.NewCipher(tb.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %v", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %v", err)
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func (tb *TelegramBot) decryptSensitiveData(ciphertext string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}

	data, err := base64.StdEncoding.DecodeString(ciphertext)
	if err != nil {
		return "", fmt.Errorf("failed to decode base64: %v", err)
	}

	block, err := aes.NewCipher(tb.encryptionKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %v", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %v", err)
	}

	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}

	nonce, ciphertextBytes := data[:nonceSize], data[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertextBytes, nil)
	if err != nil {
		return "", fmt.Errorf("failed to decrypt: %v", err)
	}

	return string(plaintext), nil
}

func NewTelegramBot(token string) (*TelegramBot, error) {
	bot, err := tgbotapi.NewBotAPI(token)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %v", err)
	}

	encryptionKey, err := generateEncryptionKey()
	if err != nil {
		return nil, fmt.Errorf("failed to setup encryption: %v", err)
	}

	botInstance := &TelegramBot{
		bot:           bot,
		dbFile:        "bot_users.json",
		encryptionKey: encryptionKey,
		database: &BotDatabase{
			Users: make(map[int64]*UserData),
		},
	}

	if err := botInstance.loadDatabase(); err != nil {
		log.Printf("Warning: Could not load database: %v", err)
	}

	loadActivePositions()
	go botInstance.startFileWatcher()
	go botInstance.startPositionReminders()

	return botInstance, nil
}

func (tb *TelegramBot) saveDatabase() error {
	tb.database.mutex.Lock()
	defer tb.database.mutex.Unlock()

	data, err := json.MarshalIndent(tb.database, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal database: %v", err)
	}

	return os.WriteFile(tb.dbFile, data, 0644)
}

func (tb *TelegramBot) loadDatabase() error {
	if _, err := os.Stat(tb.dbFile); os.IsNotExist(err) {
		return nil
	}

	data, err := os.ReadFile(tb.dbFile)
	if err != nil {
		return fmt.Errorf("failed to read database file: %v", err)
	}

	tb.database.mutex.Lock()
	defer tb.database.mutex.Unlock()

	return json.Unmarshal(data, tb.database)
}

func (tb *TelegramBot) getUser(userID int64) (*UserData, bool) {
	tb.database.mutex.RLock()
	defer tb.database.mutex.RUnlock()

	encryptedUser, exists := tb.database.Users[userID]
	if !exists {
		return nil, false
	}

	user := *encryptedUser

	if encryptedUser.BitgetAPIKey != "" {
		decrypted, err := tb.decryptSensitiveData(encryptedUser.BitgetAPIKey)
		if err != nil {
			log.Printf("Warning: Failed to decrypt API key for user %d: %v", userID, err)
			user.BitgetAPIKey = ""
		} else {
			user.BitgetAPIKey = decrypted
		}
	}

	if encryptedUser.BitgetSecret != "" {
		decrypted, err := tb.decryptSensitiveData(encryptedUser.BitgetSecret)
		if err != nil {
			log.Printf("Warning: Failed to decrypt secret for user %d: %v", userID, err)
			user.BitgetSecret = ""
		} else {
			user.BitgetSecret = decrypted
		}
	}

	if encryptedUser.BitgetPasskey != "" {
		decrypted, err := tb.decryptSensitiveData(encryptedUser.BitgetPasskey)
		if err != nil {
			log.Printf("Warning: Failed to decrypt passkey for user %d: %v", userID, err)
			user.BitgetPasskey = ""
		} else {
			user.BitgetPasskey = decrypted
		}
	}

	return &user, true
}

func (tb *TelegramBot) saveUser(user *UserData) error {
	tb.database.mutex.Lock()
	defer tb.database.mutex.Unlock()

	user.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if user.CreatedAt == "" {
		user.CreatedAt = user.UpdatedAt
	}

	encryptedUser := *user

	if user.BitgetAPIKey != "" {
		encrypted, err := tb.encryptSensitiveData(user.BitgetAPIKey)
		if err != nil {
			return fmt.Errorf("failed to encrypt API key: %v", err)
		}
		encryptedUser.BitgetAPIKey = encrypted
	}

	if user.BitgetSecret != "" {
		encrypted, err := tb.encryptSensitiveData(user.BitgetSecret)
		if err != nil {
			return fmt.Errorf("failed to encrypt secret: %v", err)
		}
		encryptedUser.BitgetSecret = encrypted
	}

	if user.BitgetPasskey != "" {
		encrypted, err := tb.encryptSensitiveData(user.BitgetPasskey)
		if err != nil {
			return fmt.Errorf("failed to encrypt passkey: %v", err)
		}
		encryptedUser.BitgetPasskey = encrypted
	}

	tb.database.Users[user.UserID] = &encryptedUser
	
	data, err := json.MarshalIndent(tb.database, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal database: %v", err)
	}

	return os.WriteFile(tb.dbFile, data, 0644)
}

func (tb *TelegramBot) getAllActiveUsers() []*UserData {
	tb.database.mutex.RLock()
	defer tb.database.mutex.RUnlock()

	var activeUsers []*UserData
	for _, encryptedUser := range tb.database.Users {
		if encryptedUser.IsActive {
			user := *encryptedUser

			if encryptedUser.BitgetAPIKey != "" {
				if decrypted, err := tb.decryptSensitiveData(encryptedUser.BitgetAPIKey); err == nil {
					user.BitgetAPIKey = decrypted
				}
			}

			if encryptedUser.BitgetSecret != "" {
				if decrypted, err := tb.decryptSensitiveData(encryptedUser.BitgetSecret); err == nil {
					user.BitgetSecret = decrypted
				}
			}

			if encryptedUser.BitgetPasskey != "" {
				if decrypted, err := tb.decryptSensitiveData(encryptedUser.BitgetPasskey); err == nil {
					user.BitgetPasskey = decrypted
				}
			}

			activeUsers = append(activeUsers, &user)
		}
	}
	return activeUsers
}

func (tb *TelegramBot) startFileWatcher() {
	log.Printf("🔧 Starting file watcher...")

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("❌ Failed to create file watcher: %v", err)
		return
	}
	defer watcher.Close()

	upbitFile := "upbit_new.json"

	if _, err := os.Stat(upbitFile); os.IsNotExist(err) {
		log.Printf("❌ File %s does not exist!", upbitFile)
		return
	}

	err = watcher.Add(upbitFile)
	if err != nil {
		log.Printf("❌ Failed to watch %s: %v", upbitFile, err)
		return
	}

	log.Printf("👁️  Successfully watching %s for new UPBIT listings...", upbitFile)

	if latestSymbol := tb.getLatestDetectedSymbol(); latestSymbol != "" {
		tb.lastProcessedSymbol = latestSymbol
		log.Printf("🔄 Current latest symbol: %s", latestSymbol)
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				log.Printf("🚨 FILE CHANGE EVENT - Processing file change: %s", event.Name)
				tb.processUpbitFile()
			}
		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			log.Printf("❌ File watcher error: %v", err)
		}
	}
}

func (tb *TelegramBot) getLatestDetectedSymbol() string {
	data, err := os.ReadFile("upbit_new.json")
	if err != nil {
		return ""
	}

	var upbitData ListingsData
	if err := json.Unmarshal(data, &upbitData); err != nil {
		return ""
	}

	if len(upbitData.Listings) == 0 {
		return ""
	}

	return upbitData.Listings[0].Symbol
}

func (tb *TelegramBot) processUpbitFile() {
	latestSymbol := tb.getLatestDetectedSymbol()
	if latestSymbol == "" {
		return
	}

	if latestSymbol == tb.lastProcessedSymbol {
		log.Printf("🔄 Symbol %s already processed, skipping", latestSymbol)
		return
	}

	tb.lastProcessedSymbol = latestSymbol
	log.Printf("🚨 NEW UPBIT LISTING DETECTED: %s", latestSymbol)

	activeUsers := tb.getAllActiveUsers()
	if len(activeUsers) == 0 {
		log.Printf("⚠️  No active users found for auto-trading")
		return
	}

	log.Printf("📊 Triggering auto-trading for %d users on symbol: %s", len(activeUsers), latestSymbol)

	for _, user := range activeUsers {
		go tb.executeAutoTrade(user, latestSymbol)
	}
}

func (tb *TelegramBot) executeAutoTrade(user *UserData, symbol string) {
	log.Printf("🤖 Auto-trading for user %d (%s) on symbol: %s", user.UserID, user.Username, symbol)

	if user.BitgetAPIKey == "" || user.BitgetSecret == "" || user.BitgetPasskey == "" {
		log.Printf("⚠️  User %d missing API credentials, skipping auto-trade", user.UserID)
		tb.sendMessage(user.UserID, fmt.Sprintf("🚫 Auto-trade failed for %s: Missing API credentials. Please /setup first.", symbol))
		return
	}

	if user.MarginUSDT <= 0 {
		log.Printf("⚠️  User %d has invalid margin amount: %f", user.UserID, user.MarginUSDT)
		tb.sendMessage(user.UserID, fmt.Sprintf("🚫 Auto-trade failed for %s: Invalid margin amount. Please /setup first.", symbol))
		return
	}

	tradingSymbol := symbol + "USDT"
	bitgetAPI := NewBitgetAPI(user.BitgetAPIKey, user.BitgetSecret, user.BitgetPasskey)

	tb.sendMessage(user.UserID, fmt.Sprintf("🚀 Auto-trade triggered for %s\nMargin: %.2f USDT\nLeverage: %dx\nOpening long position...", tradingSymbol, user.MarginUSDT, user.Leverage))

	result, err := bitgetAPI.OpenLongPosition(tradingSymbol, user.MarginUSDT, user.Leverage)
	if err != nil {
		log.Printf("❌ Auto-trade failed for user %d on %s: %v", user.UserID, tradingSymbol, err)
		tb.sendMessage(user.UserID, fmt.Sprintf("❌ Auto-trade FAILED for %s: %v", tradingSymbol, err))
		return
	}

	log.Printf("✅ Auto-trade SUCCESS for user %d on %s", user.UserID, tradingSymbol)
	tb.sendPositionNotification(user.UserID, result)
}

func (tb *TelegramBot) sendMessage(chatID int64, text string) {
	msg := tgbotapi.NewMessage(chatID, text)
	_, err := tb.bot.Send(msg)
	if err != nil {
		log.Printf("Failed to send message to %d: %v", chatID, err)
	}
}

func (tb *TelegramBot) sendPositionNotification(chatID int64, orderResp *OrderResponse) {
	user, exists := tb.getUser(chatID)
	if !exists {
		return
	}

	api := NewBitgetAPI(user.BitgetAPIKey, user.BitgetSecret, user.BitgetPasskey)
	currentPrice, err := api.GetSymbolPrice(orderResp.Symbol)
	if err != nil {
		currentPrice = orderResp.OpenPrice
	}

	priceChange := currentPrice - orderResp.OpenPrice
	priceChangePercent := (priceChange / orderResp.OpenPrice) * 100
	usdPnL := priceChange * orderResp.Size
	usdPnLWithLeverage := usdPnL * float64(orderResp.Leverage)

	pnlIcon := "🔴"
	pnlColor := "📉"
	if usdPnLWithLeverage > 0 {
		pnlIcon = "🟢"
		pnlColor = "📈"
	} else if usdPnLWithLeverage == 0 {
		pnlIcon = "⚪"
		pnlColor = "➡️"
	}

	notificationMsg := fmt.Sprintf(`🎉 Pozisyon Açıldı!

💹 Sembol: %s
📊 Açılış Fiyatı: $%.4f
💰 Güncel Fiyat: $%.4f
📏 Pozisyon Boyutu: %.8f
⚖️ Kaldıraç: %dx
💵 Marjin: %.2f USDT

%s Fiyat Değişimi: %+.4f (%.2f%%)
%s P&L: %+.2f USDT

⏰ Sonraki hatırlatma: 5 dakika
Pozisyon ID: %s`,
		orderResp.Symbol,
		orderResp.OpenPrice,
		currentPrice,
		orderResp.Size,
		orderResp.Leverage,
		orderResp.MarginUSDT,
		pnlColor,
		priceChange,
		priceChangePercent,
		pnlIcon,
		usdPnLWithLeverage,
		orderResp.OrderID)

	closeButton := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("❌ %s Pozisyonunu Kapat", orderResp.Symbol), fmt.Sprintf("close_position_%s", orderResp.Symbol)),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🏠 Ana Menü", "main_menu"),
		),
	)

	msg := tgbotapi.NewMessage(chatID, notificationMsg)
	msg.ReplyMarkup = closeButton
	tb.bot.Send(msg)

	positionKey := fmt.Sprintf("%d_%s", chatID, orderResp.Symbol)
	positionsMutex.Lock()
	activePositions[positionKey] = &PositionInfo{
		UserID:       chatID,
		Symbol:       orderResp.Symbol,
		OrderID:      orderResp.OrderID,
		OpenPrice:    orderResp.OpenPrice,
		Size:         orderResp.Size,
		MarginUSDT:   orderResp.MarginUSDT,
		Leverage:     orderResp.Leverage,
		OpenTime:     time.Now(),
		LastReminder: time.Now(),
	}
	positionsMutex.Unlock()

	go saveActivePositions()
}

func (tb *TelegramBot) startPositionReminders() {
	log.Printf("⏰ Starting position reminder system...")

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()

		positionsMutex.Lock()
		for positionKey, position := range activePositions {
			if now.Sub(position.LastReminder) >= 5*time.Minute {
				positionsMutex.Unlock()
				tb.sendPositionReminder(position)
				positionsMutex.Lock()
				if pos, exists := activePositions[positionKey]; exists {
					pos.LastReminder = now
				}
			}
		}
		positionsMutex.Unlock()
	}
}

func (tb *TelegramBot) sendPositionReminder(position *PositionInfo) {
	user, exists := tb.getUser(position.UserID)
	if !exists {
		return
	}

	api := NewBitgetAPI(user.BitgetAPIKey, user.BitgetSecret, user.BitgetPasskey)
	currentPrice, _ := api.GetSymbolPrice(position.Symbol)

	duration := time.Since(position.OpenTime)
	priceChange := currentPrice - position.OpenPrice
	priceChangePercent := (priceChange / position.OpenPrice) * 100
	realPnL := priceChange * position.Size

	pnlIcon := "🔴"
	if realPnL > 0 {
		pnlIcon = "🟢"
	}

	reminderMsg := fmt.Sprintf(`⏰ Pozisyon Hatırlatması

💹 %s Pozisyonu Aktif

📊 Açılış: $%.4f
💰 Güncel: $%.4f
⏳ Süre: %s

Fiyat Değişimi: %+.4f (%.2f%%)
%s P&L: %+.2f USDT`,
		position.Symbol,
		position.OpenPrice,
		currentPrice,
		formatDuration(duration),
		priceChange,
		priceChangePercent,
		pnlIcon,
		realPnL)

	closeButton := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(fmt.Sprintf("❌ Kapat", ), fmt.Sprintf("close_position_%s", position.Symbol)),
		),
	)

	msg := tgbotapi.NewMessage(position.UserID, reminderMsg)
	msg.ReplyMarkup = closeButton
	tb.bot.Send(msg)
}

func formatDuration(d time.Duration) string {
	if d.Hours() >= 1 {
		return fmt.Sprintf("%.0fs %.0fd", d.Hours(), d.Minutes()-d.Hours()*60)
	}
	return fmt.Sprintf("%.0fd", d.Minutes())
}

func saveActivePositions() {
	positionsMutex.RLock()
	defer positionsMutex.RUnlock()

	file, err := os.Create(positionsFile)
	if err != nil {
		log.Printf("⚠️ Could not save positions: %v", err)
		return
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	encoder.Encode(activePositions)
}

func loadActivePositions() {
	file, err := os.Open(positionsFile)
	if err != nil {
		return
	}
	defer file.Close()

	var savedPositions map[string]*PositionInfo
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&savedPositions); err != nil {
		return
	}

	positionsMutex.Lock()
	activePositions = savedPositions
	positionsMutex.Unlock()

	log.Printf("📂 Loaded %d active positions from file", len(savedPositions))
}

func (tb *TelegramBot) Start() {
	log.Printf("🤖 Telegram Bot starting...")

	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60

	updates := tb.bot.GetUpdatesChan(updateConfig)

	for update := range updates {
		if update.Message != nil {
			tb.handleMessage(update)
		} else if update.CallbackQuery != nil {
			tb.handleCallbackQuery(update.CallbackQuery)
		}
	}
}

func (tb *TelegramBot) handleMessage(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	chatID := update.Message.Chat.ID
	userID := update.Message.From.ID
	username := update.Message.From.UserName
	text := update.Message.Text

	if update.Message.IsCommand() {
		switch update.Message.Command() {
		case "start":
			tb.handleStart(chatID, userID, username)
		case "setup":
			tb.handleSetup(chatID, userID, username)
		case "status":
			msg := tgbotapi.NewMessage(chatID, "🤖 Bot aktif olarak çalışıyor!")
			tb.bot.Send(msg)
		default:
			msg := tgbotapi.NewMessage(chatID, "❓ Bilinmeyen komut. /start komutunu deneyin.")
			tb.bot.Send(msg)
		}
		return
	}

	tb.handleSetupProcess(chatID, userID, text)
}

func (tb *TelegramBot) handleStart(chatID int64, userID int64, username string) {
	welcomeMsg := fmt.Sprintf(`👋 Hoşgeldin @%s!

🚀 Upbit-Bitget Otomatik Trading Botu

Bu bot, Upbit'te listelenen yeni coinleri otomatik olarak Bitget'te long position ile alır.

Başlamak için /setup komutunu kullanın.`, username)

	msg := tgbotapi.NewMessage(chatID, welcomeMsg)
	tb.bot.Send(msg)
}

func (tb *TelegramBot) handleSetup(chatID int64, userID int64, username string) {
	setupMsg := `🔧 Bitget API Setup

1️⃣ Bitget API Key'inizi gönderin`

	msg := tgbotapi.NewMessage(chatID, setupMsg)
	tb.bot.Send(msg)

	user, exists := tb.getUser(userID)
	if !exists {
		user = &UserData{
			UserID:   userID,
			Username: username,
			IsActive: false,
			State:    StateAwaitingKey,
		}
	} else {
		user.State = StateAwaitingKey
	}

	tb.saveUser(user)
}

func (tb *TelegramBot) handleSetupProcess(chatID int64, userID int64, text string) {
	user, exists := tb.getUser(userID)
	if !exists || user.State == StateNone {
		return
	}

	switch user.State {
	case StateAwaitingKey:
		user.BitgetAPIKey = strings.TrimSpace(text)
		user.State = StateAwaitingSecret
		tb.saveUser(user)
		msg := tgbotapi.NewMessage(chatID, "✅ API Key alındı!\n\n2️⃣ Secret Key'inizi gönderin")
		tb.bot.Send(msg)

	case StateAwaitingSecret:
		user.BitgetSecret = strings.TrimSpace(text)
		user.State = StateAwaitingPasskey
		tb.saveUser(user)
		msg := tgbotapi.NewMessage(chatID, "✅ Secret Key alındı!\n\n3️⃣ Passphrase'inizi gönderin")
		tb.bot.Send(msg)

	case StateAwaitingPasskey:
		user.BitgetPasskey = strings.TrimSpace(text)
		user.State = StateAwaitingMargin
		tb.saveUser(user)
		msg := tgbotapi.NewMessage(chatID, "✅ Passphrase alındı!\n\n4️⃣ Margin tutarını USDT olarak gönderin (örn: 100)")
		tb.bot.Send(msg)

	case StateAwaitingMargin:
		margin, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
		if err != nil || margin <= 0 {
			msg := tgbotapi.NewMessage(chatID, "❌ Geçersiz tutar! Pozitif bir sayı girin")
			tb.bot.Send(msg)
			return
		}
		user.MarginUSDT = margin
		user.State = StateAwaitingLeverage
		tb.saveUser(user)
		msg := tgbotapi.NewMessage(chatID, "✅ Margin alındı!\n\n5️⃣ Leverage değerini gönderin (örn: 10)")
		tb.bot.Send(msg)

	case StateAwaitingLeverage:
		leverage, err := strconv.Atoi(strings.TrimSpace(text))
		if err != nil || leverage < 1 || leverage > 125 {
			msg := tgbotapi.NewMessage(chatID, "❌ Geçersiz leverage! 1-125 arası bir sayı girin")
			tb.bot.Send(msg)
			return
		}
		user.Leverage = leverage
		user.State = StateComplete
		user.IsActive = true
		tb.saveUser(user)

		successMsg := fmt.Sprintf(`✅ Setup Tamamlandı!

👤 Kullanıcı: @%s
💰 Margin: %.2f USDT
📈 Leverage: %dx
🎯 Durum: Aktif

Bot artık Upbit'te yeni listelenen coinleri otomatik olarak Bitget'te long position ile alacak.`, user.Username, user.MarginUSDT, user.Leverage)

		msg := tgbotapi.NewMessage(chatID, successMsg)
		tb.bot.Send(msg)
	}
}

func (tb *TelegramBot) handleCallbackQuery(callback *tgbotapi.CallbackQuery) {
	callbackConfig := tgbotapi.NewCallback(callback.ID, "")
	tb.bot.Request(callbackConfig)

	chatID := callback.Message.Chat.ID
	userID := callback.From.ID
	data := callback.Data

	if data == "main_menu" {
		tb.handleStart(chatID, userID, callback.From.UserName)
	} else if strings.HasPrefix(data, "close_position_") {
		symbol := strings.TrimPrefix(data, "close_position_")
		tb.handleClosePosition(chatID, userID, symbol)
	}
}

func (tb *TelegramBot) handleClosePosition(chatID int64, userID int64, symbol string) {
	user, exists := tb.getUser(userID)
	if !exists || user.BitgetAPIKey == "" {
		tb.sendMessage(chatID, "❌ API ayarlarınızı yapmadınız.")
		return
	}

	tb.sendMessage(chatID, fmt.Sprintf("🚨 %s pozisyonu kapatılıyor...", symbol))

	api := NewBitgetAPI(user.BitgetAPIKey, user.BitgetSecret, user.BitgetPasskey)
	result, err := api.FlashClosePosition(symbol, "long")
	if err != nil {
		tb.sendMessage(chatID, fmt.Sprintf("❌ Pozisyon kapatılamadı: %v", err))
		return
	}

	positionKey := fmt.Sprintf("%d_%s", chatID, symbol)
	positionsMutex.Lock()
	delete(activePositions, positionKey)
	positionsMutex.Unlock()

	go saveActivePositions()

	tb.sendMessage(chatID, fmt.Sprintf("✅ %s pozisyonu kapatıldı!\nPozisyon ID: %s", symbol, result.OrderID))
}
