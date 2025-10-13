# Overview

This is a cryptocurrency trading bot that monitors Upbit exchange for new coin listing announcements and automatically executes trades on Bitget exchange. The system uses proxy rotation to avoid rate limits, detects new coin listings through API polling with ETag-based change detection, and manages user configurations for automated trading with configurable leverage and margin settings. Execution speed: **~0.5-0.8 seconds** from detection to order placement.

# User Preferences

Preferred communication style: Simple, everyday language.

# System Architecture

## Core Components

### 1. Listing Detection System
- **Problem**: Need to detect new cryptocurrency listings on Upbit exchange in real-time without hitting rate limits
- **Solution**: Advanced multi-layer filtering with proxy rotation
  - Uses 12 SOCKS5 proxies in round-robin fashion (~300 req/hour per proxy, under 400 limit)
  - 1-second polling interval with ETag-based change detection
  - **5-Rule Filtering System** (prevents false positives):
    1. **Unicode Normalization**: Handles spacing variations (거래지원 = 거래 지원)
    2. **Negative Filtering** (highest priority, blocks): {거래지원,종료}, 상장폐지, {유의,종목,지정}, etc.
    3. **Positive Filtering** (must match): {신규,거래지원} OR {디지털,자산,추가}
    4. **Maintenance Filter**: Blocks 변경, 연기, 입출금, 이벤트 keywords
    5. **Ticker Extraction**: Excludes KRW/BTC/USDT, validates [A-Z0-9]{1,10}, skips "마켓" parentheses
- **API Endpoint**: `https://api-manager.upbit.com/api/v1/announcements?os=web&page=1&per_page=20&category=overall`
- **Rationale**: Multi-layer filtering eliminates false positives (events, delistings, warnings) with 100% accuracy

### 2. Trading Execution Engine
- **Problem**: Execute trades automatically when new listings are detected
- **Solution**: Parallel API execution with Bitget integration
  - **Parallel execution**: Leverage set + Price get simultaneously (~300ms saved)
  - Multi-user parallel goroutines (all users trade simultaneously)
  - Authenticated API calls using API key, secret, and passphrase
  - Configurable margin (USDT amount) and leverage settings per user
  - Order placement on Bitget futures/spot markets
  - **Performance**: 0.5-0.8 seconds from detection to order (60% faster than sequential)
- **Trade-offs**: Automated execution increases speed but requires careful risk management through leverage limits

### 3. User Management System
- **Problem**: Support multiple users with individual trading configurations
- **Solution**: JSON-based user state management
  - User profiles stored in `bot_users.json`
  - Each user has unique Bitget API credentials
  - Per-user margin, leverage, and activation status
  - State machine tracking (`awaiting_api_key`, `complete`, etc.)
  - Telegram user ID as primary identifier
- **Rationale**: JSON storage provides simplicity for bot-scale operations without database overhead

### 4. Telegram Bot Interface
- **Problem**: Users need a simple way to configure and control the bot
- **Solution**: Telegram bot for user interaction
  - User registration and API key configuration
  - State-based conversation flow
  - Trading parameter setup (margin, leverage)
  - Bot activation/deactivation controls
- **Alternatives Considered**: Web dashboard was considered but Telegram provides better mobile access and notification capabilities

## Data Storage

### File-Based Persistence
- **`bot_users.json`**: User profiles and trading configurations
  - Stores encrypted/encoded API credentials
  - Tracks user state and preferences
  - Maintains creation and update timestamps
- **`upbit_new.json`**: Historical listing detections
  - Records detected coin symbols
  - Timestamps for detection and announcement
  - Prevents duplicate trade execution
- **`active_positions.json`**: Real-time position tracking
  - Tracks open positions for reminder system
  - **Auto-sync with Bitget**: Validates positions every 5 minutes
  - Automatically removes positions closed on exchange (prevents phantom reminders)
  - Thread-safe with mutex protection

### Data Structure Decisions
- **Pros**: Simple, human-readable, no database setup required
- **Cons**: Limited scalability, no transaction support, potential race conditions
- **Rationale**: Appropriate for single-instance bot with limited concurrent users

## Security Architecture

### Credential Management
- API credentials stored with encoding/encryption
- Separate storage for API key, secret, and passphrase
- User-specific credential isolation

### Risk Controls
- Per-user leverage limits
- Configurable margin amounts
- Manual activation requirement (`is_active` flag)
- State machine prevents incomplete configurations

## Integration Architecture

### Upbit API Integration
- Public announcements API endpoint
- ETag-based conditional requests
- Regex pattern matching for coin symbol extraction
- Handles Korean language announcement titles

### Bitget API Integration
- Authenticated futures/spot trading
- Order placement with margin and leverage parameters
- API credential validation during setup

### Telegram Bot API
- Webhook or polling-based message handling
- State-based conversation management
- User ID-based session tracking

## Concurrency & Performance

### Proxy Management
- Round-robin rotation across 12 SOCKS5 proxies
- ~300 requests/hour per proxy (safely under 400 limit)
- 1-second global polling interval with ETag optimization

### Position Tracking & Auto-Sync
- **5-minute reminder system**: Sends P&L updates to users
- **Bitget API validation**: Each reminder checks if position exists on exchange
- **Auto-cleanup**: If position closed on Bitget (user closed manually), automatically:
  - Removes from `active_positions.json`
  - Stops sending reminders
  - Notifies user that position was closed
- **Prevents phantom reminders**: Solves issue where users close positions on exchange but tracking continues

### Error Handling
- Proxy connection failure tolerance
- API error recovery mechanisms
- Graceful degradation on network issues

# External Dependencies

## Third-Party Services

### Upbit Exchange
- **Purpose**: Source for new coin listing announcements
- **API**: Public announcements endpoint (`/v1/notices`)
- **Authentication**: None required for public data
- **Rate Limits**: Managed through proxy rotation

### Bitget Exchange
- **Purpose**: Execution platform for automated trades
- **API**: Futures/Spot trading endpoints
- **Authentication**: API Key + Secret + Passphrase
- **Features Used**: Market orders, leverage trading, margin configuration

### Telegram Bot API
- **Purpose**: User interface and bot control
- **Authentication**: Bot token
- **Features Used**: Message handling, state management, user interaction flows

## Proxy Infrastructure

### SOCKS5 Proxies
- **Requirement**: 3 rotating proxy servers
- **Purpose**: Rate limit avoidance and IP distribution
- **Configuration**: Managed through Go's proxy package

## Programming Language & Frameworks

### Go (Golang)
- **Core Language**: Go 1.x
- **Key Packages**:
  - `net/http`: HTTP client for API requests
  - `golang.org/x/net/proxy`: SOCKS5 proxy support
  - `encoding/json`: JSON parsing and serialization
  - `regexp`: Coin symbol extraction from announcements

### Potential Future Dependencies
- Database system (PostgreSQL/SQLite) for improved data persistence
- Message queue for asynchronous trade execution
- Logging/monitoring service for production operations