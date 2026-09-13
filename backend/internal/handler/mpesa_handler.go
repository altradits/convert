package handler

import (
	"context"
	"database/sql"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"yebente/internal/service"
)

type DarajaCallbackPayload struct {
	Body struct {
		StkCallback struct {
			MerchantRequestID string `json:"MerchantRequestID"`
			CheckoutRequestID string `json:"CheckoutRequestID"`
			ResultCode        int    `json:"ResultCode"`
			ResultDesc        string `json:"ResultDesc"`
			CallbackMetadata  struct {
				Item []struct {
					Name  string      `json:"Name"`
					Value interface{} `json:"Value"`
				} `json:"Item"`
			} `json:"CallbackMetadata"`
		} `json:"stkCallback"`
	} `json:"Body"`
}

type MpesaHandler struct {
	db         *sql.DB
	lndService service.LightningServiceInterface
}

func NewMpesaHandler(db *sql.DB, lnd service.LightningServiceInterface) *MpesaHandler {
	return &MpesaHandler{db: db, lndService: lnd}
}

func (h *MpesaHandler) HandleSTKCallback(c *gin.Context) {
	var payload DarajaCallbackPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	cb := payload.Body.StkCallback
	ctx := context.Background()

	// 1. Start DB Transaction with row lock to prevent race conditions
	tx, err := h.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Database error"})
		return
	}
	defer tx.Rollback()

	var txID, status, invoice string
	var amountSats int64

	row := tx.QueryRowContext(ctx, `
		SELECT id, status, lightning_invoice, amount_sats 
		FROM transactions 
		WHERE checkout_request_id = $1 
		FOR UPDATE
	`, cb.CheckoutRequestID)

	if err := row.Scan(&txID, &status, &invoice, &amountSats); err != nil {
		// Idempotency: Ignore duplicate or unknown callbacks
		c.JSON(http.StatusOK, gin.H{"status": "ignored_or_not_found"})
		return
	}

	// 2. Reject if already processed
	if status != "PENDING_STK" {
		c.JSON(http.StatusOK, gin.H{"status": "already_processed"})
		return
	}

	if cb.ResultCode != 0 {
		_, _ = tx.ExecContext(ctx, `
			UPDATE transactions 
			SET status = 'MPESA_FAILED', failure_reason = $1, updated_at = NOW() 
			WHERE id = $2
		`, cb.ResultDesc, txID)
		_ = tx.Commit()
		c.JSON(http.StatusOK, gin.H{"status": "acknowledged_failure"})
		return
	}

	// Extract MpesaReceiptNumber
	var mpesaReceipt string
	for _, item := range cb.CallbackMetadata.Item {
		if item.Name == "MpesaReceiptNumber" {
			if val, ok := item.Value.(string); ok {
				mpesaReceipt = val
			}
		}
	}

	// Update to MPESA_CONFIRMED
	_, err = tx.ExecContext(ctx, `
		UPDATE transactions 
		SET status = 'MPESA_CONFIRMED', mpesa_receipt_number = $1, updated_at = NOW() 
		WHERE id = $2
	`, mpesaReceipt, txID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update state"})
		return
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to commit tx"})
		return
	}

	// 3. Trigger immediate asynchronous LND settlement
	go h.settleLightning(txID, invoice)

	c.JSON(http.StatusOK, gin.H{"status": "SUCCESS"})
}

func (h *MpesaHandler) settleLightning(txID string, invoice string) {
	ctx := context.Background()

	_, _ = h.db.ExecContext(ctx, `
		UPDATE transactions SET status = 'LIGHTNING_PENDING', updated_at = NOW() WHERE id = $1
	`, txID)

	if h.lndService == nil {
		reason := "LND service not available"
		_, _ = h.db.ExecContext(ctx, `
			UPDATE transactions 
			SET status = 'SETTLEMENT_FAILED', failure_reason = $1, updated_at = NOW() 
			WHERE id = $2
		`, reason, txID)
		log.Printf("Settlement failed for tx %s: %s", txID, reason)
		return
	}

	// Max routing fee allowance: 100 Sats
	paymentHash, preimage, err := h.lndService.PayInvoice(ctx, invoice, 100)
	if err != nil {
		_, _ = h.db.ExecContext(ctx, `
			UPDATE transactions 
			SET status = 'SETTLEMENT_FAILED', failure_reason = $1, updated_at = NOW() 
			WHERE id = $2
		`, err.Error(), txID)
		log.Printf("LND payment failed for tx %s: %v", txID, err)
		return
	}

	_, _ = h.db.ExecContext(ctx, `
		UPDATE transactions 
		SET status = 'SETTLED', lightning_payment_hash = $1, lightning_preimage = $2, updated_at = NOW() 
		WHERE id = $3
	`, paymentHash, preimage, txID)

	log.Printf("LND payment settled for tx %s (preimage: %s)", txID, preimage)
}
