package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"yebente/internal/domain"
	"yebente/internal/repository/postgres"
	"yebente/internal/service"
)

type PaymentHandler struct {
	settlementService *service.SettlementService
	paymentRepo       postgres.PaymentRepository
}

func NewPaymentHandler(settlementService *service.SettlementService, paymentRepo postgres.PaymentRepository) *PaymentHandler {
	return &PaymentHandler{
		settlementService: settlementService,
		paymentRepo:       paymentRepo,
	}
}

// InitiateDeposit handles POST /api/v1/payments/deposit
func (h *PaymentHandler) InitiateDeposit(c *gin.Context) {
	var req domain.InitiateDepositRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	resp, err := h.settlementService.InitiateDeposit(c.Request.Context(), req.PhoneNumber, req.AmountKES, req.Invoice)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to initiate deposit", "details": err.Error()})
		return
	}

	c.JSON(http.StatusOK, resp)
}

// GetStatus handles GET /api/v1/payments/:id/status
func (h *PaymentHandler) GetStatus(c *gin.Context) {
	idParam := c.Param("id")
	txID, err := uuid.Parse(idParam)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid transaction ID format"})
		return
	}

	tx, err := h.paymentRepo.GetByID(c.Request.Context(), txID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve transaction status"})
		return
	}

	if tx == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Transaction not found"})
		return
	}

	c.JSON(http.StatusOK, domain.PaymentStatusResponse{
		TransactionID:        tx.ID,
		Status:               tx.Status,
		AmountKES:            tx.AmountKES,
		AmountSats:           tx.AmountSats,
		MpesaReceiptNumber:   tx.MpesaReceiptNumber,
		LightningPaymentHash: tx.LightningPaymentHash,
		LightningPreimage:    tx.LightningPreimage,
		FailureReason:        tx.FailureReason,
		CreatedAt:            tx.CreatedAt,
		UpdatedAt:            tx.UpdatedAt,
	})
}

// GetRates handles GET /api/v1/rates
func (h *PaymentHandler) GetRates(c *gin.Context) {
	quote := h.settlementService.GetRateQuote()
	c.JSON(http.StatusOK, quote)
}
