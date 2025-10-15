.PHONY: run checksync synctime testrate build clean install-tools

# Start the bot
run:
	go run .

# Check time synchronization with exchanges
checksync:
	@cd tools && go run checksync.go

# Sync system time with Upbit server (requires root)
synctime:
	@./sync_upbit_time.sh

# Test Upbit API rate limits (discovers real limits empirically)
testrate:
	@echo "🔬 Starting rate limit discovery test..."
	@echo "⚠️  This will take ~7-10 minutes to complete"
	@echo ""
	@cd tools && go run test_rate_limit.go

# Build the bot
build:
	go build -o upbit-bitget-bot .

# Install helper tools to system (requires root on server)
install-tools:
	@echo "Installing helper tools..."
	@cd tools && go build -o /usr/local/bin/checksync checksync.go
	@cp sync_upbit_time.sh /usr/local/bin/synctime
	@chmod +x /usr/local/bin/synctime
	@echo "✅ Tools installed: checksync, synctime"
	@echo "   You can now use: checksync or synctime from anywhere"

# Clean build artifacts
clean:
	rm -f upbit-bitget-bot
