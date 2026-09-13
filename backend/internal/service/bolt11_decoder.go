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

const bech32Charset = "qpzry9x8gf2tvdw0s3jn54khce6mua7l"

type DestinationType string

const (
	DestinationBOLT11          DestinationType = "BOLT11_INVOICE"
	DestinationLNURL           DestinationType = "LNURL_PAY"
	DestinationLightningAddress DestinationType = "LIGHTNING_ADDRESS"
	DestinationUnknown         DestinationType = "UNKNOWN"
)

type DestinationInfo struct {
	Type        DestinationType `json:"type"`
	CleanTarget string          `json:"clean_target"`
	Network     string          `json:"network,omitempty"`
	ResolvedURL string          `json:"resolved_url,omitempty"`
	IsValid     bool            `json:"is_valid"`
}

type LightningAddressResolution struct {
	Type           DestinationType `json:"type"`
	Target         string          `json:"target"`
	ResolvedURL    string          `json:"resolved_url,omitempty"`
	MinSats        int64           `json:"min_sats"`
	MaxSats        int64           `json:"max_sats"`
	Description    string          `json:"description"`
	CommentAllowed int             `json:"comment_allowed,omitempty"`
	PaymentRequest string          `json:"payment_request,omitempty"`
	Decoded        *DecodedInvoice `json:"decoded,omitempty"`
}

// ConvertBits converts a byte slice from one bit width to another
func convertBits(data []byte, fromBits, toBits uint8, pad bool) ([]byte, error) {
	acc := uint32(0)
	bits := uint8(0)
	var ret []byte
	maxv := uint32((1 << toBits) - 1)
	maxAcc := uint32((1 << (fromBits + toBits - 1)) - 1)

	for _, val := range data {
		if val>>fromBits != 0 {
			return nil, errors.New("invalid data value in convertBits")
		}
		acc = ((acc << fromBits) | uint32(val)) & maxAcc
		bits += fromBits
		for bits >= toBits {
			bits -= toBits
			ret = append(ret, byte((acc>>bits)&maxv))
		}
	}

	if pad {
		if bits > 0 {
			ret = append(ret, byte((acc<<(toBits-bits))&maxv))
		}
	}

	return ret, nil
}

// DecodeLNURL converts bech32 encoded lnurl1... strings or raw lnurl URLs into standard https endpoints
func DecodeLNURL(input string) (string, error) {
	clean := strings.TrimSpace(input)
	if strings.HasPrefix(strings.ToLower(clean), "lightning:") {
		clean = clean[10:]
	}
	lower := strings.ToLower(clean)

	if strings.HasPrefix(lower, "lnurlp://") {
		return "https://" + clean[9:], nil
	}
	if strings.HasPrefix(lower, "lnurlw://") {
		return "https://" + clean[9:], nil
	}
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return clean, nil
	}

	if !strings.HasPrefix(lower, "lnurl1") {
		return "", errors.New("invalid lnurl: must start with lnurl1, lnurlp://, or https://")
	}

	// Find separator '1' (for lnurl1... it is the first '1' at index 5)
	sepIdx := 5
	if len(lower) <= sepIdx || lower[sepIdx] != '1' {
		sepIdx = strings.Index(lower, "1")
	}
	if sepIdx == -1 || sepIdx+1 >= len(lower) {
		return "", errors.New("invalid bech32 lnurl separator")
	}

	dataPart := lower[sepIdx+1:]
	var data5 []byte
	for _, ch := range dataPart {
		idx := strings.IndexRune(bech32Charset, ch)
		if idx == -1 {
			return "", fmt.Errorf("invalid bech32 character: %c", ch)
		}
		data5 = append(data5, byte(idx))
	}

	// Drop checksum if present (standard 6 chars)
	if len(data5) > 6 {
		data5 = data5[:len(data5)-6]
	}

	data8, err := convertBits(data5, 5, 8, false)
	if err != nil {
		return "", fmt.Errorf("failed to convert bech32 bits: %w", err)
	}

	decodedURL := string(data8)
	if !strings.HasPrefix(decodedURL, "http://") && !strings.HasPrefix(decodedURL, "https://") {
		return "", fmt.Errorf("decoded lnurl is not a valid http/https url: %s", decodedURL)
	}

	return decodedURL, nil
}

