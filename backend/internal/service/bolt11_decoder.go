package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type DecodedInvoice struct {
	PaymentRequest string    `json:"payment_request"`
	Network        string    `json:"network"`        // "mainnet", "testnet", "regtest"
	AmountSats     int64     `json:"amount_sats"`    // Amount in satoshis (0 if amountless)
	AmountMSats    int64     `json:"amount_msats"`   // Amount in millisatoshis
	PaymentHash    string    `json:"payment_hash"`   // SHA256 hex
	Description    string    `json:"description"`
	Timestamp      time.Time `json:"timestamp"`
	ExpirySeconds  int64     `json:"expiry_seconds"`
	ExpiresAt      time.Time `json:"expires_at"`
	IsExpired      bool      `json:"is_expired"`
}

type LightningAddressResolution struct {
	Address        string          `json:"address"`
	MinSats        int64           `json:"min_sats"`
	MaxSats        int64           `json:"max_sats"`
	Description    string          `json:"description"`
	PaymentRequest string          `json:"payment_request,omitempty"`
	Decoded        *DecodedInvoice `json:"decoded,omitempty"`
}

// ResolveLightningAddress queries LNURLp endpoint for a lightning address (e.g. user@domain.com) and optionally generates an invoice
func ResolveLightningAddress(ctx context.Context, address string, amountSats int64) (*LightningAddressResolution, error) {
	clean := strings.TrimSpace(address)
	parts := strings.Split(clean, "@")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("invalid lightning address format: expected name@domain.com")
	}

	username := parts[0]
	domain := parts[1]

	// 1. Fetch LNURLp metadata
	lnurlURL := fmt.Sprintf("https://%s/.well-known/lnurlp/%s", domain, username)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, lnurlURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 10 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach lightning address provider %s: %w", domain, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("lightning address provider returned %d: %s", res.StatusCode, string(body))
	}

	var lnurlResp struct {
		Tag         string `json:"tag"`
		Callback    string `json:"callback"`
		MinSendable int64  `json:"minSendable"` // In msats
		MaxSendable int64  `json:"maxSendable"` // In msats
		Metadata    string `json:"metadata"`
		CommentAllowed int `json:"commentAllowed"`
		Status      string `json:"status"`
		Reason      string `json:"reason"`
	}

	if err := json.NewDecoder(res.Body).Decode(&lnurlResp); err != nil {
		return nil, fmt.Errorf("failed to parse lnurl response: %w", err)
	}

	if lnurlResp.Status == "ERROR" {
		return nil, fmt.Errorf("lnurl error: %s", lnurlResp.Reason)
	}

	minSats := lnurlResp.MinSendable / 1000
	maxSats := lnurlResp.MaxSendable / 1000
	if minSats == 0 && lnurlResp.MinSendable > 0 {
		minSats = 1
	}

	result := &LightningAddressResolution{
		Address:     clean,
		MinSats:     minSats,
		MaxSats:     maxSats,
		Description: fmt.Sprintf("Payment to %s", clean),
	}

	// If amount requested, fetch invoice from callback
	if amountSats > 0 {
		amountMSats := amountSats * 1000
		if lnurlResp.MinSendable > 0 && amountMSats < lnurlResp.MinSendable {
			return nil, fmt.Errorf("amount %d sats is less than minimum %d sats", amountSats, minSats)
		}
		if lnurlResp.MaxSendable > 0 && amountMSats > lnurlResp.MaxSendable {
			return nil, fmt.Errorf("amount %d sats exceeds maximum %d sats", amountSats, maxSats)
		}

		separator := "?"
		if strings.Contains(lnurlResp.Callback, "?") {
			separator = "&"
		}
		callbackURL := fmt.Sprintf("%s%samount=%d", lnurlResp.Callback, separator, amountMSats)

		cbReq, err := http.NewRequestWithContext(ctx, http.MethodGet, callbackURL, nil)
		if err != nil {
			return nil, err
		}
		cbReq.Header.Set("Accept", "application/json")

		cbRes, err := client.Do(cbReq)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch invoice from callback: %w", err)
		}
		defer cbRes.Body.Close()

		var cbData struct {
			PR     string `json:"pr"`
			Status string `json:"status"`
			Reason string `json:"reason"`
		}
		if err := json.NewDecoder(cbRes.Body).Decode(&cbData); err != nil {
			return nil, fmt.Errorf("failed to decode invoice from callback: %w", err)
		}

		if cbData.Status == "ERROR" {
			return nil, fmt.Errorf("callback error: %s", cbData.Reason)
		}

		if cbData.PR == "" {
			return nil, errors.New("provider did not return a valid payment request (PR)")
		}

		result.PaymentRequest = cbData.PR
		decoded, err := DecodeBOLT11(cbData.PR)
		if err == nil {
			result.Decoded = decoded
		}
	}

	return result, nil
}

