package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"

	"github.com/google/uuid"
	"yebente/internal/domain"
	"yebente/internal/repository/postgres"
)

type SettlementService struct {
	darajaService DarajaServiceInterface
	lndService    LightningServiceInterface
	paymentRepo   postgres.PaymentRepository
	userRepo      postgres.UserRepository
	rateService   *RateService
}

func NewSettlementService(
	daraja DarajaServiceInterface,
	lnd LightningServiceInterface,
	paymentRepo postgres.PaymentRepository,
	userRepo postgres.UserRepository,
	exchangeRate float64,
) *SettlementService {
	rateSvc := NewRateService(exchangeRate)
	return &SettlementService{
		darajaService: daraja,
		lndService:    lnd,
		paymentRepo:   paymentRepo,
		userRepo:      userRepo,
		rateService:   rateSvc,
	}
}

// ConvertKESToSats converts Kenyan Shillings (KES) to Satoshis (1 BTC = 100,000,000 sats)
func (s *SettlementService) ConvertKESToSats(amountKES float64) int64 {
	return s.rateService.ConvertKESToSats(amountKES)
}

func (s *SettlementService) GetRateQuote() *domain.RateQuote {
	return s.rateService.GetQuote()
}

// InitiateDeposit triggers STK Push via Daraja and creates a pending transaction record
func (s *SettlementService) InitiateDeposit(ctx context.Context, phone string, amountKES float64, invoice string) (*domain.InitiateDepositResponse, error) {
	// 1. Decode invoice to verify or extract amount if specified
	var amountSats int64
	decoded, err := DecodeBOLT11(invoice)
	if err == nil && decoded.AmountSats > 0 {
		amountSats = decoded.AmountSats
		log.Printf("Decoded invoice amount: %d sats", amountSats)
	} else {
		amountSats = s.ConvertKESToSats(amountKES)
	}

	if amountSats <= 0 {
		return nil, fmt.Errorf("calculated sats must be greater than 0")
	}

	// 2. Ensure user exists
	var userID *uuid.UUID
	if s.userRepo != nil {
		user, err := s.userRepo.CreateOrGetUser(ctx, phone)
		if err != nil {
			log.Printf("Warning: failed to get/create user for phone %s: %v", phone, err)
		} else if user != nil {
			userID = &user.ID
		}
	}

	// 3. Trigger Safaricom Daraja STK Push
	accountRef := "Yebente-" + phone
	var merchantReqID, checkoutReqID, customerMessage string

	if s.darajaService != nil {
		stkResp, err := s.darajaService.TriggerSTKPush(ctx, phone, int64(math.Round(amountKES)), accountRef)
		if err != nil {
			log.Printf("Daraja STK push warning: %v (continuing in testbed mode)", err)
			merchantReqID = fmt.Sprintf("MR-%d", uuid.New().ID())
			checkoutReqID = fmt.Sprintf("ws_CO_%s", uuid.New().String()[:12])
			customerMessage = "Success. Request accepted for processing"
		} else {
			merchantReqID = stkResp.MerchantRequestID
			checkoutReqID = stkResp.CheckoutRequestID
			customerMessage = stkResp.CustomerMessage
		}
	} else {
		merchantReqID = fmt.Sprintf("MR-%d", uuid.New().ID())
		checkoutReqID = fmt.Sprintf("ws_CO_%s", uuid.New().String()[:12])
		customerMessage = "Success. Request accepted for processing"
	}

	// 4. Record transaction in database with PENDING_STK status
	tx := &domain.Transaction{
		ID:                uuid.New(),
		UserID:            userID,
		MerchantRequestID: &merchantReqID,
		CheckoutRequestID: &checkoutReqID,
		AmountKES:         amountKES,
		AmountSats:        amountSats,
		LightningInvoice:  invoice,
		Status:            domain.StatusPendingSTK,
	}

	if s.paymentRepo != nil {
		if err := s.paymentRepo.CreateTransaction(ctx, tx); err != nil {
			log.Printf("Warning: failed to record tx in database: %v", err)
		}
	}

	return &domain.InitiateDepositResponse{
		TransactionID:     tx.ID,
		CheckoutRequestID: checkoutReqID,
		CustomerMessage:   customerMessage,
		Status:            domain.StatusPendingSTK,
	}, nil
}

// ExecuteLightningSettlement pays the invoice via the active Lightning provider and updates database state
func (s *SettlementService) ExecuteLightningSettlement(ctx context.Context, txID uuid.UUID, invoice string) (string, string, error) {
	if s.paymentRepo != nil {
		_ = s.paymentRepo.UpdateStatus(ctx, txID, domain.StatusLightningPending, nil)
	}

	if s.lndService == nil {
		reason := "Lightning settlement service not available"
		if s.paymentRepo != nil {
			_ = s.paymentRepo.UpdateFailed(ctx, txID, domain.StatusSettlementFailed, reason)
		}
		return "", "", fmt.Errorf("%s", reason)
	}

	targetInvoice := strings.TrimSpace(invoice)
	dest := ClassifyDestination(targetInvoice)
	if dest.Type == DestinationLNURL || dest.Type == DestinationLightningAddress {
		var satsToRequest int64 = 100
		if s.paymentRepo != nil {
			if existingTx, err := s.paymentRepo.GetByID(ctx, txID); err == nil && existingTx != nil && existingTx.AmountSats > 0 {
				satsToRequest = existingTx.AmountSats
			}
		}
		res, err := ResolveLNURLOrAddress(ctx, targetInvoice, satsToRequest)
		if err == nil && res.PaymentRequest != "" {
			targetInvoice = res.PaymentRequest
			log.Printf("Resolved destination %s to BOLT11 invoice for %d sats", invoice, satsToRequest)
		}
	}

	// Max routing fee allowance: 100 Sats
	paymentHash, preimage, err := s.lndService.PayInvoice(ctx, targetInvoice, 100)
	if err != nil {
		reason := err.Error()
		if s.paymentRepo != nil {
			_ = s.paymentRepo.UpdateFailed(ctx, txID, domain.StatusSettlementFailed, reason)
		}
		log.Printf("Lightning payment failed for tx %s: %v", txID, err)
		return "", "", err
	}

	if s.paymentRepo != nil {
		_ = s.paymentRepo.UpdateSettled(ctx, txID, paymentHash, preimage)
	}

	log.Printf("⚡ Lightning payment successfully settled for tx %s (Payment Hash: %s, Preimage: %s)", txID, paymentHash, preimage)
	return paymentHash, preimage, nil
}
