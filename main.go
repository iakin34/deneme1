package main

import (
        "log"
        "time"

        "github.com/joho/godotenv"
)

func main() {
        log.Println("🚀 Upbit-Bitget Auto Trading System Starting...")

        _ = godotenv.Load()

        // Start Telegram bot first to get bot instance
        telegramBot := InitializeTelegramBot()
        
        // Create Upbit monitor with DIRECT callback to trading
        upbitMonitor := NewUpbitMonitor(func(symbol string) {
                log.Printf("🔥 INSTANT CALLBACK - New Upbit listing: %s", symbol)
                // DIRECT execution - no file delay!
                go telegramBot.ExecuteAutoTradeForAllUsers(symbol)
        })
        
        // Link monitor to bot for trade logging
        telegramBot.SetUpbitMonitor(upbitMonitor)

        log.Println("✅ All systems initialized")
        
        // TIME SYNCHRONIZATION CHECK
        log.Println("⏰ Checking time synchronization with exchanges...")
        
        // Check Upbit time sync
        upbitSync, err := upbitMonitor.GetServerTime()
        if err != nil {
                log.Printf("⚠️ Upbit time sync failed: %v", err)
        } else {
                log.Printf("📡 UPBIT TIME SYNC:")
                log.Printf("   • Server Time: %s", upbitSync.ServerTime.Format("2006-01-02 15:04:05.000"))
                log.Printf("   • Local Time:  %s", upbitSync.LocalTime.Format("2006-01-02 15:04:05.000"))
                log.Printf("   • Clock Offset: %v", upbitSync.ClockOffset)
                log.Printf("   • Network Latency: %v", upbitSync.NetworkLatency)
                
                if upbitSync.ClockOffset.Abs() > 1*time.Second {
                        log.Printf("⚠️ WARNING: Clock offset > 1s! May cause timing issues!")
                } else {
                        log.Printf("   ✅ Clock sync OK (offset < 1s)")
                }
        }
        
        // Check Bitget time sync (create temporary API instance)
        testBitget := NewBitgetAPI("test", "test", "test")
        bitgetSync, err := testBitget.GetServerTime()
        if err != nil {
                log.Printf("⚠️ Bitget time sync failed: %v", err)
        } else {
                log.Printf("📡 BITGET TIME SYNC:")
                log.Printf("   • Server Time: %s", bitgetSync.ServerTime.Format("2006-01-02 15:04:05.000"))
                log.Printf("   • Local Time:  %s", bitgetSync.LocalTime.Format("2006-01-02 15:04:05.000"))
                log.Printf("   • Clock Offset: %v", bitgetSync.ClockOffset)
                log.Printf("   • Network Latency: %v", bitgetSync.NetworkLatency)
                
                if bitgetSync.ClockOffset.Abs() > 1*time.Second {
                        log.Printf("⚠️ WARNING: Clock offset > 1s! May cause timing issues!")
                } else {
                        log.Printf("   ✅ Clock sync OK (offset < 1s)")
                }
        }
        
        log.Println("📡 Starting Upbit monitor...")
        log.Println("🤖 Starting Telegram bot...")

        go upbitMonitor.Start()

        // Start bot message loop
        telegramBot.Start()
}
