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
                apiURL:        "https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=20&category=overall",
                proxies:       proxies,
                tickerRegex:   regexp.MustCompile(`\(([A-Z]{2,6})\)`), // Only 2-6 uppercase letters (valid tickers)
                cachedTickers: make(map[string]bool),
                proxyIndex:    0,
                jsonFile:      "upbit_new.json",
                onNewListing:  onNewListing,
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

        log.Printf("Successfully saved %s to %s", symbol, um.jsonFile)
        return nil
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
                // Check for new listing keywords (multiple formats)
                isNewListing := contains(announcement.Title, "신규") || 
                                contains(announcement.Title, "Market Support") ||
                                contains(announcement.Title, "디지털 자산 추가") ||
                                contains(announcement.Title, "KRW 마켓")
                
                if isNewListing {
                        matches := um.tickerRegex.FindStringSubmatch(announcement.Title)
                        if len(matches) > 1 {
                                ticker := matches[1]
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

        um.cachedTickers = newTickers
        log.Printf("Mevcut ticker listesi güncellendi: %v", newTickersList)
}

func contains(s, substr string) bool {
        return regexp.MustCompile(substr).MatchString(s)
}

func (um *UpbitMonitor) Start() {
        log.Println("Upbit Yeni Listeleme Takipçisi Başlatıldı...")
        log.Printf("Kullanılacak Proxy Sayısı: %d", len(um.proxies))

        if err := um.loadExistingData(); err != nil {
                log.Printf("Warning: %v", err)
        }

        ticker := time.NewTicker(1 * time.Second)
        defer ticker.Stop()

        for range ticker.C {
                um.mu.Lock()
                currentProxy := um.proxies[um.proxyIndex]
                um.proxyIndex = (um.proxyIndex + 1) % len(um.proxies)
                um.mu.Unlock()

                log.Printf("Kontrol ediliyor... Proxy: %s", currentProxy)

                client, err := um.createProxyClient(currentProxy)
                if err != nil {
                        log.Printf("HATA: Client oluşturulamadı: %v", err)
                        continue
                }

                req, err := http.NewRequest("GET", um.apiURL, nil)
                if err != nil {
                        log.Printf("HATA: İstek oluşturulamadı: %v", err)
                        continue
                }

                um.mu.Lock()
                if um.cachedETag != "" {
                        req.Header.Set("If-None-Match", um.cachedETag)
                }
                um.mu.Unlock()

                resp, err := client.Do(req)
                if err != nil {
                        log.Printf("HATA: API'ye istek gönderilemedi: %v", err)
                        continue
                }

                switch resp.StatusCode {
                case http.StatusOK:
                        log.Println("Değişiklik tespit edildi! Veri işleniyor...")
                        newETag := resp.Header.Get("ETag")
                        um.mu.Lock()
                        um.cachedETag = newETag
                        um.mu.Unlock()
                        um.processAnnouncements(resp.Body)
                        resp.Body.Close()

                case http.StatusNotModified:
                        log.Println("Veride değişiklik yok (304 Not Modified).")
                        resp.Body.Close()

                default:
                        log.Printf("Beklenmedik HTTP durum kodu: %d", resp.StatusCode)
                        resp.Body.Close()
                }
        }
}
