#!/bin/bash

# Time Synchronization Check Script
# Usage: ./checksync.sh

cat << 'EOF' > /tmp/check_sync_temp.go
package main

import (
	"fmt"
	"log"
	"os"
	"time"
	"github.com/joho/godotenv"
)

func main() {
	log.SetFlags(0)
	godotenv.Load()

	fmt.Println("⏰ Checking time synchronization with exchanges...")
	fmt.Println()

	upbitProxies := os.Getenv("UPBIT_PROXIES")
	if upbitProxies == "" {
		log.Fatal("❌ UPBIT_PROXIES not found in .env")
	}

	upbitMonitor := NewUpbitMonitor(func(symbol string) {})
	upbitSync, err := upbitMonitor.GetServerTime()
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

	testBitget := NewBitgetAPI("test", "test", "test")
	bitgetSync, err := testBitget.GetServerTime()
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
EOF

# Run with all necessary files
go run /tmp/check_sync_temp.go upbit_monitor.go bitget.go types.go 2>/dev/null

# Clean up
rm -f /tmp/check_sync_temp.go
