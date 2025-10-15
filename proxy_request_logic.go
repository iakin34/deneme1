package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/net/proxy"
)

// createProxyClient creates HTTP client with SOCKS5 proxy
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

// startProxyWorker starts a single proxy worker with staggered execution
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

// Start function - proxy initialization and launch
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
	requestsPerHour := 3600 / proxyInterval // 1091 req/hour per proxy
	
	// Stagger dynamically: spread interval across all proxies
	staggerMs := int((proxyInterval * 1000.0 / float64(proxyCount))) // milliseconds
	coverageSeconds := float64(staggerMs) / 1000.0
	checksPerSecond := 1.0 / coverageSeconds
	
	log.Printf("📊 DYNAMIC PROXY CONFIGURATION:")
	log.Printf("   • Total Proxies: %d", proxyCount)
	log.Printf("   • Rate Limit: %.0f req/hour per proxy (%.2f req/sec, limit: 30 req/sec)", requestsPerHour, requestsPerHour/3600.0)
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
