# Overview

This is a cryptocurrency trading bot that monitors Upbit exchange for new coin listing announcements and automatically executes trades on Bitget exchange. The system uses proxy rotation to avoid rate limits, detects new coin listings through API polling with ETag-based change detection, and manages user configurations for automated trading with configurable leverage and margin settings.

# User Preferences

Preferred communication style: Simple, everyday language.

# System Architecture

## Core Components

### 1. Listing Detection System
- **Problem**: Need to detect new cryptocurrency listings on Upbit exchange in real-time without hitting rate limits
- **Solution**: Proxy rotation with ETag-based polling
  - Uses 3 SOCKS5 proxies in round-robin fashion
  - 1-second polling interval (each proxy used every 3 seconds)
  - ETag-based caching to minimize bandwidth and server load
  - Regex-based ticker symbol extraction from announcement titles
- **Rationale**: Proxy rotation prevents rate limiting (max 1200 requests/hour per IP), while ETag reduces unnecessary data transfer

### 2. Trading Execution Engine
- **Problem**: Execute trades automatically when new listings are detected
- **Solution**: Bitget API integration with user-specific configurations
  - Authenticated API calls using API key, secret, and passphrase
  - Configurable margin (USDT amount) and leverage settings per user
  - Order placement on Bitget futures/spot markets
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
- Round-robin rotation across 3 SOCKS5 proxies
- 3-second interval per proxy (1-second global polling)
- Built-in rate limit compliance (400 requests/hour per proxy)

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