// ClassifyDestination checks whether the destination is BOLT11, Lightning Address, or LNURL
func ClassifyDestination(input string) *DestinationInfo {
	clean := strings.TrimSpace(input)
	if strings.HasPrefix(strings.ToLower(clean), "lightning:") {
		clean = clean[10:]
	}
	lower := strings.ToLower(clean)

	// 1. BOLT11 Invoice
	if strings.HasPrefix(lower, "lnbc") || strings.HasPrefix(lower, "lntb") || strings.HasPrefix(lower, "lnbcrt") || strings.HasPrefix(lower, "lnsb") {
		net := "mainnet"
		if strings.HasPrefix(lower, "lntb") {
			net = "testnet"
		} else if strings.HasPrefix(lower, "lnbcrt") {
			net = "regtest"
		} else if strings.HasPrefix(lower, "lnsb") {
			net = "simnet"
		}
		return &DestinationInfo{
			Type:        DestinationBOLT11,
			CleanTarget: clean,
			Network:     net,
			IsValid:     true,
		}
	}

	// 2. Lightning Address (user@domain.com)
	if strings.Contains(clean, "@") && !strings.Contains(clean, "/") && !strings.HasPrefix(lower, "ln") {
		parts := strings.Split(clean, "@")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" && strings.Contains(parts[1], ".") {
			url := fmt.Sprintf("https://%s/.well-known/lnurlp/%s", parts[1], parts[0])
			return &DestinationInfo{
				Type:        DestinationLightningAddress,
				CleanTarget: clean,
				ResolvedURL: url,
				IsValid:     true,
			}
		}
	}

	// 3. Bech32 LNURL or raw LNURL URL
	if strings.HasPrefix(lower, "lnurl1") || strings.HasPrefix(lower, "lnurlp://") || strings.HasPrefix(lower, "lnurlw://") || (strings.HasPrefix(lower, "https://") && strings.Contains(lower, "lnurl")) {
		decodedURL, err := DecodeLNURL(clean)
		if err == nil {
			return &DestinationInfo{
				Type:        DestinationLNURL,
				CleanTarget: clean,
				ResolvedURL: decodedURL,
				IsValid:     true,
			}
		}
	}

	return &DestinationInfo{
		Type:        DestinationUnknown,
		CleanTarget: clean,
		IsValid:     false,
	}
}

// ResolveLNURLOrAddress handles both human-readable Lightning addresses (user@domain.com) and LNURL-pay endpoints (lnurl1...)
func ResolveLNURLOrAddress(ctx context.Context, input string, amountSats int64) (*LightningAddressResolution, error) {
	dest := ClassifyDestination(input)
	var endpointURL string
	var destType DestinationType = dest.Type

	if dest.Type == DestinationLightningAddress {
		endpointURL = dest.ResolvedURL
	} else if dest.Type == DestinationLNURL {
		endpointURL = dest.ResolvedURL
	} else {
		// Attempt direct LNURL decode
		url, err := DecodeLNURL(input)
		if err == nil {
			endpointURL = url
			destType = DestinationLNURL
		} else {
			// Try as lightning address fallback
			clean := strings.TrimSpace(input)
			parts := strings.Split(clean, "@")
			if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
				endpointURL = fmt.Sprintf("https://%s/.well-known/lnurlp/%s", parts[1], parts[0])
				destType = DestinationLightningAddress
			} else {
				return nil, fmt.Errorf("unsupported destination: expected BOLT11 invoice, LNURL (lnurl1...), or Lightning Address (name@domain.com)")
			}
		}
	}

	// 1. Fetch LNURL metadata
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 12 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach LNURL endpoint %s: %w", endpointURL, err)
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(res.Body)
		return nil, fmt.Errorf("LNURL endpoint returned HTTP %d: %s", res.StatusCode, string(body))
	}

	var lnurlResp struct {
		Tag            string `json:"tag"`
		Callback       string `json:"callback"`
		MinSendable    int64  `json:"minSendable"` // In msats
		MaxSendable    int64  `json:"maxSendable"` // In msats
		Metadata       string `json:"metadata"`
		CommentAllowed int    `json:"commentAllowed"`
		Status         string `json:"status"`
		Reason         string `json:"reason"`
	}

	if err := json.NewDecoder(res.Body).Decode(&lnurlResp); err != nil {
		return nil, fmt.Errorf("failed to parse LNURL response: %w", err)
	}

	if lnurlResp.Status == "ERROR" {
		return nil, fmt.Errorf("LNURL service error: %s", lnurlResp.Reason)
	}

	minSats := lnurlResp.MinSendable / 1000
	maxSats := lnurlResp.MaxSendable / 1000
	if minSats == 0 && lnurlResp.MinSendable > 0 {
		minSats = 1
	}

	desc := "Payment via LNURL"
	if lnurlResp.Metadata != "" {
		var meta [][]string
		if err := json.Unmarshal([]byte(lnurlResp.Metadata), &meta); err == nil {
			for _, item := range meta {
				if len(item) == 2 && item[0] == "text/plain" {
					desc = item[1]
					break
				}
			}
		}
	}

	result := &LightningAddressResolution{
		Type:           destType,
		Target:         strings.TrimSpace(input),
		ResolvedURL:    endpointURL,
		MinSats:        minSats,
		MaxSats:        maxSats,
		Description:    desc,
		CommentAllowed: lnurlResp.CommentAllowed,
	}

	// 2. If amount specified, fetch live BOLT11 invoice from callback
	if amountSats > 0 {
		amountMSats := amountSats * 1000
		if lnurlResp.MinSendable > 0 && amountMSats < lnurlResp.MinSendable {
			return nil, fmt.Errorf("amount %d sats is below minimum allowed %d sats", amountSats, minSats)
		}
		if lnurlResp.MaxSendable > 0 && amountMSats > lnurlResp.MaxSendable {
			return nil, fmt.Errorf("amount %d sats exceeds maximum allowed %d sats", amountSats, maxSats)
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
			return nil, fmt.Errorf("failed to decode invoice callback response: %w", err)
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

// ResolveLightningAddress is an alias for ResolveLNURLOrAddress for backwards compatibility
func ResolveLightningAddress(ctx context.Context, address string, amountSats int64) (*LightningAddressResolution, error) {
	return ResolveLNURLOrAddress(ctx, address, amountSats)
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
