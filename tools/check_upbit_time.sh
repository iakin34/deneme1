#!/bin/bash
# Upbit API ile sistem saati sync kontrolü

echo "⏰ Checking time sync with Upbit API..."
echo ""

# Upbit API'den Date header al
UPBIT_URL="https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=1"

# Server time al (Date header)
SERVER_TIME=$(curl -sI "$UPBIT_URL" | grep -i "^date:" | sed 's/date: //I')

if [ -z "$SERVER_TIME" ]; then
    echo "❌ Failed to fetch Upbit server time"
    exit 1
fi

# Convert to timestamp
SERVER_TS=$(date -d "$SERVER_TIME" +%s 2>/dev/null)
LOCAL_TS=$(date +%s)

# Calculate offset
OFFSET=$((LOCAL_TS - SERVER_TS))

echo "📡 Upbit Server Time: $SERVER_TIME"
echo "🖥️  Local System Time: $(date -R)"
echo ""
echo "⏱️  Offset: ${OFFSET}s"

if [ $OFFSET -lt 0 ]; then
    ABS_OFFSET=$((-OFFSET))
else
    ABS_OFFSET=$OFFSET
fi

if [ $ABS_OFFSET -gt 1 ]; then
    echo "⚠️  WARNING: Clock offset > 1 second!"
    echo "   Run: sudo systemctl restart chrony"
else
    echo "✅ Clock sync OK (offset < 1s)"
fi
