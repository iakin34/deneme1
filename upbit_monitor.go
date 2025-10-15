package main

import (
        "encoding/json"
        "fmt"
        "io"
        "log"
        "net/http"
        "net/url"
        "os"
        "regexp"
        "sync"
        "time"

        "golang.org/x/net/proxy"
)

type UpbitAPIResponse struct {
        Success bool       `json:"success"`
        Data    UpbitData2 `json:"data"`
}

type UpbitData2 struct {
        Notices []Announcement `json:"notices"`
}

type Announcement struct {
        ID    int    `json:"id"`
        Title string `json:"title"`
}

type ListingEntry struct {
        Symbol     string `json:"symbol"`
        Timestamp  string `json:"timestamp"`
        DetectedAt string `json:"detected_at"`
}

type ListingsData struct {
        Listings []ListingEntry `json:"listings"`
}

// Type aliases for compatibility with telegram_bot.go
type CoinDetection = ListingEntry
type UpbitDetection = ListingEntry
type UpbitData = ListingsData

type TradeExecutionLog struct {
        Ticker               string                 `json:"ticker"`
        UpbitDetectedAt      string                 `json:"upbit_detected_at"`
        SavedToFileAt        string                 `json:"saved_to_file_at"`
        UserID               int64                  `json:"user_id"`
        BitgetOrderSentAt    string                 `json:"bitget_order_sent_at"`
        BitgetOrderConfirmed string                 `json:"bitget_order_confirmed_at"`
        LatencyBreakdown     map[string]interface{} `json:"latency_breakdown"`
}

type UpbitMonitor struct {
        apiURL          string
        proxies         []string
        tickerRegex     *regexp.Regexp
        cachedTickers   map[string]bool
        cachedETag      string
        proxyIndex      int
        mu              sync.Mutex
        jsonFile        string
        onNewListing    func(symbol string) // Callback for new listings
        executionLogFile string
        currentLogEntry  *TradeExecutionLog
        logMu            sync.Mutex
}

func NewUpbitMonitor(onNewListing func(string)) *UpbitMonitor {
        var proxies []string
        
        for i := 1; i <= 11; i++ {
                proxyEnv := os.Getenv(fmt.Sprintf("UPBIT_PROXY_%d", i))
                if proxyEnv != "" {
                        proxies = append(proxies, proxyEnv)
                }
        }

        if len(proxies) == 0 {
                proxies = []string{
                        "socks5://doproxy1:DigitalOcean55@143.198.221.194:1080",
                        "socks5://doproxy2:DigitalOcean55@159.223.68.49:1080",
                        "socks5://doproxy3:DigitalOcean55@104.248.147.230:1080",
                }
                log.Printf("⚠️ UPBIT_PROXY environment variables not set, using %d default proxies", len(proxies))
        } else {
                log.Printf("✅ Loaded %d proxies from environment variables", len(proxies))
        }

        return &UpbitMonitor{
                apiURL:           "https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=20&category=overall",
                proxies:          proxies,
                tickerRegex:      regexp.MustCompile(`\(([A-Z]{2,6})\)`), // Only 2-6 uppercase letters (valid tickers)
                cachedTickers:    make(map[string]bool),
                proxyIndex:       0,
                jsonFile:         "upbit_new.json",
                executionLogFile: "trade_execution_log.json",
                onNewListing:     onNewListing,
        }
}

func (um *UpbitMonitor) createProxyClient(proxyURL string) (*http.Client, error) {
        parsedURL, err := url.Parse(proxyURL)
        if err != nil {
                return nil, fmt.Errorf("proxy URL'si ayrıştırılamadı: %w", err)
        }

        dialer, err := proxy.FromURL(parsedURL, proxy.Direct)
        if err != nil {
                return nil, fmt.Errorf("proxy dialer oluşturulamadı: %w", err)
        }

        transport := &http.Transport{
                Dial: dialer.Dial,
        }

        client := &http.Client{
                Transport: transport,
                Timeout:   10 * time.Second,
        }

        return client, nil
}

