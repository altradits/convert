package service

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"yebente/internal/domain"
)

type mockDarajaService struct {
	triggerSTKFunc func(ctx context.Context, phone string, amountKES int64, accountReference string) (*STKPushResponse, error)
}

func (m *mockDarajaService) GetAccessToken(ctx context.Context) (string, error) {
	return "mock_access_token", nil
}

func (m *mockDarajaService) TriggerSTKPush(ctx context.Context, phone string, amountKES int64, accountReference string) (*STKPushResponse, error) {
	if m.triggerSTKFunc != nil {
		return m.triggerSTKFunc(ctx, phone, amountKES, accountReference)
	}
	return &STKPushResponse{
		MerchantRequestID:   "MR-12345",
		CheckoutRequestID:   "ws_CO_123456789",
		ResponseCode:        "0",
		ResponseDescription: "Success",
		CustomerMessage:     "Success. Request accepted for processing",
	}, nil
}

type mockLightningService struct {
	payInvoiceFunc func(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error)
}

func (m *mockLightningService) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error) {
	if m.payInvoiceFunc != nil {
		return m.payInvoiceFunc(ctx, invoice, maxFeeSats)
	}
	return "mock_payment_hash_1234", "mock_preimage_abcd", nil
}

func (m *mockLightningService) Close() error {
	return nil
}

type mockPaymentRepo struct {
	transactions map[uuid.UUID]*domain.Transaction
}

func newMockPaymentRepo() *mockPaymentRepo {
	return &mockPaymentRepo{transactions: make(map[uuid.UUID]*domain.Transaction)}
}

func (r *mockPaymentRepo) CreateTransaction(ctx context.Context, tx *domain.Transaction) error {
	r.transactions[tx.ID] = tx
	return nil
}

func (r *mockPaymentRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	return r.transactions[id], nil
}

func (r *mockPaymentRepo) GetByCheckoutRequestID(ctx context.Context, checkoutRequestID string) (*domain.Transaction, error) {
	for _, tx := range r.transactions {
		if tx.CheckoutRequestID != nil && *tx.CheckoutRequestID == checkoutRequestID {
			return tx, nil
		}
	}
	return nil, nil
}

func (r *mockPaymentRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.TransactionStatus, failureReason *string) error {
	if tx, ok := r.transactions[id]; ok {
		tx.Status = status
		tx.FailureReason = failureReason
	}
	return nil
}

func (r *mockPaymentRepo) UpdateMpesaConfirmed(ctx context.Context, id uuid.UUID, receiptNumber string) error {
	if tx, ok := r.transactions[id]; ok {
		tx.Status = domain.StatusMpesaConfirmed
		tx.MpesaReceiptNumber = &receiptNumber
	}
	return nil
}

func (r *mockPaymentRepo) UpdateSettled(ctx context.Context, id uuid.UUID, paymentHash, preimage string) error {
	if tx, ok := r.transactions[id]; ok {
		tx.Status = domain.StatusSettled
		tx.LightningPaymentHash = &paymentHash
		tx.LightningPreimage = &preimage
	}
	return nil
}

func (r *mockPaymentRepo) UpdateFailed(ctx context.Context, id uuid.UUID, status domain.TransactionStatus, reason string) error {
	if tx, ok := r.transactions[id]; ok {
		tx.Status = status
		tx.FailureReason = &reason
	}
	return nil
}

type mockUserRepo struct{}

func (m *mockUserRepo) CreateOrGetUser(ctx context.Context, phone string) (*domain.User, error) {
	return &domain.User{ID: uuid.New(), PhoneNumber: phone, IsVerified: true}, nil
}

func (m *mockUserRepo) GetByPhone(ctx context.Context, phone string) (*domain.User, error) {
	return &domain.User{ID: uuid.New(), PhoneNumber: phone, IsVerified: true}, nil
}

func (m *mockUserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	return &domain.User{ID: id, PhoneNumber: "+254707172370", IsVerified: true}, nil
}

func (m *mockUserRepo) SetOTP(ctx context.Context, phone string, otpHash string, expiresAt time.Time) error {
	return nil
}

