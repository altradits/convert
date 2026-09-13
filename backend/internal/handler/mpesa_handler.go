package handler

import (
	"context"
	"log"
	"net/http"
	"time"

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
	paymentRepo       postgres.PaymentRepository
	settlementService *service.SettlementService
}

func NewMpesaHandler(paymentRepo postgres.PaymentRepository, settlementService *service.SettlementService) *MpesaHandler {
	return &MpesaHandler{paymentRepo: paymentRepo, settlementService: settlementService}
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

	// Trigger immediate asynchronous settlement using independent background context
	go func(targetTx domain.Transaction) {
		bgCtx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()

		if h.settlementService == nil {
			if h.paymentRepo != nil {
				_ = h.paymentRepo.UpdateFailed(bgCtx, targetTx.ID, domain.StatusSettlementFailed, "Settlement service unavailable")
			}
			return
		}

		hash, preimage, err := h.settlementService.ExecuteLightningSettlement(bgCtx, targetTx.ID, targetTx.LightningInvoice)
		if err != nil {
			log.Printf("Settlement error for tx %s: %v", targetTx.ID, err)
			return
		}

		log.Printf("⚡ Payment successfully settled for tx %s (Hash: %s, Preimage: %s)", targetTx.ID, hash, preimage)
	}(*tx)

	c.JSON(http.StatusOK, gin.H{"status": "SUCCESS"})
}