func (um *UpbitMonitor) loadExistingData() error {
        if _, err := os.Stat(um.jsonFile); os.IsNotExist(err) {
                return nil
        }

        data, err := os.ReadFile(um.jsonFile)
        if err != nil {
                return fmt.Errorf("error reading JSON file: %v", err)
        }

        var listingsData ListingsData
        if err := json.Unmarshal(data, &listingsData); err != nil {
                return fmt.Errorf("error parsing JSON: %v", err)
        }

        for _, entry := range listingsData.Listings {
                um.cachedTickers[entry.Symbol] = true
        }

        log.Printf("Loaded %d existing symbols from %s", len(um.cachedTickers), um.jsonFile)
        return nil
}

func (um *UpbitMonitor) saveToJSON(symbol string) error {
        var data ListingsData
        if _, err := os.Stat(um.jsonFile); err == nil {
                fileData, err := os.ReadFile(um.jsonFile)
                if err != nil {
                        return fmt.Errorf("error reading existing JSON: %v", err)
                }
                json.Unmarshal(fileData, &data)
        }

        // DUPLICATE CHECK: If symbol already exists in file, skip saving
        for _, entry := range data.Listings {
                if entry.Symbol == symbol {
                        log.Printf("⚠️ DUPLICATE PREVENTED: %s already exists in %s, skipping save", symbol, um.jsonFile)
                        return nil // Not an error, just skip
                }
        }

        // Record detection timestamp for trade log
        detectedAt := time.Now()
        
        now := time.Now()
        newEntry := ListingEntry{
                Symbol:     symbol,
                Timestamp:  now.Format(time.RFC3339),
                DetectedAt: now.UTC().Format("2006-01-02 15:04:05 UTC"),
        }

        data.Listings = append([]ListingEntry{newEntry}, data.Listings...)

        tempFile := um.jsonFile + ".tmp"
        jsonData, err := json.MarshalIndent(data, "", "  ")
        if err != nil {
                return fmt.Errorf("error marshaling JSON: %v", err)
        }

        if err := os.WriteFile(tempFile, jsonData, 0644); err != nil {
                return fmt.Errorf("error writing temp file: %v", err)
        }

        if err := os.Rename(tempFile, um.jsonFile); err != nil {
                os.Remove(tempFile)
                return fmt.Errorf("error renaming temp file: %v", err)
        }

        savedAt := time.Now()
        
        // Initialize trade execution log entry
        um.logMu.Lock()
        um.currentLogEntry = &TradeExecutionLog{
                Ticker:          symbol,
                UpbitDetectedAt: detectedAt.Format("2006-01-02 15:04:05.000000"),
                SavedToFileAt:   savedAt.Format("2006-01-02 15:04:05.000000"),
                LatencyBreakdown: make(map[string]interface{}),
        }
        um.logMu.Unlock()

        log.Printf("✅ Successfully saved NEW listing %s to %s", symbol, um.jsonFile)
        return nil
}

// normalizeText: Unicode normalization and whitespace cleanup
func normalizeText(text string) string {
        // Remove punctuation and emojis, normalize whitespace
        reg := regexp.MustCompile(`[\p{P}\p{S}\p{Z}]+`)
        normalized := reg.ReplaceAllString(text, " ")
        normalized = regexp.MustCompile(`\s+`).ReplaceAllString(normalized, " ")
        return regexp.MustCompile(`\s+`).ReplaceAllString(normalized, "")
}

// containsAll: Check if text contains all words (order independent)
func containsAll(text string, words []string) bool {
        normalized := normalizeText(text)
        for _, word := range words {
                if !regexp.MustCompile(normalizeText(word)).MatchString(normalized) {
                        return false
                }
        }
        return true
}

// containsAny: Check if text contains any word
func containsAny(text string, words []string) bool {
        normalized := normalizeText(text)
        for _, word := range words {
                if regexp.MustCompile(normalizeText(word)).MatchString(normalized) {
                        return true
                }
        }
        return false
}

// isNegativeFiltered: Rule 2 - Negative filtering (highest priority)
func isNegativeFiltered(title string) bool {
        negativeRules := [][]string{
                {"거래지원", "종료"},           // trading support ended
                {"상장폐지"},                   // delisting
                {"유의", "종목", "지정"},       // caution designation
                {"투자", "유의", "촉구"},       // investment caution warning
                {"유의", "촉구"},               // caution warning
                {"유의", "종목", "지정", "해제"}, // caution designation removal
        }
        
        for _, rule := range negativeRules {
                if containsAll(title, rule) {
                        log.Printf("🚫 Negative filter: '%s' (contains: %v)", title, rule)
                        return true
                }
        }
        return false
}

