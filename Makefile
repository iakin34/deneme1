.PHONY: run checksync synctime build clean install-tools

# Start the bot
run:
	go run .

# Check time synchronization with exchanges
checksync:
	@cd tools && go run checksync.go

# Sync system time with Upbit server (requires root)
synctime:
	@./sync_upbit_time.sh

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
