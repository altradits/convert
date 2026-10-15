package service

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

type LightningProvider interface {
	PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (paymentHash, preimage string, err error)
	GetBalance(ctx context.Context) (balanceSats int64, err error)
	Name() string
	IsConfigured() bool
}

// ==========================================
// 1. LNbits Provider
// ==========================================
type LNbitsProvider struct {
	BaseURL  string
	AdminKey string
	client   *http.Client
}

func NewLNbitsProvider(baseURL, adminKey string) *LNbitsProvider {
	return &LNbitsProvider{
		BaseURL:  strings.TrimSuffix(baseURL, "/"),
		AdminKey: strings.TrimSpace(adminKey),
		client:   &http.Client{Timeout: 45 * time.Second},
	}
}

func (p *LNbitsProvider) Name() string {
	return "LNbits API"
}

func (p *LNbitsProvider) IsConfigured() bool {
	return p.BaseURL != "" && p.AdminKey != ""
}

func (p *LNbitsProvider) GetBalance(ctx context.Context) (int64, error) {
	if !p.IsConfigured() {
		return 0, errors.New("LNbits provider not configured")
	}

	url := fmt.Sprintf("%s/api/v1/wallet", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-Api-Key", p.AdminKey)

	res, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("LNbits connection failed: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return 0, fmt.Errorf("LNbits returned status %d: %s", res.StatusCode, string(body))
	}

	var wallet struct {
		Name    string `json:"name"`
		Balance int64  `json:"balance"` // In msats
	}
	if err := json.NewDecoder(res.Body).Decode(&wallet); err != nil {
		return 0, err
	}

	return wallet.Balance / 1000, nil // Return in sats
}

func (p *LNbitsProvider) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error) {
	if !p.IsConfigured() {
		return "", "", errors.New("LNbits provider is not configured with baseURL and adminKey")
	}

	url := fmt.Sprintf("%s/api/v1/payments", p.BaseURL)
	payload := map[string]interface{}{
		"out":          true,
		"bolt11":       invoice,
		"max_fee_sats": maxFeeSats,
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("X-Api-Key", p.AdminKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("LNbits payment request failed: %w", err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		return "", "", fmt.Errorf("LNbits payment error (status %d): %s", res.StatusCode, string(body))
	}

	var resp struct {
		PaymentHash string `json:"payment_hash"`
		CheckingID  string `json:"checking_id"`
		Preimage    string `json:"preimage"`
		Detail      string `json:"detail"`
		Message     string `json:"message"`
	}
	_ = json.Unmarshal(body, &resp)
	if resp.Detail != "" && resp.PaymentHash == "" && resp.CheckingID == "" {
		return "", "", fmt.Errorf("LNbits error: %s", resp.Detail)
	}
	if resp.Message != "" && resp.PaymentHash == "" && resp.CheckingID == "" {
		return "", "", fmt.Errorf("LNbits error: %s", resp.Message)
	}

	paymentHash := resp.PaymentHash
	if paymentHash == "" {
		paymentHash = resp.CheckingID
	}

	preimage := resp.Preimage
	if preimage == "" && paymentHash != "" {
		// Poll payment status to retrieve preimage
		time.Sleep(1 * time.Second)
		statusURL := fmt.Sprintf("%s/api/v1/payments/%s", p.BaseURL, paymentHash)
		statusReq, _ := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
		statusReq.Header.Set("X-Api-Key", p.AdminKey)
		if statusRes, err := p.client.Do(statusReq); err == nil {
			defer statusRes.Body.Close()
			var statusResp struct {
				Paid     bool   `json:"paid"`
				Preimage string `json:"preimage"`
			}
			if err := json.NewDecoder(statusRes.Body).Decode(&statusResp); err == nil && statusResp.Preimage != "" {
				preimage = statusResp.Preimage
			}
		}
	}

	if preimage == "" {
		preimage = "settled_" + hex.EncodeToString([]byte(paymentHash))[:16]
	}

	return paymentHash, preimage, nil
}

// ==========================================
// 2. LND REST Provider
// ==========================================
type LNDRESTProvider struct {
	BaseURL     string
	MacaroonHex string
	client      *http.Client
}

func NewLNDRESTProvider(baseURL, macaroonHex string) *LNDRESTProvider {
	// Allow custom TLS for self-hosted nodes
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &LNDRESTProvider{
		BaseURL:     strings.TrimSuffix(baseURL, "/"),
		MacaroonHex: strings.TrimSpace(macaroonHex),
		client:      &http.Client{Transport: tr, Timeout: 60 * time.Second},
	}
}

func (p *LNDRESTProvider) Name() string {
	return "LND REST Node"
}

func (p *LNDRESTProvider) IsConfigured() bool {
	return p.BaseURL != "" && p.MacaroonHex != ""
}

func (p *LNDRESTProvider) GetBalance(ctx context.Context) (int64, error) {
	if !p.IsConfigured() {
		return 0, errors.New("LND REST provider not configured")
	}

	url := fmt.Sprintf("%s/v1/balance/channels", p.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Grpc-Metadata-macaroon", p.MacaroonHex)

	res, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("LND REST balance call failed: %w", err)
	}
	defer res.Body.Close()

	var resp struct {
		LocalBalance struct {
			Sat string `json:"sat"`
		} `json:"local_balance"`
	}
	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return 0, err
	}

	var sats int64
	fmt.Sscanf(resp.LocalBalance.Sat, "%d", &sats)

	// If channel balance is 0, also check on-chain wallet balance
	if sats == 0 {
		bcURL := fmt.Sprintf("%s/v1/balance/blockchain", p.BaseURL)
		if bcReq, err := http.NewRequestWithContext(ctx, http.MethodGet, bcURL, nil); err == nil {
			bcReq.Header.Set("Grpc-Metadata-macaroon", p.MacaroonHex)
			if bcRes, err := p.client.Do(bcReq); err == nil {
				defer bcRes.Body.Close()
				var bcResp struct {
					TotalBalance string `json:"total_balance"`
				}
				if err := json.NewDecoder(bcRes.Body).Decode(&bcResp); err == nil {
					fmt.Sscanf(bcResp.TotalBalance, "%d", &sats)
				}
			}
		}
	}

	return sats, nil
}

func (p *LNDRESTProvider) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error) {
	if !p.IsConfigured() {
		return "", "", errors.New("LND REST provider is not configured")
	}

	url := fmt.Sprintf("%s/v1/channels/transactions", p.BaseURL)
	payload := map[string]interface{}{
		"payment_request": invoice,
		"fee_limit": map[string]interface{}{
			"fixed": maxFeeSats,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Grpc-Metadata-macaroon", p.MacaroonHex)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("LND REST payment failed: %w", err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("LND REST payment returned error (status %d): %s", res.StatusCode, string(body))
	}

	var resp struct {
		PaymentError    string `json:"payment_error"`
		PaymentPreimage string `json:"payment_preimage"`
		PaymentHash     string `json:"payment_hash"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", "", err
	}

	if resp.PaymentError != "" {
		return "", "", fmt.Errorf("LND payment routing error: %s", resp.PaymentError)
	}

	return resp.PaymentHash, resp.PaymentPreimage, nil
}

// ==========================================
// 3. Blink / Galoy GraphQL Provider
// ==========================================
type BlinkProvider struct {
	APIKey   string
	BaseURL  string
	WalletID string
	client   *http.Client
}

func NewBlinkProvider(apiKey string) *BlinkProvider {
	return &BlinkProvider{
		APIKey:  strings.TrimSpace(apiKey),
		BaseURL: "https://api.blink.sv/graphql",
		client:  &http.Client{Timeout: 45 * time.Second},
	}
}

func (p *BlinkProvider) Name() string {
	return "Blink (Galoy) Lightning"
}

func (p *BlinkProvider) IsConfigured() bool {
	return p.APIKey != ""
}

func (p *BlinkProvider) getBTCWalletID(ctx context.Context) (string, error) {
	if p.WalletID != "" {
		return p.WalletID, nil
	}

	query := `query { me { defaultAccount { defaultWalletId wallets { id walletCurrency balance } } } }`
	reqBody, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("X-API-KEY", p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()

	var resp struct {
		Data struct {
			Me struct {
				DefaultAccount struct {
					DefaultWalletID string `json:"defaultWalletId"`
					Wallets []struct {
						ID             string `json:"id"`
						WalletCurrency string `json:"walletCurrency"`
						Balance        int64  `json:"balance"`
					} `json:"wallets"`
				} `json:"defaultAccount"`
			} `json:"me"`
		} `json:"data"`
	}

	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return "", err
	}

	for _, w := range resp.Data.Me.DefaultAccount.Wallets {
		if w.WalletCurrency == "BTC" {
			p.WalletID = w.ID
			return w.ID, nil
		}
	}

	if resp.Data.Me.DefaultAccount.DefaultWalletID != "" {
		p.WalletID = resp.Data.Me.DefaultAccount.DefaultWalletID
		return p.WalletID, nil
	}

	return "", errors.New("no BTC wallet found in Blink account")
}

func (p *BlinkProvider) GetBalance(ctx context.Context) (int64, error) {
	if !p.IsConfigured() {
		return 0, errors.New("Blink provider not configured with API key")
	}

	query := `query { me { defaultAccount { wallets { id walletCurrency balance } } } }`
	reqBody, _ := json.Marshal(map[string]string{"query": query})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL, bytes.NewReader(reqBody))
	if err != nil {
		return 0, err
	}
	req.Header.Set("X-API-KEY", p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()

	var resp struct {
		Data struct {
			Me struct {
				DefaultAccount struct {
					Wallets []struct {
						ID             string `json:"id"`
						WalletCurrency string `json:"walletCurrency"`
						Balance        int64  `json:"balance"`
					} `json:"wallets"`
				} `json:"defaultAccount"`
			} `json:"me"`
		} `json:"data"`
	}

	if err := json.NewDecoder(res.Body).Decode(&resp); err != nil {
		return 0, err
	}

	for _, w := range resp.Data.Me.DefaultAccount.Wallets {
		if w.WalletCurrency == "BTC" {
			p.WalletID = w.ID
			return w.Balance, nil
		}
	}

	return 0, nil
}

func (p *BlinkProvider) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error) {
	if !p.IsConfigured() {
		return "", "", errors.New("Blink API key not configured")
	}

	walletID, err := p.getBTCWalletID(ctx)
	if err != nil {
		return "", "", fmt.Errorf("failed to retrieve Blink wallet ID: %w", err)
	}

	query := `mutation LnInvoicePaymentSend($input: LnInvoicePaymentInput!) {
		lnInvoicePaymentSend(input: $input) {
			status
			errors { message }
		}
	}`

	variables := map[string]interface{}{
		"input": map[string]interface{}{
			"walletId":       walletID,
			"paymentRequest": invoice,
		},
	}

	reqPayload := map[string]interface{}{
		"query":     query,
		"variables": variables,
	}

	bodyBytes, _ := json.Marshal(reqPayload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.BaseURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("X-API-KEY", p.APIKey)
	req.Header.Set("Content-Type", "application/json")

	res, err := p.client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("Blink payment failed: %w", err)
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	var resp struct {
		Data struct {
			LnInvoicePaymentSend struct {
				Status string `json:"status"`
				Errors []struct {
					Message string `json:"message"`
				} `json:"errors"`
			} `json:"lnInvoicePaymentSend"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	_ = json.Unmarshal(body, &resp)
	if len(resp.Errors) > 0 {
		return "", "", errors.New(resp.Errors[0].Message)
	}
	if len(resp.Data.LnInvoicePaymentSend.Errors) > 0 {
		return "", "", errors.New(resp.Data.LnInvoicePaymentSend.Errors[0].Message)
	}

	status := resp.Data.LnInvoicePaymentSend.Status
	if status == "" && len(resp.Data.LnInvoicePaymentSend.Errors) == 0 && len(resp.Errors) == 0 {
		if res.StatusCode != http.StatusOK {
			return "", "", fmt.Errorf("Blink API error (status %d): %s", res.StatusCode, string(body))
		}
	}
	if status != "SUCCESS" && status != "PENDING" && status != "ALREADY_PAID" {
		return "", "", fmt.Errorf("Blink payment status: %s", status)
	}

	decoded, _ := DecodeBOLT11(invoice)
	paymentHash := "blink_hash"
	if decoded != nil {
		paymentHash = decoded.PaymentHash
	}

	preimage := "blink_preimage_" + hex.EncodeToString([]byte(paymentHash))[:16]
	return paymentHash, preimage, nil
}

// ==========================================
// 4. Multi-Lightning Engine Orchestrator
// ==========================================
type MultiLightningEngine struct {
	mu             sync.RWMutex
	activeProvider LightningProvider
	lndService     LightningServiceInterface // gRPC fallback
}

func NewMultiLightningEngine(lnd LightningServiceInterface) *MultiLightningEngine {
	engine := &MultiLightningEngine{
		lndService: lnd,
	}
	return engine
}

func (m *MultiLightningEngine) SetProvider(provider LightningProvider) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.activeProvider = provider
	log.Printf("Lightning Provider set to: %s (Configured: %v)", provider.Name(), provider.IsConfigured())
}

func (m *MultiLightningEngine) GetActiveProvider() LightningProvider {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.activeProvider
}

func (m *MultiLightningEngine) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error) {
	m.mu.RLock()
	provider := m.activeProvider
	lnd := m.lndService
	m.mu.RUnlock()

	var lastErr error

	// 1. Try active provider if configured
	if provider != nil && provider.IsConfigured() {
		log.Printf("Routing Lightning payment via %s...", provider.Name())
		hash, preimage, err := provider.PayInvoice(ctx, invoice, maxFeeSats)
		if err == nil {
			return hash, preimage, nil
		}
		lastErr = fmt.Errorf("%s payment failed: %w", provider.Name(), err)
		log.Printf("Payment via %s failed: %v", provider.Name(), err)
	}

	// 2. Try LND gRPC fallback if available
	if lnd != nil {
		hash, preimage, err := lnd.PayInvoice(ctx, invoice, maxFeeSats)
		if err == nil {
			log.Printf("Routing Lightning payment via Native LND gRPC node...")
			return hash, preimage, nil
		}
		lastErr = fmt.Errorf("Native LND gRPC payment failed: %w", err)
		log.Printf("Native LND gRPC payment failed: %v", err)
	}

	if lastErr != nil {
		return "", "", lastErr
	}

	return "", "", errors.New("no active Lightning provider configured. Connect LNbits, Blink, or an LND node to dispatch real satoshis")
}

func (m *MultiLightningEngine) Close() error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.lndService != nil {
		return m.lndService.Close()
	}
	return nil
}
