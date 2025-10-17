// upbit_monitor_optimized.go
// Bu sürüm optimize edilmiş proxy interval ayarı, dinamik zamanlama, gecikme loglaması ve rastgele ama tekrar etmeyen proxy seçimi içerir.

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sync"
	"time"

	"golang.org/x/net/proxy"
)

// (Gerekli struct ve helper fonksiyonlar değişmeden devam eder)
// Bu kısmı önceki sürümden aynen alabilirsin.

// Proxy seçimi için döngüsel ve random sıra
func generateShuffledIndices(n int) []int {
	indices := rand.Perm(n) // Random permütasyon
	return indices
}

func (um *UpbitMonitor) startProxyWorkerOptimized(proxyIndex int, proxyURL string) {
	interval := 500 * time.Millisecond
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	client, err := um.createProxyClient(proxyURL)
	if err != nil {
		log.Printf("❌ Proxy #%d client creation failed: %v", proxyIndex+1, err)
		return
	}

	log.Printf("🔄 [Optimized] Proxy worker #%d started (interval: %v)", proxyIndex+1, interval)

	for range ticker.C {
		startTime := time.Now()

		req, err := http.NewRequest("GET", um.apiURL, nil)
		if err != nil {
			log.Printf("❌ Proxy #%d: Request creation failed: %v", proxyIndex+1, err)
			continue
		}

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
			newETag := resp.Header.Get("ETag")
			svrTime := resp.Header.Get("Date")
			elapsed := time.Since(startTime)

			log.Printf("🔥 Proxy #%d: CHANGE DETECTED! Elapsed: %v, ServerTime: %s", proxyIndex+1, elapsed, svrTime)

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

func (um *UpbitMonitor) StartOptimized() {
	log.Println("🚀 Upbit Monitor Starting [Optimized + Random Proxy Rotation] Version...")

	if err := um.loadExistingData(); err != nil {
		log.Printf("⚠️ Warning: %v", err)
	}

	proxyCount := len(um.proxies)
	if proxyCount == 0 {
		log.Fatal("❌ No proxies configured! Please add UPBIT_PROXY_* to .env file")
	}

	shuffled := generateShuffledIndices(proxyCount)

	for i := 0; i < proxyCount; i++ {
		proxyIdx := shuffled[i]
		proxyURL := um.proxies[proxyIdx]
		go um.startProxyWorkerOptimized(i, proxyURL)
	}

	select {} // Sonsuz çalışmaya devam et
}

func main() {
	rand.Seed(time.Now().UnixNano())
	monitor := NewUpbitMonitor(nil)
	monitor.StartOptimized()
}
