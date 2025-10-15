.PHONY: run checksync build clean

# Start the bot
run:
	go run .

# Check time synchronization with exchanges
checksync:
	@echo "⏰ Checking time synchronization with exchanges..."
	@echo ""
	@go run check_sync_helper.go upbit_monitor.go bitget.go

# Build the bot
build:
	go build -o upbit-bot .

# Clean build artifacts
clean:
	rm -f upbit-bot