// isPositiveFiltered: Rule 3 - Positive filtering
func isPositiveFiltered(title string) bool {
        positiveRules := [][]string{
                {"신규", "거래지원"},     // new trading support
                {"디지털", "자산", "추가"}, // digital asset addition
        }
        
        for _, rule := range positiveRules {
                if containsAll(title, rule) {
                        return true
                }
        }
        return false
}

// isMaintenanceUpdate: Rule 4 - Maintenance/Update filter
func isMaintenanceUpdate(title string) bool {
        updateKeywords := []string{
                "변경", "연기", "연장", "재개", 
                "입출금", "이벤트", "출금 수수료",
        }
        
        if containsAny(title, updateKeywords) {
                log.Printf("🔧 Maintenance/Update filter: '%s'", title)
                return true
        }
        return false
}

// extractTickers: Rule 5 - Extract tickers from title
func extractTickers(title string) []string {
        var tickers []string
        tickerMap := make(map[string]bool)
        
        // Find all parentheses content
        parenRegex := regexp.MustCompile(`\(([^)]+)\)`)
        matches := parenRegex.FindAllStringSubmatch(title, -1)
        
        for _, match := range matches {
                content := match[1]
                
                // Skip if contains "마켓" (market indicator)
                if regexp.MustCompile(`마켓`).MatchString(content) {
                        continue
                }
                
                // Split by comma, trim, uppercase
                parts := regexp.MustCompile(`[,\s]+`).Split(content, -1)
                for _, part := range parts {
                        part = regexp.MustCompile(`\s+`).ReplaceAllString(part, "")
                        part = regexp.MustCompile(`[^A-Z0-9]`).ReplaceAllString(part, "")
                        
                        // Exclude market symbols
                        if part == "KRW" || part == "BTC" || part == "USDT" {
                                continue
                        }
                        
                        // Validate pattern [A-Z0-9]{1,10}
                        if regexp.MustCompile(`^[A-Z0-9]{1,10}$`).MatchString(part) {
                                if !tickerMap[part] {
                                        tickerMap[part] = true
                                        tickers = append(tickers, part)
                                }
                        }
                }
        }
        
        return tickers
}

func (um *UpbitMonitor) processAnnouncements(body io.Reader) {
        var response UpbitAPIResponse
        if err := json.NewDecoder(body).Decode(&response); err != nil {
                log.Printf("JSON verisi işlenemedi: %v", err)
                return
        }

        newTickers := make(map[string]bool)
        var newTickersList []string

        for _, announcement := range response.Data.Notices {
                title := announcement.Title
                
                // Rule 2: Negative filtering (highest priority - skips everything)
                if isNegativeFiltered(title) {
                        continue
                }
                
                // Rule 3: Positive filtering (must pass)
                if !isPositiveFiltered(title) {
                        continue
                }
                
                // Rule 4: Maintenance/Update filter
                if isMaintenanceUpdate(title) {
                        continue
                }
                
                // Rule 5: Extract tickers
                tickers := extractTickers(title)
                if len(tickers) > 0 {
                        log.Printf("✅ Valid listing detected: '%s' → Tickers: %v", title, tickers)
                        for _, ticker := range tickers {
                                newTickers[ticker] = true
                                newTickersList = append(newTickersList, ticker)
                        }
                }
        }

        um.mu.Lock()
        defer um.mu.Unlock()

        var newlyAdded []string
        for ticker := range newTickers {
                if !um.cachedTickers[ticker] {
                        newlyAdded = append(newlyAdded, ticker)
                }
        }

        if len(newlyAdded) > 0 {
                fmt.Printf("\n🔥🔥🔥 YENİ LİSTELEME TESPİT EDİLDİ: %v 🔥🔥🔥\n", newlyAdded)
                for _, ticker := range newlyAdded {
                        um.cachedTickers[ticker] = true
                        if err := um.saveToJSON(ticker); err != nil {
                                log.Printf("Error saving ticker %s: %v", ticker, err)
                        }
                        if um.onNewListing != nil {
                                go um.onNewListing(ticker)
                        }
                }
        }

        // MERGE newTickers into cachedTickers (don't replace!)
        for ticker := range newTickers {
                um.cachedTickers[ticker] = true
        }
        
        log.Printf("📊 Cached tickers count: %d, Current API response: %v", len(um.cachedTickers), newTickersList)
}

