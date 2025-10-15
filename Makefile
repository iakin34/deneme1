.PHONY: run checksync synctime build clean

# Start the bot
run:
        go run .

# Check time synchronization with exchanges
checksync:
        @echo "⏰ Checking time synchronization with exchanges..."
        @echo ""
        @go run check_sync_helper.go upbit_monitor.go bitget.go

# Sync system time with Upbit server (requires root)
synctime:
        @./sync_upbit_time.sh

# Build the bot
build:
        go build -o upbit-bot .

# Clean build artifacts
clean:
        rm -f upbit-bot
