package service

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
	"time"

	"yebente/internal/domain"
)

type RateService struct {
	mu           sync.RWMutex
	kesPerBTC    float64
	lastUpdated  time.Time
	httpClient   *http.Client
}

func NewRateService(defaultRate float64) *RateService {
	if defaultRate <= 0 {
		defaultRate = 8500000.0 // Default 8.5M KES / BTC (~$65k)
	}
	s := &RateService{
		kesPerBTC:   defaultRate,
		lastUpdated: time.Now(),
		httpClient:  &http.Client{Timeout: 10 * time.Second},
	}
	// Initial fetch in background
	go s.RefreshRate(context.Background())
	return s
}

func (r *RateService) GetRate() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.kesPerBTC
}

func (r *RateService) GetQuote() *domain.RateQuote {
	r.mu.RLock()
	rate := r.kesPerBTC
	lastUp := r.lastUpdated
	r.mu.RUnlock()

	satsPerKES := (1.0 / rate) * 100000000.0
	return &domain.RateQuote{
		ExchangeRate: rate,
		SatsPerKES:   satsPerKES,
		Timestamp:    lastUp,
	}
}

func (r *RateService) RefreshRate(ctx context.Context) {
	url := "https://api.coingecko.com/api/v3/simple/price?ids=bitcoin&vs_currencies=kes,usd"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return
	}
	req.Header.Set("User-Agent", "Yebente-Gateway/1.0")

	res, err := r.httpClient.Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()

	if res.StatusCode != http.StatusOK {
		return
	}

	var data struct {
		Bitcoin struct {
			KES float64 `json:"kes"`
			USD float64 `json:"usd"`
		} `json:"bitcoin"`
	}

	if err := json.NewDecoder(res.Body).Decode(&data); err == nil && data.Bitcoin.KES > 0 {
		r.mu.Lock()
		r.kesPerBTC = data.Bitcoin.KES
		r.lastUpdated = time.Now()
		r.mu.Unlock()
		log.Printf("Live Bitcoin market rate updated: 1 BTC = %.2f KES ($%.2f USD)", data.Bitcoin.KES, data.Bitcoin.USD)
	}
}

// ConvertKESToSats converts Kenyan Shillings (KES) to Satoshis (1 BTC = 100,000,000 sats)
func (r *RateService) ConvertKESToSats(amountKES float64) int64 {
	rate := r.GetRate()
	btc := amountKES / rate
	sats := btc * 100000000.0
	return int64(sats + 0.5)
}

// ConvertSatsToKES converts Satoshis to Kenyan Shillings
func (r *RateService) ConvertSatsToKES(amountSats int64) float64 {
	rate := r.GetRate()
	btc := float64(amountSats) / 100000000.0
	kes := btc * rate
	return float64(int64(kes*100+0.5)) / 100.0
}