func (m *mockUserRepo) VerifyUser(ctx context.Context, phone string) error {
	return nil
}

func TestConvertKESToSats(t *testing.T) {
	rate := 8500000.0 // 8.5M KES / BTC
	svc := NewSettlementService(&mockDarajaService{}, &mockLightningService{}, newMockPaymentRepo(), nil, rate)

	// 850 KES = 0.0001 BTC = 10,000 sats
	sats := svc.ConvertKESToSats(850.0)
	if sats != 10000 {
		t.Fatalf("expected 10000 sats for 850 KES at 8.5M rate, got %d", sats)
	}

	// 85 KES = 0.00001 BTC = 1,000 sats
	sats = svc.ConvertKESToSats(85.0)
	if sats != 1000 {
		t.Fatalf("expected 1000 sats for 85 KES, got %d", sats)
	}
}

func TestInitiateDepositAndSettlementFlow(t *testing.T) {
	ctx := context.Background()
	darajaMock := &mockDarajaService{}
	lndMock := &mockLightningService{}
	paymentRepo := newMockPaymentRepo()
	userRepo := &mockUserRepo{}

	svc := NewSettlementService(darajaMock, lndMock, paymentRepo, userRepo, 8500000.0)

	// 1. Initiate Deposit (100u = 100 micro-BTC = 10,000 sats)
	invoice := "lnbc100u1pj...mock_invoice"
	resp, err := svc.InitiateDeposit(ctx, "+254707172370", 850.0, invoice)
	if err != nil {
		t.Fatalf("failed to initiate deposit: %v", err)
	}

	if resp.Status != domain.StatusPendingSTK {
		t.Fatalf("expected status %s, got %s", domain.StatusPendingSTK, resp.Status)
	}

	tx := paymentRepo.transactions[resp.TransactionID]
	if tx == nil {
		t.Fatalf("expected transaction to be saved in repository")
	}
	if tx.AmountSats != 10000 {
		t.Fatalf("expected 10000 sats, got %d", tx.AmountSats)
	}

	// 2. Simulate settlement
	svc.ExecuteLightningSettlement(ctx, tx.ID, invoice)

	if tx.Status != domain.StatusSettled {
		t.Fatalf("expected status %s, got %s", domain.StatusSettled, tx.Status)
	}
	if tx.LightningPreimage == nil || *tx.LightningPreimage != "mock_preimage_abcd" {
		t.Fatalf("expected preimage to be populated")
	}
}

func TestMultiLightningEngine(t *testing.T) {
	ctx := context.Background()

	// 1. Unconfigured engine must strictly return an error
	emptyEngine := NewMultiLightningEngine(nil)
	_, _, err := emptyEngine.PayInvoice(ctx, "lnbc100u1pj_test", 100)
	if err == nil {
		t.Fatalf("expected unconfigured engine to return an error, but got nil")
	}

	// 2. Test LND fallback provider
	engine := NewMultiLightningEngine(&mockLightningService{})
	hash, preimage, err := engine.PayInvoice(ctx, "lnbc100u1pj_test", 100)
	if err != nil {
		t.Fatalf("expected payment to succeed: %v", err)
	}
	if hash != "mock_payment_hash_1234" || preimage != "mock_preimage_abcd" {
		t.Fatalf("unexpected hash/preimage: %s / %s", hash, preimage)
	}

	// 3. Test LNbits provider setup
	lnbits := NewLNbitsProvider("https://legend.lnbits.com", "fake_admin_key")
	if !lnbits.IsConfigured() {
		t.Fatalf("expected lnbits to be configured")
	}
	engine.SetProvider(lnbits)
	if engine.GetActiveProvider().Name() != "LNbits API" {
		t.Fatalf("expected LNbits API active provider")
	}

	// 4. Test Blink provider setup
	blink := NewBlinkProvider("fake_blink_api_key")
	if !blink.IsConfigured() {
		t.Fatalf("expected blink to be configured")
	}
	engine.SetProvider(blink)
	if engine.GetActiveProvider().Name() != "Blink (Galoy) Lightning" {
		t.Fatalf("expected Blink active provider")
	}
}
