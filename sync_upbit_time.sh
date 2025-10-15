#!/bin/bash

echo "⏰ Syncing system time with Upbit server..."
echo ""

# Get Upbit server time from HTTP header
RESPONSE=$(curl -s -D - https://api.upbit.com/v1/notices -o /dev/null)
SERVER_TIME=$(echo "$RESPONSE" | grep -i "^date:" | cut -d' ' -f2-)

if [ -z "$SERVER_TIME" ]; then
    echo "❌ Failed to get Upbit server time"
    exit 1
fi

echo "📡 Upbit Server Time: $SERVER_TIME"

# Parse and set system time
FORMATTED_TIME=$(date -d "$SERVER_TIME" "+%Y-%m-%d %H:%M:%S")
echo "🔧 Setting system time to: $FORMATTED_TIME"

# Set system time (requires root)
timedatectl set-ntp false
date -s "$SERVER_TIME"

# Verify
echo ""
echo "✅ System time synchronized!"
echo "🕐 New system time: $(date '+%Y-%m-%d %H:%M:%S.%3N')"
echo ""
echo "⚠️  Note: Auto NTP sync disabled. Re-enable with: timedatectl set-ntp true"