func (um *UpbitMonitor) startProxyWorker(proxyURL string, proxyIndex int, staggerMs int) {
        // Stagger start times dynamically based on proxy count
        staggerDelay := time.Duration(proxyIndex*staggerMs) * time.Millisecond
        time.Sleep(staggerDelay)

        // Upbit Quotation API: 30 req/sec per IP (without Origin header)
        // Using 3.3s interval = 1091 req/hour = 0.303 req/sec per proxy
        // Total with 11 proxies: 3.33 req/sec, Coverage: 300ms (0.3s)
        interval := time.Duration(3300) * time.Millisecond
        ticker := time.NewTicker(interval)
        defer ticker.Stop()

        log.Printf("🔄 Proxy worker #%d started (interval: %v, stagger: %v)", proxyIndex+1, interval, staggerDelay)

        client, err := um.createProxyClient(proxyURL)
        if err != nil {
                log.Printf("❌ Proxy #%d client creation failed: %v", proxyIndex+1, err)
                return
        }

        for range ticker.C {
                req, err := http.NewRequest("GET", um.apiURL, nil)
                if err != nil {
                        log.Printf("❌ Proxy #%d: Request creation failed: %v", proxyIndex+1, err)
                        continue
                }

                // CRITICAL: Remove Origin header to avoid 1 req/10s limit
                req.Header.Del("Origin")
                req.Header.Del("Referer")
                
                um.mu.Lock()
                if um.cachedETag != "" {
                        req.Header.Set("If-None-Match", um.cachedETag)
                }
                um.mu.Unlock()

                resp, err := client.Do(req)
                if err != nil {
                        log.Printf("❌ Proxy #%d: API request failed: %v", proxyIndex+1, err)
                        continue
                }

                switch resp.StatusCode {
                case http.StatusOK:
                        log.Printf("🔥 Proxy #%d: CHANGE DETECTED! Processing...", proxyIndex+1)
                        newETag := resp.Header.Get("ETag")
                        um.mu.Lock()
                        um.cachedETag = newETag
                        um.mu.Unlock()
                        um.processAnnouncements(resp.Body)
                        resp.Body.Close()

                case http.StatusNotModified:
                        log.Printf("✓ Proxy #%d: No change (304)", proxyIndex+1)
                        resp.Body.Close()

                default:
                        log.Printf("⚠️ Proxy #%d: Unexpected status %d", proxyIndex+1, resp.StatusCode)
                        resp.Body.Close()
                }
        }
}

func (um *UpbitMonitor) Start() {
        log.Println("🚀 Upbit Monitor Starting with DYNAMIC PARALLEL PROXY EXECUTION...")
        
        if err := um.loadExistingData(); err != nil {
                log.Printf("⚠️ Warning: %v", err)
        }

        proxyCount := len(um.proxies)
        if proxyCount == 0 {
                log.Fatal("❌ No proxies configured! Please add UPBIT_PROXY_* to .env file")
        }

        // DYNAMIC CALCULATION based on proxy count
        // Upbit Quotation API: 30 req/sec per IP (without Origin header)
        // Using 3.3s interval for 300ms (0.3s) coverage
        proxyInterval := 3.3 // seconds per proxy (1091 req/hour per proxy)
        requestsPerHour := 3600 / proxyInterval // 1200 req/hour per proxy
        
        // Stagger dynamically: spread interval across all proxies
        staggerMs := int((proxyInterval * 1000.0 / float64(proxyCount))) // milliseconds
        coverageSeconds := float64(staggerMs) / 1000.0
        checksPerSecond := 1.0 / coverageSeconds
        
        log.Printf("📊 DYNAMIC PROXY CONFIGURATION:")
        log.Printf("   • Total Proxies: %d", proxyCount)
        log.Printf("   • Rate Limit: %.0f req/hour per proxy (%.2f req/sec, limit: 10 req/sec)", requestsPerHour, requestsPerHour/3600.0)
        log.Printf("   • Interval: %.1fs per proxy", proxyInterval)
        log.Printf("   • Stagger: %dms between workers", staggerMs)
        log.Printf("   • Origin header: REMOVED (avoids 1 req/10s strict limit)")
        log.Printf("⚡ PERFORMANCE:")
        log.Printf("   • Coverage: %.0fms (%.3fs)", coverageSeconds*1000, coverageSeconds)
        log.Printf("   • Speed: ~%.1f checks/second", checksPerSecond)
        log.Printf("   • Total capacity: %.0f req/hour", float64(proxyCount)*requestsPerHour)

        // Launch parallel workers for each proxy with dynamic stagger
        for i, proxyURL := range um.proxies {
                go um.startProxyWorker(proxyURL, i, staggerMs)
        }

        // Keep main goroutine alive
        select {}
}

