package main

import (
        "bytes"
        "crypto/hmac"
        "crypto/sha256"
        "encoding/base64"
        "encoding/json"
        "fmt"
        "io"
        "net/http"
        "net/url"
        "strconv"
        "strings"
        "sync"
        "time"
)

type BalanceCache struct {
        Available  float64
        LastUpdate time.Time
        IsStale    bool
        mutex      sync.RWMutex
        api        *BitgetAPI
}

type BitgetAPI struct {
        APIKey     string
        APISecret  string
        Passphrase string
        BaseURL    string
        Client     *http.Client
        Cache      *BalanceCache
}

type OrderSide string

const (
        OrderSideBuy  OrderSide = "buy"
        OrderSideSell OrderSide = "sell"
)

type OrderType string

const (
        OrderTypeMarket OrderType = "market"
        OrderTypeLimit  OrderType = "limit"
)

type PositionSide string

const (
        PositionSideLong  PositionSide = "long"
        PositionSideShort PositionSide = "short"
)

type BitgetPosition struct {
        PositionID       string `json:"positionId"`
        Symbol           string `json:"symbol"`
        Size             string `json:"size"`
        Side             string `json:"side"`
        MarkPrice        string `json:"markPrice"`
        EntryPrice       string `json:"entryPrice"`
        UnrealizedPL     string `json:"unrealizedPL"`
        Leverage         string `json:"leverage"`
        MarginSize       string `json:"marginSize"`
        LiquidationPrice string `json:"liquidationPrice"`
        CreatedAt        string `json:"cTime"`
        UpdatedAt        string `json:"uTime"`
}

type OrderRequest struct {
        Symbol      string    `json:"symbol"`
        ProductType string    `json:"productType"`
        MarginMode  string    `json:"marginMode"`
        MarginCoin  string    `json:"marginCoin"`
        Size        string    `json:"size"`
        Side        OrderSide `json:"side"`
        TradeSide   string    `json:"tradeSide,omitempty"`
        OrderType   OrderType `json:"orderType"`
        Price       string    `json:"price,omitempty"`
        Force       string    `json:"force,omitempty"`
        ClientOID   string    `json:"clientOid,omitempty"`
        ReduceOnly  string    `json:"reduceOnly,omitempty"`
}

type OrderResponse struct {
        OrderID    string  `json:"orderId"`
        ClientOID  string  `json:"clientOid"`
        OpenPrice  float64 `json:"-"`
        Symbol     string  `json:"-"`
        Size       float64 `json:"-"`
        MarginUSDT float64 `json:"-"`
        Leverage   int     `json:"-"`
}

type APIResponse struct {
        Code      string      `json:"code"`
        Message   string      `json:"msg"`
        RequestID interface{} `json:"requestTime"`
        Data      interface{} `json:"data"`
}

type AccountBalance struct {
        MarginCoin        string `json:"marginCoin"`
        Locked            string `json:"locked"`
        Available         string `json:"available"`
        CrossMaxAvailable string `json:"crossMaxAvailable"`
        FixedMaxAvailable string `json:"fixedMaxAvailable"`
        MaxTransferOut    string `json:"maxTransferOut"`
        Equity            string `json:"equity"`
        USDTEquity        string `json:"usdtEquity"`
        BonusAmount       string `json:"bonusAmount"`
}

func NewBitgetAPI(apiKey, apiSecret, passphrase string) *BitgetAPI {
        api := &BitgetAPI{
                APIKey:     apiKey,
                APISecret:  apiSecret,
                Passphrase: passphrase,
                BaseURL:    "https://api.bitget.com",
                Client: &http.Client{
                        Timeout: 30 * time.Second,
                },
        }

        api.Cache = &BalanceCache{
                Available:  0,
                LastUpdate: time.Time{},
                IsStale:    true,
                api:        api,
        }

        return api
}

