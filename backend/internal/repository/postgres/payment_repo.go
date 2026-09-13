package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"yebente/internal/domain"
)

type PaymentRepository interface {
	CreateTransaction(ctx context.Context, tx *domain.Transaction) error
	GetByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error)
	GetByCheckoutRequestID(ctx context.Context, checkoutRequestID string) (*domain.Transaction, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status domain.TransactionStatus, failureReason *string) error
	UpdateMpesaConfirmed(ctx context.Context, id uuid.UUID, receiptNumber string) error
	UpdateSettled(ctx context.Context, id uuid.UUID, paymentHash, preimage string) error
	UpdateFailed(ctx context.Context, id uuid.UUID, status domain.TransactionStatus, reason string) error
}

type PaymentRepo struct {
	db     *sql.DB
	mu     sync.RWMutex
	memTxs map[uuid.UUID]*domain.Transaction
}

func NewPaymentRepository(db *sql.DB) *PaymentRepo {
	return &PaymentRepo{
		db:     db,
		memTxs: make(map[uuid.UUID]*domain.Transaction),
	}
}

func (r *PaymentRepo) CreateTransaction(ctx context.Context, tx *domain.Transaction) error {
	if tx.ID == uuid.Nil {
		tx.ID = uuid.New()
	}
	tx.CreatedAt = time.Now()
	tx.UpdatedAt = time.Now()

	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.memTxs[tx.ID] = tx
		return nil
	}

	query := `
		INSERT INTO transactions (
			id, user_id, merchant_request_id, checkout_request_id, mpesa_receipt_number,
			amount_kes, amount_sats, lightning_invoice, lightning_payment_hash,
			lightning_preimage, status, failure_reason, created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, NOW(), NOW()
		)
		RETURNING created_at, updated_at
	`

	return r.db.QueryRowContext(
		ctx,
		query,
		tx.ID,
		tx.UserID,
		tx.MerchantRequestID,
		tx.CheckoutRequestID,
		tx.MpesaReceiptNumber,
		tx.AmountKES,
		tx.AmountSats,
		tx.LightningInvoice,
		tx.LightningPaymentHash,
		tx.LightningPreimage,
		tx.Status,
		tx.FailureReason,
	).Scan(&tx.CreatedAt, &tx.UpdatedAt)
}

func (r *PaymentRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.Transaction, error) {
	if r.db == nil {
		r.mu.RLock()
		defer r.mu.RUnlock()
		return r.memTxs[id], nil
	}

	query := `
		SELECT id, user_id, merchant_request_id, checkout_request_id, mpesa_receipt_number,
		       amount_kes, amount_sats, lightning_invoice, lightning_payment_hash,
		       lightning_preimage, status, failure_reason, created_at, updated_at
		FROM transactions
		WHERE id = $1
	`

	var tx domain.Transaction
	var userID sql.NullString
	var merchantReqID, checkoutReqID, mpesaReceipt, paymentHash, preimage, failureReason sql.NullString

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&tx.ID,
		&userID,
		&merchantReqID,
		&checkoutReqID,
		&mpesaReceipt,
		&tx.AmountKES,
		&tx.AmountSats,
		&tx.LightningInvoice,
		&paymentHash,
		&preimage,
		&tx.Status,
		&failureReason,
		&tx.CreatedAt,
		&tx.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get transaction by id: %w", err)
	}

	if userID.Valid {
		uid, err := uuid.Parse(userID.String)
		if err == nil {
			tx.UserID = &uid
		}
	}
	if merchantReqID.Valid {
		tx.MerchantRequestID = &merchantReqID.String
	}
	if checkoutReqID.Valid {
		tx.CheckoutRequestID = &checkoutReqID.String
	}
	if mpesaReceipt.Valid {
		tx.MpesaReceiptNumber = &mpesaReceipt.String
	}
	if paymentHash.Valid {
		tx.LightningPaymentHash = &paymentHash.String
	}
	if preimage.Valid {
		tx.LightningPreimage = &preimage.String
	}
	if failureReason.Valid {
		tx.FailureReason = &failureReason.String
	}

	return &tx, nil
}

