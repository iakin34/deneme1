package main

import (
        "log"

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

        log.Println("✅ All systems initialized")
        log.Println("📡 Starting Upbit monitor...")
        log.Println("🤖 Starting Telegram bot...")

        go upbitMonitor.Start()

        // Start bot message loop
        telegramBot.Start()
}
