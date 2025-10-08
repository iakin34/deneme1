package main

import (
	"log"
	"os"
)

func main() {
	log.Println("🚀 Upbit-Bitget Auto Trading System Starting...")

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN environment variable is required")
	}

	encKey := os.Getenv("BOT_ENCRYPTION_KEY")
	if encKey == "" {
		log.Fatal("BOT_ENCRYPTION_KEY environment variable is required")
	}

	bot, err := NewTelegramBot(token)
	if err != nil {
		log.Fatalf("Failed to create Telegram bot: %v", err)
	}

	upbitMonitor := NewUpbitMonitor(func(symbol string) {
		log.Printf("🔥 New Upbit listing callback: %s", symbol)
	})

	log.Println("✅ All systems initialized")
	log.Println("📡 Starting Upbit monitor...")
	log.Println("🤖 Starting Telegram bot...")

	go upbitMonitor.Start()

	bot.Start()
}
