package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type DarajaServiceInterface interface {
	GetAccessToken(ctx context.Context) (string, error)
	TriggerSTKPush(ctx context.Context, phone string, amountKES int64, accountReference string) (*STKPushResponse, error)
}

type DarajaService struct {
	ConsumerKey       string
	ConsumerSecret    string
	Passkey           string
	BusinessShortCode string
	CallbackURL       string
	BaseURL           string
	HTTPClient        *http.Client
}

type STKPushResponse struct {
	MerchantRequestID   string `json:"MerchantRequestID"`
	CheckoutRequestID   string `json:"CheckoutRequestID"`
	ResponseCode        string `json:"ResponseCode"`
	ResponseDescription string `json:"ResponseDescription"`
	CustomerMessage     string `json:"CustomerMessage"`
}

func NewDarajaService(baseURL, consumerKey, consumerSecret, passkey, shortCode, callbackURL string) *DarajaService {
	return &DarajaService{
		BaseURL:           strings.TrimSuffix(baseURL, "/"),
		ConsumerKey:       consumerKey,
		ConsumerSecret:    consumerSecret,
		Passkey:           passkey,
		BusinessShortCode: shortCode,
		CallbackURL:       callbackURL,
		HTTPClient:        &http.Client{Timeout: 30 * time.Second},
	}
}

func (d *DarajaService) getClient() *http.Client {
	if d.HTTPClient == nil {
		d.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return d.HTTPClient
}

func (d *DarajaService) GetAccessToken(ctx context.Context) (string, error) {
	url := fmt.Sprintf("%s/oauth/v1/generate?grant_type=client_credentials", d.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}

	auth := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", d.ConsumerKey, d.ConsumerSecret)))
	req.Header.Set("Authorization", "Basic "+auth)

	resp, err := d.getClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("daraja auth failed with status code %d", resp.StatusCode)
	}

	var authResp struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&authResp); err != nil {
		return "", fmt.Errorf("failed to decode auth response: %w", err)
	}
	return authResp.AccessToken, nil
}

func (d *DarajaService) TriggerSTKPush(ctx context.Context, phone string, amountKES int64, accountReference string) (*STKPushResponse, error) {
	token, err := d.GetAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed auth: %w", err)
	}

	// Format phone number: Remove leading '+' if present
	formattedPhone := strings.TrimPrefix(strings.TrimSpace(phone), "+")

	timestamp := time.Now().Format("20060102150405")
	password := base64.StdEncoding.EncodeToString([]byte(d.BusinessShortCode + d.Passkey + timestamp))

	payload := map[string]interface{}{
		"BusinessShortCode": d.BusinessShortCode,
		"Password":          password,
		"Timestamp":         timestamp,
		"TransactionType":   "CustomerPayBillOnline",
		"Amount":            amountKES,
		"PartyA":            formattedPhone, // Format 2547XXXXXXXX
		"PartyB":            d.BusinessShortCode,
		"PhoneNumber":       formattedPhone,
		"CallBackURL":       d.CallbackURL,
		"AccountReference":  accountReference,
		"TransactionDesc":   "Yebente Lightning Settlement",
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal STK payload: %w", err)
	}

	url := fmt.Sprintf("%s/mpesa/stkpush/v1/processrequest", d.BaseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("failed to create STK request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	res, err := d.getClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute STK push request: %w", err)
	}
	defer res.Body.Close()

	var stkResp STKPushResponse
	if err := json.NewDecoder(res.Body).Decode(&stkResp); err != nil {
		return nil, fmt.Errorf("failed to decode STK response: %w", err)
	}
	return &stkResp, nil
}
