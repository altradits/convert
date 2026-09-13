package domain

import (
	"time"

	"github.com/google/uuid"
)

type TransactionStatus string

const (
	StatusPendingSTK        TransactionStatus = "PENDING_STK"
	StatusMpesaConfirmed    TransactionStatus = "MPESA_CONFIRMED"
	StatusMpesaFailed       TransactionStatus = "MPESA_FAILED"
	StatusLightningPending  TransactionStatus = "LIGHTNING_PENDING"
	StatusSettled           TransactionStatus = "SETTLED"
	StatusSettlementFailed  TransactionStatus = "SETTLEMENT_FAILED"
)

type Transaction struct {
	ID                   uuid.UUID         `json:"id"`
	UserID               *uuid.UUID        `json:"user_id,omitempty"`
	MerchantRequestID    *string           `json:"merchant_request_id,omitempty"`
	CheckoutRequestID    *string           `json:"checkout_request_id,omitempty"`
	MpesaReceiptNumber   *string           `json:"mpesa_receipt_number,omitempty"`
	AmountKES            float64           `json:"amount_kes"`
	AmountSats           int64             `json:"amount_sats"`
	LightningInvoice     string            `json:"lightning_invoice"`
	LightningPaymentHash *string           `json:"lightning_payment_hash,omitempty"`
	LightningPreimage    *string           `json:"lightning_preimage,omitempty"`
	Status               TransactionStatus `json:"status"`
	FailureReason        *string           `json:"failure_reason,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type InitiateDepositRequest struct {
	PhoneNumber string  `json:"phone_number" binding:"required"` // Format: 2547XXXXXXXX or +2547XXXXXXXX
	AmountKES   float64 `json:"amount_kes" binding:"required,gt=0"`
	Invoice     string  `json:"invoice" binding:"required"`
}

type InitiateDepositResponse struct {
	TransactionID     uuid.UUID         `json:"transaction_id"`
	CheckoutRequestID string            `json:"checkout_request_id"`
	CustomerMessage   string            `json:"customer_message"`
	Status            TransactionStatus `json:"status"`
}

type PaymentStatusResponse struct {
	TransactionID        uuid.UUID         `json:"transaction_id"`
	Status               TransactionStatus `json:"status"`
	AmountKES            float64           `json:"amount_kes"`
	AmountSats           int64             `json:"amount_sats"`
	MpesaReceiptNumber   *string           `json:"mpesa_receipt_number,omitempty"`
	LightningPaymentHash *string           `json:"lightning_payment_hash,omitempty"`
	LightningPreimage    *string           `json:"lightning_preimage,omitempty"`
	FailureReason        *string           `json:"failure_reason,omitempty"`
	CreatedAt            time.Time         `json:"created_at"`
	UpdatedAt            time.Time         `json:"updated_at"`
}

type RateQuote struct {
	ExchangeRate float64   `json:"exchange_rate_kes_per_btc"` // KES per 1 BTC
	SatsPerKES   float64   `json:"sats_per_kes"`
	Timestamp    time.Time `json:"timestamp"`
}
