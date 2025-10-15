package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"time"

	"golang.org/x/net/proxy"
)

type TestResult struct {
	Interval          string        `json:"interval"`
	RequestsPerSec    float64       `json:"requests_per_sec"`
	TotalRequests     int           `json:"total_requests"`
	SuccessRequests   int           `json:"success_requests"`
	RateLimitHits     int           `json:"rate_limit_hits"`
	FirstRateLimitAt  int           `json:"first_rate_limit_at_request"`
	Duration          time.Duration `json:"duration_seconds"`
	SafeForProduction bool          `json:"safe_for_production"`
}

func createProxyClient(proxyURL string) (*http.Client, error) {
	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		return nil, fmt.Errorf("proxy parse error: %w", err)
	}

	dialer, err := proxy.FromURL(parsedURL, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("proxy dialer error: %w", err)
	}

	transport := &http.Transport{
		Dial: dialer.Dial,
	}

	return &http.Client{
		Transport: transport,
		Timeout:   10 * time.Second,
	}, nil
}

func testInterval(client *http.Client, apiURL string, interval time.Duration, count int) TestResult {
	log.Printf("\n🧪 Testing interval: %v (%.2f req/sec)", interval, 1000.0/float64(interval.Milliseconds()))
	
	result := TestResult{
		Interval:       interval.String(),
		RequestsPerSec: 1000.0 / float64(interval.Milliseconds()),
		TotalRequests:  count,
	}
	
	startTime := time.Now()
	firstRateLimit := -1
	
	for i := 0; i < count; i++ {
		req, err := http.NewRequest("GET", apiURL, nil)
		if err != nil {
			log.Printf("❌ Request creation failed: %v", err)
			continue
		}
		
		// Remove Origin header
		req.Header.Del("Origin")
		req.Header.Del("Referer")
		
		resp, err := client.Do(req)
		if err != nil {
			log.Printf("❌ Request failed: %v", err)
			continue
		}
		
		status := resp.StatusCode
		resp.Body.Close()
		
		if status == http.StatusTooManyRequests {
			result.RateLimitHits++
			if firstRateLimit == -1 {
				firstRateLimit = i + 1
				result.FirstRateLimitAt = firstRateLimit
			}
			log.Printf("⚠️ RATE LIMIT at request #%d (elapsed: %v)", i+1, time.Since(startTime))
		} else if status == http.StatusOK || status == http.StatusNotModified {
			result.SuccessRequests++
		}
		
		// Progress indicator
		if (i+1)%10 == 0 {
			log.Printf("   Progress: %d/%d (status: %d)", i+1, count, status)
		}
		
		time.Sleep(interval)
	}
	
	result.Duration = time.Since(startTime)
	
	// Safe if less than 5% rate limit hits
	successRate := float64(result.SuccessRequests) / float64(result.TotalRequests) * 100
	result.SafeForProduction = successRate >= 95.0
	
	log.Printf("✅ Completed: %d success, %d rate limits (%.1f%% success)", 
		result.SuccessRequests, result.RateLimitHits, successRate)
	
	if result.SafeForProduction {
		log.Printf("🎯 This interval is SAFE for production!")
	} else {
		log.Printf("❌ This interval is NOT safe (too many rate limits)")
	}
	
	return result
}

func main() {
	log.Println("🔬 UPBIT RATE LIMIT DISCOVERY TOOL")
	log.Println("==================================\n")
	
	// Get proxy from environment
	proxyURL := os.Getenv("UPBIT_PROXY_1")
	if proxyURL == "" {
		log.Fatal("❌ UPBIT_PROXY_1 environment variable not set")
	}
	
	apiURL := "https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=5&category=overall"
	
	// Create proxy client
	client, err := createProxyClient(proxyURL)
	if err != nil {
		log.Fatalf("❌ Failed to create proxy client: %v", err)
	}
	
	log.Printf("✅ Using proxy: %s\n", proxyURL)
	log.Printf("📍 Testing endpoint: %s\n", apiURL)
	
	// Test different intervals
	testCases := []struct {
		interval time.Duration
		count    int
		waitTime time.Duration
	}{
		{500 * time.Millisecond, 50, 60 * time.Second},  // 2 req/sec
		{1000 * time.Millisecond, 50, 60 * time.Second}, // 1 req/sec
		{2000 * time.Millisecond, 50, 60 * time.Second}, // 0.5 req/sec
		{3000 * time.Millisecond, 50, 60 * time.Second}, // 0.33 req/sec
		{3300 * time.Millisecond, 50, 60 * time.Second}, // 0.303 req/sec (current)
		{4000 * time.Millisecond, 50, 60 * time.Second}, // 0.25 req/sec
		{5000 * time.Millisecond, 50, 0},                 // 0.2 req/sec (no wait after last)
	}
	
	var results []TestResult
	
	for i, tc := range testCases {
		result := testInterval(client, apiURL, tc.interval, tc.count)
		results = append(results, result)
		
		// Wait between tests (except last one)
		if i < len(testCases)-1 {
			log.Printf("\n⏸️  Waiting %v before next test...\n", tc.waitTime)
			time.Sleep(tc.waitTime)
		}
	}
	
	// Save results
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		log.Printf("⚠️ Failed to marshal results: %v", err)
	} else {
		if err := os.WriteFile("rate_limit_test_results.json", data, 0644); err != nil {
			log.Printf("⚠️ Failed to save results: %v", err)
		} else {
			log.Printf("\n💾 Results saved to: rate_limit_test_results.json")
		}
	}
	
	// Print summary
	log.Println("\n📊 TEST SUMMARY")
	log.Println("================")
	
	var safeInterval time.Duration
	for _, r := range results {
		status := "❌"
		if r.SafeForProduction {
			status = "✅"
			if safeInterval == 0 {
				safeInterval, _ = time.ParseDuration(r.Interval)
			}
		}
		
		log.Printf("%s %s (%.2f req/sec): %d/%d success", 
			status, r.Interval, r.RequestsPerSec, r.SuccessRequests, r.TotalRequests)
		
		if r.RateLimitHits > 0 {
			log.Printf("   First rate limit at request #%d", r.FirstRateLimitAt)
		}
	}
	
	// Recommendation
	log.Println("\n🎯 RECOMMENDATION")
	log.Println("=================")
	
	if safeInterval > 0 {
		coverage := safeInterval.Milliseconds() / 11 // 11 proxies
		log.Printf("✅ Safe interval found: %v", safeInterval)
		log.Printf("📏 With 11 proxies, coverage would be: %dms (%.3fs)", coverage, float64(coverage)/1000.0)
		
		if coverage <= 300 {
			log.Printf("🎉 This achieves your 0.3s target!")
		} else {
			log.Printf("⚠️ This is slower than 0.3s target (%dms > 300ms)", coverage)
			log.Printf("💡 Consider using different ASN proxies to increase rate limit tolerance")
		}
	} else {
		log.Printf("❌ No safe interval found in tested range")
		log.Printf("💡 The API may have stricter limits or ASN-based restrictions")
		log.Printf("🔧 Try testing with proxies from different providers (AWS, Vultr, Hetzner)")
	}
	
	log.Println("\n✅ Test completed!")
}
