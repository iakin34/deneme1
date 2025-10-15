package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"time"
)

type TimeSyncResult struct {
	ServerTime     time.Time
	LocalTime      time.Time
	ClockOffset    time.Duration
	NetworkLatency time.Duration
}

func main() {
	log.SetFlags(0)
	
	fmt.Println("⏰ Checking time synchronization with exchanges...")
	fmt.Println()

	upbitSync, err := getUpbitServerTime()
	if err != nil {
		log.Printf("❌ Upbit time sync failed: %v\n", err)
	} else {
		fmt.Println("📡 UPBIT TIME SYNC:")
		fmt.Printf("   • Server Time:     %s\n", upbitSync.ServerTime.Format("2006-01-02 15:04:05.000"))
		fmt.Printf("   • Local Time:      %s\n", upbitSync.LocalTime.Format("2006-01-02 15:04:05.000"))
		fmt.Printf("   • Clock Offset:    %v\n", upbitSync.ClockOffset)
		fmt.Printf("   • Network Latency: %v\n", upbitSync.NetworkLatency)
		
		if upbitSync.ClockOffset.Abs() > 1*time.Second {
			fmt.Println("   ⚠️ WARNING: Clock offset > 1s!")
		} else {
			fmt.Println("   ✅ Clock sync OK (offset < 1s)")
		}
		fmt.Println()
	}

	bitgetSync, err := getBitgetServerTime()
	if err != nil {
		log.Printf("❌ Bitget time sync failed: %v\n", err)
	} else {
		fmt.Println("📡 BITGET TIME SYNC:")
		fmt.Printf("   • Server Time:     %s\n", bitgetSync.ServerTime.Format("2006-01-02 15:04:05.000"))
		fmt.Printf("   • Local Time:      %s\n", bitgetSync.LocalTime.Format("2006-01-02 15:04:05.000"))
		fmt.Printf("   • Clock Offset:    %v\n", bitgetSync.ClockOffset)
		fmt.Printf("   • Network Latency: %v\n", bitgetSync.NetworkLatency)
		
		if bitgetSync.ClockOffset.Abs() > 1*time.Second {
			fmt.Println("   ⚠️ WARNING: Clock offset > 1s!")
		} else {
			fmt.Println("   ✅ Clock sync OK (offset < 1s)")
		}
	}
}

func getUpbitServerTime() (*TimeSyncResult, error) {
	localTimeBefore := time.Now()
	
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", "https://api.upbit.com/v1/notices?page=1&per_page=1", nil)
	if err != nil {
		return nil, err
	}
	
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	localTimeAfter := time.Now()
	
	dateHeader := resp.Header.Get("Date")
	if dateHeader == "" {
		return nil, fmt.Errorf("no Date header")
	}
	
	serverTime, err := time.Parse(time.RFC1123, dateHeader)
	if err != nil {
		return nil, err
	}
	
	roundTripTime := localTimeAfter.Sub(localTimeBefore)
	networkLatency := roundTripTime / 2
	adjustedServerTime := serverTime.Add(networkLatency)
	clockOffset := adjustedServerTime.Sub(localTimeAfter)
	
	return &TimeSyncResult{
		ServerTime:     adjustedServerTime,
		LocalTime:      localTimeAfter,
		ClockOffset:    clockOffset,
		NetworkLatency: networkLatency,
	}, nil
}

func getBitgetServerTime() (*TimeSyncResult, error) {
	localTimeBefore := time.Now()
	
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.bitget.com/api/v2/public/time")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	
	localTimeAfter := time.Now()
	
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	
	var result struct {
		Code string `json:"code"`
		Data struct {
			ServerTime string `json:"serverTime"`
		} `json:"data"`
	}
	
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	
	serverTimeMs, err := strconv.ParseInt(result.Data.ServerTime, 10, 64)
	if err != nil {
		return nil, err
	}
	
	serverTime := time.UnixMilli(serverTimeMs)
	roundTripTime := localTimeAfter.Sub(localTimeBefore)
	networkLatency := roundTripTime / 2
	adjustedServerTime := serverTime.Add(networkLatency)
	clockOffset := adjustedServerTime.Sub(localTimeAfter)
	
	return &TimeSyncResult{
		ServerTime:     adjustedServerTime,
		LocalTime:      localTimeAfter,
		ClockOffset:    clockOffset,
		NetworkLatency: networkLatency,
	}, nil
}