func (b *BitgetAPI) sign(timestamp, method, requestPath string, body []byte) string {
        message := timestamp + method + requestPath + string(body)
        mac := hmac.New(sha256.New, []byte(b.APISecret))
        mac.Write([]byte(message))
        return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

func (b *BitgetAPI) makeRequest(method, endpoint string, body interface{}, result interface{}) error {
        return b.makeRequestWithRetry(method, endpoint, nil, body, result)
}

func (b *BitgetAPI) makeRequestWithRetry(method, endpoint string, queryParams map[string]string, body interface{}, result interface{}) error {
        var bodyBytes []byte
        if body != nil {
                var err error
                bodyBytes, err = json.Marshal(body)
                if err != nil {
                        return fmt.Errorf("failed to marshal request body: %w", err)
                }
        }

        timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
        requestPath := endpoint
        if len(queryParams) > 0 {
                params := make([]string, 0, len(queryParams))
                for k, v := range queryParams {
                        params = append(params, fmt.Sprintf("%s=%s", k, v))
                }
                requestPath = endpoint + "?" + strings.Join(params, "&")
        }

        signature := b.sign(timestamp, method, requestPath, bodyBytes)

        url := b.BaseURL + requestPath
        req, err := http.NewRequest(method, url, bytes.NewReader(bodyBytes))
        if err != nil {
                return fmt.Errorf("failed to create request: %w", err)
        }

        req.Header.Set("ACCESS-KEY", b.APIKey)
        req.Header.Set("ACCESS-SIGN", signature)
        req.Header.Set("ACCESS-TIMESTAMP", timestamp)
        req.Header.Set("ACCESS-PASSPHRASE", b.Passphrase)
        req.Header.Set("Content-Type", "application/json")
        req.Header.Set("locale", "en-US")

        resp, err := b.Client.Do(req)
        if err != nil {
                return fmt.Errorf("request failed: %w", err)
        }
        defer resp.Body.Close()

        respBody, err := io.ReadAll(resp.Body)
        if err != nil {
                return fmt.Errorf("failed to read response: %w", err)
        }

        var apiResp APIResponse
        if err := json.Unmarshal(respBody, &apiResp); err != nil {
                return fmt.Errorf("failed to parse API response: %w", err)
        }

        if apiResp.Code != "00000" {
                return fmt.Errorf("API error: %s - %s", apiResp.Code, apiResp.Message)
        }

        if result != nil && apiResp.Data != nil {
                dataBytes, _ := json.Marshal(apiResp.Data)
                if err := json.Unmarshal(dataBytes, result); err != nil {
                        return fmt.Errorf("failed to parse response data: %w", err)
                }
        }

        return nil
}

func (b *BitgetAPI) PlaceOrder(symbol string, side OrderSide, size float64, tradeSide string) (*OrderResponse, error) {
        orderReq := OrderRequest{
                Symbol:      symbol,
                ProductType: "USDT-FUTURES",
                MarginMode:  "isolated",
                MarginCoin:  "USDT",
                Size:        fmt.Sprintf("%.8f", size),
                Side:        side,
                TradeSide:   tradeSide,
                OrderType:   OrderTypeMarket,
                Force:       "gtc",
        }

        endpoint := "/api/v2/mix/order/place-order"
        fmt.Printf("🚀 Placing v2 order: %+v\n", orderReq)

        var orderResp OrderResponse
        err := b.makeRequest("POST", endpoint, orderReq, &orderResp)
        if err != nil {
                fmt.Printf("❌ Order placement failed: %v\n", err)
                return nil, fmt.Errorf("failed to place order: %w", err)
        }

        fmt.Printf("✅ Order placed successfully: %+v\n", orderResp)
        return &orderResp, nil
}

func (b *BitgetAPI) SetLeverage(symbol string, leverage int) error {
        endpoint := "/api/v2/mix/account/set-leverage"
        leverageReq := map[string]interface{}{
                "symbol":      symbol,
                "productType": "USDT-FUTURES",
                "marginCoin":  "USDT",
                "leverage":    strconv.Itoa(leverage),
        }

        return b.makeRequest("POST", endpoint, leverageReq, nil)
}

func (b *BitgetAPI) GetSymbolPrice(symbol string) (float64, error) {
        endpoint := "/api/v2/mix/market/ticker"
        params := map[string]string{
                "symbol":      symbol,
                "productType": "USDT-FUTURES",
        }
        
        fmt.Printf("🔍 Getting price for symbol: %s\n", symbol)
        
        // Build query string
        values := url.Values{}
        for k, v := range params {
                values.Add(k, v)
        }
        queryString := values.Encode()
        
        // Build full URL
        fullURL := b.BaseURL + endpoint + "?" + queryString
        
        // Create HTTP request
        req, err := http.NewRequest("GET", fullURL, nil)
        if err != nil {
                return 0, fmt.Errorf("failed to create request: %w", err)
        }
        
        // Set headers
        timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
        signaturePath := endpoint + "?" + queryString
        
        req.Header.Set("ACCESS-KEY", b.APIKey)
        req.Header.Set("ACCESS-SIGN", b.sign(timestamp, "GET", signaturePath, []byte{}))
        req.Header.Set("ACCESS-PASSPHRASE", b.Passphrase)
        req.Header.Set("ACCESS-TIMESTAMP", timestamp)
        req.Header.Set("locale", "en-US")
        req.Header.Set("Content-Type", "application/json")
        
        // Make request
        resp, err := b.Client.Do(req)
        if err != nil {
                return 0, fmt.Errorf("failed to make request: %w", err)
        }
        defer resp.Body.Close()
        
        // Read response
        respBody, err := io.ReadAll(resp.Body)
        if err != nil {
                return 0, fmt.Errorf("failed to read response: %w", err)
        }
        
        fmt.Printf("🔍 HTTP Status: %d\n", resp.StatusCode)
        fmt.Printf("🔍 API Response received\n")
        
        // Parse response directly without APIResponse wrapper
        var directResponse map[string]interface{}
        if err := json.Unmarshal(respBody, &directResponse); err != nil {
                return 0, fmt.Errorf("failed to parse response: %w", err)
        }
        
        // Check response code
        code, ok := directResponse["code"].(string)
        if !ok || code != "00000" {
                msg, _ := directResponse["msg"].(string)
                return 0, fmt.Errorf("API error: %s - %s", code, msg)
        }
        
        // Parse data array (Bitget returns array, not map!)
        data, ok := directResponse["data"].([]interface{})
        if !ok || len(data) == 0 {
                return 0, fmt.Errorf("invalid response format or no data")
        }
        
        // Get first ticker item
        tickerData, ok := data[0].(map[string]interface{})
        if !ok {
                return 0, fmt.Errorf("invalid ticker data format")
        }
        
        // Get price
        priceStr, ok := tickerData["lastPr"].(string)
        if !ok {
                return 0, fmt.Errorf("lastPr field not found")
        }
        
        price, err := strconv.ParseFloat(priceStr, 64)
        if err != nil {
                return 0, fmt.Errorf("failed to parse price: %w", err)
        }
        
        fmt.Printf("📊 Current price for %s: $%.2f\n", symbol, price)
        return price, nil
}

func (b *BitgetAPI) GetCurrentLeverage(symbol string) (int, error) {
        endpoint := "/api/v2/mix/account/account"
        queryParams := map[string]string{
                "symbol":      symbol,
                "productType": "USDT-FUTURES",
                "marginCoin":  "USDT",
        }

        var accountData map[string]interface{}
        err := b.makeRequestWithRetry("GET", endpoint, queryParams, nil, &accountData)
        if err != nil {
                return 0, err
        }

        leverageStr, ok := accountData["leverage"].(string)
        if !ok {
                return 0, fmt.Errorf("invalid leverage format")
        }

        leverage, err := strconv.Atoi(leverageStr)
        if err != nil {
                return 0, fmt.Errorf("failed to parse leverage: %w", err)
        }

        return leverage, nil
}

func (bc *BalanceCache) HasSufficientBalance(required float64) (bool, error) {
        bc.mutex.RLock()
        if !bc.IsStale && time.Since(bc.LastUpdate) < 5*time.Second {
                sufficient := bc.Available >= required
                bc.mutex.RUnlock()
                return sufficient, nil
        }
        bc.mutex.RUnlock()

        if err := bc.RefreshBalance(); err != nil {
                return false, err
        }

        bc.mutex.RLock()
        defer bc.mutex.RUnlock()
        return bc.Available >= required, nil
}

func (bc *BalanceCache) RefreshBalance() error {
        endpoint := "/api/v2/mix/account/accounts"
        queryParams := map[string]string{
                "productType": "USDT-FUTURES",
        }

        var accounts []AccountBalance
        err := bc.api.makeRequestWithRetry("GET", endpoint, queryParams, nil, &accounts)
        if err != nil {
                return err
        }

        for _, account := range accounts {
                if account.MarginCoin == "USDT" {
                        available, _ := strconv.ParseFloat(account.Available, 64)
                        bc.mutex.Lock()
                        bc.Available = available
                        bc.LastUpdate = time.Now()
                        bc.IsStale = false
                        bc.mutex.Unlock()
                        return nil
                }
        }

        return fmt.Errorf("USDT account not found")
}

func (b *BitgetAPI) OpenLongPosition(symbol string, marginUSDT float64, leverage int) (*OrderResponse, error) {
        fmt.Printf("🚀 Starting position: symbol=%s, user_margin=%.2f USDT, requested_leverage=%dx\n",
                symbol, marginUSDT, leverage)

        originalMargin := marginUSDT
        originalLeverage := leverage

        sufficient, err := b.Cache.HasSufficientBalance(marginUSDT)
        if err != nil {
                return nil, fmt.Errorf("balance check failed: %w", err)
        }
        if !sufficient {
                return nil, fmt.Errorf("insufficient balance: %.2f USDT required, check your account", marginUSDT)
        }

        // PARALLEL EXECUTION for speed - Set leverage + Get price at same time
        type parallelResult struct {
                price float64
                leverageErr error
                priceErr error
        }
        
        resultChan := make(chan parallelResult, 1)
        
        go func() {
                var result parallelResult
                
                // Parallel goroutines
                var wg sync.WaitGroup
                wg.Add(2)
                
                // Set leverage (parallel)
                go func() {
                        defer wg.Done()
                        fmt.Printf("⚡ Setting leverage %dx for %s\n", leverage, symbol)
                        result.leverageErr = b.SetLeverage(symbol, leverage)
                }()
                
                // Get price (parallel)
                go func() {
                        defer wg.Done()
                        result.price, result.priceErr = b.GetSymbolPrice(symbol)
                }()
                
                wg.Wait()
                resultChan <- result
        }()
        
        result := <-resultChan
        
        if result.leverageErr != nil {
                return nil, fmt.Errorf("failed to set leverage: %w", result.leverageErr)
        }
        
        if result.priceErr != nil {
                return nil, fmt.Errorf("failed to get current price: %w", result.priceErr)
        }
        
        currentPrice := result.price
        fmt.Printf("✅ Leverage set & price fetched (parallel execution)")


        positionSizeUSDT := marginUSDT * float64(leverage)
        baseSize := positionSizeUSDT / currentPrice

        fmt.Printf("📊 Position calculation: margin=%.2f USDT, leverage=%dx, position_size=%.2f USDT, price=%.6f, coin_amount=%.8f\n",
                marginUSDT, leverage, positionSizeUSDT, currentPrice, baseSize)

        fmt.Printf("🎯 Placing order: %.8f %s at market price\n", baseSize, symbol)
        orderResp, err := b.PlaceOrder(symbol, OrderSideBuy, baseSize, "open")
        if err != nil {
                return nil, fmt.Errorf("order placement failed: %w", err)
        }

        if orderResp != nil {
                orderResp.OpenPrice = currentPrice
                orderResp.Symbol = symbol
                orderResp.Size = baseSize
                orderResp.MarginUSDT = originalMargin
                orderResp.Leverage = originalLeverage

                fmt.Printf("✅ Position opened successfully!\n")
                fmt.Printf("🏷️ Details: Symbol=%s, Size=%.8f, OpenPrice=%.4f, UserMargin=%.2f, UserLeverage=%dx (Actual=%dx)\n",
                        symbol, baseSize, currentPrice, originalMargin, originalLeverage, leverage)
        }

        return orderResp, nil
}

func (b *BitgetAPI) FlashClosePosition(symbol string, holdSide string) (*OrderResponse, error) {
        endpoint := "/api/v2/mix/order/close-positions"

        closeReq := map[string]interface{}{
                "symbol":      symbol,
                "productType": "USDT-FUTURES",
                "holdSide":    holdSide,
        }

        fmt.Printf("🚨 Flash closing position: %+v\n", closeReq)

        var response map[string]interface{}
        err := b.makeRequestWithRetry("POST", endpoint, nil, closeReq, &response)
        if err != nil {
                fmt.Printf("❌ Flash close failed: %v\n", err)
                return nil, fmt.Errorf("failed to flash close position: %w", err)
        }

        var data map[string]interface{}

        if apiResp, ok := response["data"].(map[string]interface{}); ok {
                data = apiResp
        } else {
                data = response
        }

        fmt.Printf("🔍 Flash close response data: %+v\n", data)

        successList, ok := data["successList"].([]interface{})
        if !ok || len(successList) == 0 {
                if failureList, ok := data["failureList"].([]interface{}); ok && len(failureList) > 0 {
                        failure := failureList[0].(map[string]interface{})
                        errorMsg, _ := failure["errorMsg"].(string)
                        return nil, fmt.Errorf("flash close failed: %s", errorMsg)
                }
                return nil, fmt.Errorf("no successful closes in response")
        }

        success := successList[0].(map[string]interface{})
        orderResp := &OrderResponse{
                OrderID:   fmt.Sprintf("%v", success["orderId"]),
                ClientOID: fmt.Sprintf("%v", success["clientOid"]),
        }

        fmt.Printf("✅ Flash close successful: %+v\n", orderResp)
        return orderResp, nil
}

func (b *BitgetAPI) GetAccountBalance() ([]AccountBalance, error) {
        endpoint := "/api/v2/mix/account/accounts"
        queryParams := map[string]string{
                "productType": "USDT-FUTURES",
        }

        var accounts []AccountBalance
        err := b.makeRequestWithRetry("GET", endpoint, queryParams, nil, &accounts)
        if err != nil {
                return nil, err
        }

        return accounts, nil
}

func (b *BitgetAPI) GetAllPositions() ([]BitgetPosition, error) {
        endpoint := "/api/v2/mix/position/all-position"
        queryParams := map[string]string{
                "productType": "USDT-FUTURES",
                "marginCoin":  "USDT",
        }

        var positions []BitgetPosition
        err := b.makeRequestWithRetry("GET", endpoint, queryParams, nil, &positions)
        if err != nil {
                return nil, err
        }

        return positions, nil
}

func (b *BitgetAPI) CloseAllPositions() (*OrderResponse, error) {
        endpoint := "/api/v2/mix/order/close-positions"

        closeReq := map[string]interface{}{
                "productType": "USDT-FUTURES",
        }

        var response map[string]interface{}
        err := b.makeRequestWithRetry("POST", endpoint, nil, closeReq, &response)
        if err != nil {
                return nil, fmt.Errorf("failed to close all positions: %w", err)
        }

        return &OrderResponse{OrderID: "all_closed"}, nil
}