func (r *PaymentRepo) GetByCheckoutRequestID(ctx context.Context, checkoutRequestID string) (*domain.Transaction, error) {
	if r.db == nil {
		r.mu.RLock()
		defer r.mu.RUnlock()
		for _, tx := range r.memTxs {
			if tx.CheckoutRequestID != nil && *tx.CheckoutRequestID == checkoutRequestID {
				return tx, nil
			}
		}
		return nil, nil
	}

	query := `
		SELECT id, user_id, merchant_request_id, checkout_request_id, mpesa_receipt_number,
		       amount_kes, amount_sats, lightning_invoice, lightning_payment_hash,
		       lightning_preimage, status, failure_reason, created_at, updated_at
		FROM transactions
		WHERE checkout_request_id = $1
	`

	var tx domain.Transaction
	var userID sql.NullString
	var merchantReqID, checkoutReqID, mpesaReceipt, paymentHash, preimage, failureReason sql.NullString

	err := r.db.QueryRowContext(ctx, query, checkoutRequestID).Scan(
		&tx.ID,
		&userID,
		&merchantReqID,
		&checkoutReqID,
		&mpesaReceipt,
		&tx.AmountKES,
		&tx.AmountSats,
		&tx.LightningInvoice,
		&paymentHash,
		&preimage,
		&tx.Status,
		&failureReason,
		&tx.CreatedAt,
		&tx.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get transaction by checkout_request_id: %w", err)
	}

	if userID.Valid {
		uid, err := uuid.Parse(userID.String)
		if err == nil {
			tx.UserID = &uid
		}
	}
	if merchantReqID.Valid {
		tx.MerchantRequestID = &merchantReqID.String
	}
	if checkoutReqID.Valid {
		tx.CheckoutRequestID = &checkoutReqID.String
	}
	if mpesaReceipt.Valid {
		tx.MpesaReceiptNumber = &mpesaReceipt.String
	}
	if paymentHash.Valid {
		tx.LightningPaymentHash = &paymentHash.String
	}
	if preimage.Valid {
		tx.LightningPreimage = &preimage.String
	}
	if failureReason.Valid {
		tx.FailureReason = &failureReason.String
	}

	return &tx, nil
}

func (r *PaymentRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status domain.TransactionStatus, failureReason *string) error {
	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if tx, ok := r.memTxs[id]; ok {
			tx.Status = status
			tx.FailureReason = failureReason
			tx.UpdatedAt = time.Now()
		}
		return nil
	}

	query := `
		UPDATE transactions
		SET status = $1,
		    failure_reason = $2,
		    updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, status, failureReason, id)
	return err
}

func (r *PaymentRepo) UpdateMpesaConfirmed(ctx context.Context, id uuid.UUID, receiptNumber string) error {
	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if tx, ok := r.memTxs[id]; ok {
			tx.Status = domain.StatusMpesaConfirmed
			tx.MpesaReceiptNumber = &receiptNumber
			tx.UpdatedAt = time.Now()
		}
		return nil
	}

	query := `
		UPDATE transactions
		SET status = 'MPESA_CONFIRMED',
		    mpesa_receipt_number = $1,
		    updated_at = NOW()
		WHERE id = $2
	`
	_, err := r.db.ExecContext(ctx, query, receiptNumber, id)
	return err
}

func (r *PaymentRepo) UpdateSettled(ctx context.Context, id uuid.UUID, paymentHash, preimage string) error {
	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if tx, ok := r.memTxs[id]; ok {
			tx.Status = domain.StatusSettled
			tx.LightningPaymentHash = &paymentHash
			tx.LightningPreimage = &preimage
			tx.UpdatedAt = time.Now()
		}
		return nil
	}

	query := `
		UPDATE transactions
		SET status = 'SETTLED',
		    lightning_payment_hash = $1,
		    lightning_preimage = $2,
		    updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, paymentHash, preimage, id)
	return err
}

func (r *PaymentRepo) UpdateFailed(ctx context.Context, id uuid.UUID, status domain.TransactionStatus, reason string) error {
	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if tx, ok := r.memTxs[id]; ok {
			tx.Status = status
			tx.FailureReason = &reason
			tx.UpdatedAt = time.Now()
		}
		return nil
	}

	query := `
		UPDATE transactions
		SET status = $1,
		    failure_reason = $2,
		    updated_at = NOW()
		WHERE id = $3
	`
	_, err := r.db.ExecContext(ctx, query, status, reason, id)
	return err
}
