package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTransactionStatusValues(t *testing.T) {
	statuses := []TransactionStatus{
		StatusPendingSTK,
		StatusMpesaConfirmed,
		StatusMpesaFailed,
		StatusLightningPending,
		StatusSettled,
		StatusSettlementFailed,
	}

	for _, s := range statuses {
		if s == "" {
			t.Errorf("empty transaction status found")
		}
	}
}

func TestTransactionInstantiation(t *testing.T) {
	id := uuid.New()
	checkoutID := "ws_CO_TEST123"
	receipt := "SDF893JK2L"

	tx := &Transaction{
		ID:                 id,
		CheckoutRequestID:  &checkoutID,
		MpesaReceiptNumber: &receipt,
		AmountKES:          500.0,
		AmountSats:         5882,
		LightningInvoice:   "lnbc10u1...",
		Status:             StatusSettled,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if tx.ID != id {
		t.Fatalf("expected ID %s, got %s", id, tx.ID)
	}
	if tx.AmountKES != 500.0 {
		t.Fatalf("expected AmountKES 500.0, got %f", tx.AmountKES)
	}
	if *tx.MpesaReceiptNumber != receipt {
		t.Fatalf("expected receipt %s, got %s", receipt, *tx.MpesaReceiptNumber)
	}
}