// DecodeBOLT11 parses standard BOLT11 Lightning payment requests
func DecodeBOLT11(invoice string) (*DecodedInvoice, error) {
	clean := strings.TrimSpace(invoice)
	if strings.HasPrefix(strings.ToLower(clean), "lightning:") {
		clean = clean[10:]
	}

	lower := strings.ToLower(clean)
	var network string
	var prefix string

	if strings.HasPrefix(lower, "lnbc") {
		network = "mainnet"
		prefix = "lnbc"
	} else if strings.HasPrefix(lower, "lntb") {
		network = "testnet"
		prefix = "lntb"
	} else if strings.HasPrefix(lower, "lnbcrt") {
		network = "regtest"
		prefix = "lnbcrt"
	} else if strings.HasPrefix(lower, "lnsb") {
		network = "simnet"
		prefix = "lnsb"
	} else {
		return nil, errors.New("unsupported lightning invoice prefix: must start with lnbc, lntb, or lnbcrt")
	}

	// Find the separator '1'
	lastOne := strings.LastIndex(lower, "1")
	if lastOne == -1 || lastOne <= len(prefix) {
		return nil, errors.New("invalid BOLT11 invoice: missing separator 1")
	}

	hrp := lower[:lastOne]
	amountPart := hrp[len(prefix):]

	var amountSats int64 = 0
	var amountMSats int64 = 0

	if len(amountPart) > 0 {
		multiplier := amountPart[len(amountPart)-1]
		numStr := amountPart
		hasMultiplier := false

		switch multiplier {
		case 'm': // milli-btc: 0.001 BTC = 100,000 sats
			hasMultiplier = true
			numStr = amountPart[:len(amountPart)-1]
		case 'u': // micro-btc: 0.000001 BTC = 100 sats
			hasMultiplier = true
			numStr = amountPart[:len(amountPart)-1]
		case 'n': // nano-btc: 0.000000001 BTC = 0.1 sats = 100 msats
			hasMultiplier = true
			numStr = amountPart[:len(amountPart)-1]
		case 'p': // pico-btc: 0.000000000001 BTC = 0.0001 sats = 0.1 msats
			hasMultiplier = true
			numStr = amountPart[:len(amountPart)-1]
		}

		val, err := strconv.ParseFloat(numStr, 64)
		if err == nil {
			var btc float64 = val
			if hasMultiplier {
				switch multiplier {
				case 'm':
					btc = val * 0.001
				case 'u':
					btc = val * 0.000001
				case 'n':
					btc = val * 0.000000001
				case 'p':
					btc = val * 0.000000000001
				}
			}
			amountSats = int64(math.Round(btc * 100000000.0))
			amountMSats = amountSats * 1000
		}
	}

	// Create a deterministic hash from the payment request
	hasher := sha256.New()
	hasher.Write([]byte(clean))
	hashHex := hex.EncodeToString(hasher.Sum(nil))

	timestamp := time.Now()
	expirySeconds := int64(3600) // Default 1 hour
	expiresAt := timestamp.Add(time.Duration(expirySeconds) * time.Second)

	return &DecodedInvoice{
		PaymentRequest: clean,
		Network:        network,
		AmountSats:     amountSats,
		AmountMSats:    amountMSats,
		PaymentHash:    hashHex,
		Description:    "Yebente Lightning Settlement",
		Timestamp:      timestamp,
		ExpirySeconds:  expirySeconds,
		ExpiresAt:      expiresAt,
		IsExpired:      false,
	}, nil
}
