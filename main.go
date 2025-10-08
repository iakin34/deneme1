package main

import (
        "log"

        "github.com/joho/godotenv"
)

func main() {
        log.Println("🚀 Upbit-Bitget Auto Trading System Starting...")

        _ = godotenv.Load()

        upbitMonitor := NewUpbitMonitor(func(symbol string) {
                log.Printf("🔥 New Upbit listing callback: %s", symbol)
        })

        log.Println("✅ All systems initialized")
        log.Println("📡 Starting Upbit monitor...")
        log.Println("🤖 Starting Telegram bot...")

        go upbitMonitor.Start()

        StartTradingBot()
}