// appendTradeLog appends a trade execution log entry to the JSON file
func (um *UpbitMonitor) appendTradeLog(logEntry *TradeExecutionLog) error {
        um.logMu.Lock()
        defer um.logMu.Unlock()

        var logs []TradeExecutionLog
        
        // Read existing logs if file exists
        if _, err := os.Stat(um.executionLogFile); err == nil {
                fileData, err := os.ReadFile(um.executionLogFile)
                if err != nil {
                        return fmt.Errorf("error reading execution log: %v", err)
                }
                if len(fileData) > 0 {
                        json.Unmarshal(fileData, &logs)
                }
        }

        // Append new log entry
        logs = append(logs, *logEntry)

        // Write back to file
        jsonData, err := json.MarshalIndent(logs, "", "  ")
        if err != nil {
                return fmt.Errorf("error marshaling execution log: %v", err)
        }

        if err := os.WriteFile(um.executionLogFile, jsonData, 0644); err != nil {
                return fmt.Errorf("error writing execution log: %v", err)
        }

        log.Printf("📊 Trade execution log saved for %s", logEntry.Ticker)
        return nil
}

// GetCurrentLogEntry returns the current log entry (for use in ExecuteTrade)
func (um *UpbitMonitor) GetCurrentLogEntry(ticker string) *TradeExecutionLog {
        um.logMu.Lock()
        defer um.logMu.Unlock()
        
        if um.currentLogEntry != nil && um.currentLogEntry.Ticker == ticker {
                return um.currentLogEntry
        }
        return nil
}

// GetServerTime retrieves Upbit server time from HTTP response headers
func (um *UpbitMonitor) GetServerTime() (*TimeSyncResult, error) {
        localTimeBefore := time.Now()

        // Use any lightweight public endpoint
        client, err := um.createProxyClient(um.proxies[0])
        if err != nil {
                // Fallback to default client if proxy fails
                client = &http.Client{Timeout: 10 * time.Second}
        }

        req, err := http.NewRequest("GET", um.apiURL, nil)
        if err != nil {
                return nil, fmt.Errorf("failed to create request: %w", err)
        }

        resp, err := client.Do(req)
        if err != nil {
                return nil, fmt.Errorf("request failed: %w", err)
        }
        defer resp.Body.Close()

        localTimeAfter := time.Now()

        // Parse Date header (RFC1123 format)
        dateHeader := resp.Header.Get("Date")
        if dateHeader == "" {
                return nil, fmt.Errorf("no Date header in response")
        }

        serverTime, err := time.Parse(time.RFC1123, dateHeader)
        if err != nil {
                return nil, fmt.Errorf("failed to parse Date header: %w", err)
        }

        // Calculate network latency (round-trip time / 2)
        roundTripTime := localTimeAfter.Sub(localTimeBefore)
        networkLatency := roundTripTime / 2

        // Adjust server time for network latency
        adjustedServerTime := serverTime.Add(networkLatency)

        // Calculate clock offset
        clockOffset := adjustedServerTime.Sub(localTimeAfter)

        return &TimeSyncResult{
                ServerTime:     adjustedServerTime,
                LocalTime:      localTimeAfter,
                ClockOffset:    clockOffset,
                NetworkLatency: networkLatency,
        }, nil
}
