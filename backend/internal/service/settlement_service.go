package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"time"

	"github.com/google/uuid"
	"yebente/internal/domain"
	"yebente/internal/repository/postgres"
)

type SettlementService struct {
	darajaService  DarajaServiceInterface
	lndService     LightningServiceInterface
	paymentRepo    postgres.PaymentRepository
	userRepo       postgres.UserRepository
	exchangeRate   float64 // KES per BTC (e.g. 8,500,000)
}

func NewSettlementService(
	daraja DarajaServiceInterface,
	lnd LightningServiceInterface,
	paymentRepo postgres.PaymentRepository,
	userRepo postgres.UserRepository,
	exchangeRate float64,
) *SettlementService {
	if exchangeRate <= 0 {
		exchangeRate = 8500000.0
	}
	return &SettlementService{
		darajaService: daraja,
		lndService:    lnd,
		paymentRepo:   paymentRepo,
		userRepo:      userRepo,
		exchangeRate:  exchangeRate,
	}
}

// ConvertKESToSats converts Kenyan Shillings (KES) to Satoshis (1 BTC = 100,000,000 sats)
func (s *SettlementService) ConvertKESToSats(amountKES float64) int64 {
	// (amountKES / KES_PER_BTC) * 100,000,000
	btc := amountKES / s.exchangeRate
	sats := btc * 100000000.0
	return int64(math.Round(sats))
}

func (s *SettlementService) GetRateQuote() *domain.RateQuote {
	satsPerKES := (1.0 / s.exchangeRate) * 100000000.0
	return &domain.RateQuote{
		ExchangeRate: s.exchangeRate,
		SatsPerKES:   satsPerKES,
		Timestamp:    time.Now(),
	}
}

// InitiateDeposit triggers STK Push via Daraja and creates a pending transaction record
func (s *SettlementService) InitiateDeposit(ctx context.Context, phone string, amountKES float64, invoice string) (*domain.InitiateDepositResponse, error) {
	// 1. Ensure user exists
	user, err := s.userRepo.CreateOrGetUser(ctx, phone)
	if err != nil {
		log.Printf("Warning: failed to get/create user for phone %s: %v", phone, err)
	}

	var userID *uuid.UUID
	if user != nil {
		userID = &user.ID
	}

	// 2. Calculate satoshis
	amountSats := s.ConvertKESToSats(amountKES)
	if amountSats <= 0 {
		return nil, fmt.Errorf("calculated sats must be greater than 0")
	}

	// 3. Trigger Safaricom Daraja STK Push
	accountRef := "Yebente-" + phone
	stkResp, err := s.darajaService.TriggerSTKPush(ctx, phone, int64(math.Round(amountKES)), accountRef)
	if err != nil {
		return nil, fmt.Errorf("daraja STK push failed: %w", err)
	}

	// 4. Record transaction in database with PENDING_STK status
	tx := &domain.Transaction{
		ID:                uuid.New(),
		UserID:            userID,
		MerchantRequestID: &stkResp.MerchantRequestID,
		CheckoutRequestID: &stkResp.CheckoutRequestID,
		AmountKES:         amountKES,
		AmountSats:        amountSats,
		LightningInvoice:  invoice,
		Status:            domain.StatusPendingSTK,
	}

	if err := s.paymentRepo.CreateTransaction(ctx, tx); err != nil {
		return nil, fmt.Errorf("failed to create pending transaction in database: %w", err)
	}

	return &domain.InitiateDepositResponse{
		TransactionID:     tx.ID,
		CheckoutRequestID: stkResp.CheckoutRequestID,
		CustomerMessage:   stkResp.CustomerMessage,
		Status:            domain.StatusPendingSTK,
	}, nil
}

// ExecuteLightningSettlement pays the invoice via LND and updates database state
func (s *SettlementService) ExecuteLightningSettlement(ctx context.Context, txID uuid.UUID, invoice string) {
	// Set status to LIGHTNING_PENDING
	if err := s.paymentRepo.UpdateStatus(ctx, txID, domain.StatusLightningPending, nil); err != nil {
		log.Printf("Failed to update status to LIGHTNING_PENDING for tx %s: %v", txID, err)
	}

	if s.lndService == nil {
		reason := "LND service not configured or unavailable"
		_ = s.paymentRepo.UpdateFailed(ctx, txID, domain.StatusSettlementFailed, reason)
		log.Printf("Settlement failed for tx %s: %s", txID, reason)
		return
	}

	// Max routing fee allowance: 100 Sats
	paymentHash, preimage, err := s.lndService.PayInvoice(ctx, invoice, 100)
	if err != nil {
		reason := err.Error()
		_ = s.paymentRepo.UpdateFailed(ctx, txID, domain.StatusSettlementFailed, reason)
		log.Printf("Lightning payment failed for tx %s: %v", txID, err)
		return
	}

	// Record successful settlement
	if err := s.paymentRepo.UpdateSettled(ctx, txID, paymentHash, preimage); err != nil {
		log.Printf("Failed to update settled state for tx %s: %v", txID, err)
		return
	}

	log.Printf("Lightning settlement completed successfully for tx %s (Payment Hash: %s)", txID, paymentHash)
}
