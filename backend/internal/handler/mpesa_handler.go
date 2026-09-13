package handler

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"yebente/internal/domain"
	"yebente/internal/repository/postgres"
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
	paymentRepo     postgres.PaymentRepository
	lightningEngine service.LightningServiceInterface
}

func NewMpesaHandler(paymentRepo postgres.PaymentRepository, lnd service.LightningServiceInterface) *MpesaHandler {
	return &MpesaHandler{paymentRepo: paymentRepo, lightningEngine: lnd}
}

func (h *MpesaHandler) HandleSTKCallback(c *gin.Context) {
	var payload DarajaCallbackPayload
	if err := c.ShouldBindJSON(&payload); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid payload"})
		return
	}

	cb := payload.Body.StkCallback
	ctx := c.Request.Context()

	if h.paymentRepo == nil {
		c.JSON(http.StatusOK, gin.H{"status": "SUCCESS"})
		return
	}

	tx, err := h.paymentRepo.GetByCheckoutRequestID(ctx, cb.CheckoutRequestID)
	if err != nil || tx == nil {
		c.JSON(http.StatusOK, gin.H{"status": "ignored_or_not_found"})
		return
	}

	if tx.Status != domain.StatusPendingSTK {
		c.JSON(http.StatusOK, gin.H{"status": "already_processed"})
		return
	}

	if cb.ResultCode != 0 {
		_ = h.paymentRepo.UpdateFailed(ctx, tx.ID, domain.StatusMpesaFailed, cb.ResultDesc)
		c.JSON(http.StatusOK, gin.H{"status": "acknowledged_failure"})
		return
	}

	var mpesaReceipt string
	for _, item := range cb.CallbackMetadata.Item {
		if item.Name == "MpesaReceiptNumber" {
			if val, ok := item.Value.(string); ok {
				mpesaReceipt = val
			}
		}
	}

	_ = h.paymentRepo.UpdateMpesaConfirmed(ctx, tx.ID, mpesaReceipt)

	// Trigger immediate asynchronous settlement
	go func(targetTx domain.Transaction) {
		bgCtx := c.Request.Context()
		_ = h.paymentRepo.UpdateStatus(bgCtx, targetTx.ID, domain.StatusLightningPending, nil)

		if h.lightningEngine == nil {
			_ = h.paymentRepo.UpdateFailed(bgCtx, targetTx.ID, domain.StatusSettlementFailed, "Lightning provider offline")
			return
		}

		hash, preimage, err := h.lightningEngine.PayInvoice(bgCtx, targetTx.LightningInvoice, 100)
		if err != nil {
			_ = h.paymentRepo.UpdateFailed(bgCtx, targetTx.ID, domain.StatusSettlementFailed, err.Error())
			log.Printf("Settlement error for tx %s: %v", targetTx.ID, err)
			return
		}

		_ = h.paymentRepo.UpdateSettled(bgCtx, targetTx.ID, hash, preimage)
		log.Printf("⚡ Payment settled for tx %s (Hash: %s)", targetTx.ID, hash)
	}(*tx)

	c.JSON(http.StatusOK, gin.H{"status": "SUCCESS"})
}
