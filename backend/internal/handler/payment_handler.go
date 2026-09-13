package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"yebente/internal/domain"
	"yebente/internal/repository/postgres"
	"yebente/internal/service"
)

type LightningConfigRequest struct {
	ProviderType string `json:"provider_type"` // "lnbits", "lnd_rest", "lnd_grpc", "blink"
	BaseURL      string `json:"base_url"`
	APIKey       string `json:"api_key"`       // LNbits admin key, Blink key, etc.
	MacaroonHex  string `json:"macaroon_hex"`  // LND macaroon
}

type PaymentHandler struct {
	settlementService *service.SettlementService
	lightningEngine   *service.MultiLightningEngine
	paymentRepo       postgres.PaymentRepository
}

func NewPaymentHandler(
	settlementService *service.SettlementService,
	lightningEngine *service.MultiLightningEngine,
	paymentRepo postgres.PaymentRepository,
) *PaymentHandler {
	return &PaymentHandler{
		settlementService: settlementService,
		lightningEngine:   lightningEngine,
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

	if h.paymentRepo == nil {
		c.JSON(http.StatusOK, gin.H{
			"transaction_id": txID,
			"status":         "SETTLED",
			"customer_message": "Payment verified in local testbed",
		})
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

// DecodeInvoice handles POST /api/v1/lightning/decode-invoice
func (h *PaymentHandler) DecodeInvoice(c *gin.Context) {
	var req struct {
		Invoice string `json:"invoice" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invoice is required"})
		return
	}

	decoded, err := service.DecodeBOLT11(req.Invoice)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Also calculate estimated KES needed
	quote := h.settlementService.GetRateQuote()
	var estimatedKES float64 = 0
	if decoded.AmountSats > 0 {
		estimatedKES = (float64(decoded.AmountSats) / 100000000.0) * quote.ExchangeRate
	}

	c.JSON(http.StatusOK, gin.H{
		"decoded":       decoded,
		"estimated_kes": fmt.Sprintf("%.2f", estimatedKES),
		"exchange_rate": quote.ExchangeRate,
	})
}

// ResolveLightningAddress handles POST /api/v1/lightning/resolve-address
func (h *PaymentHandler) ResolveLightningAddress(c *gin.Context) {
	var req struct {
		Address    string `json:"address" binding:"required"`
		AmountSats int64  `json:"amount_sats"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Address is required"})
		return
	}

	res, err := service.ResolveLightningAddress(c.Request.Context(), req.Address, req.AmountSats)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	quote := h.settlementService.GetRateQuote()
	var estimatedKES float64 = 0
	if req.AmountSats > 0 {
		estimatedKES = (float64(req.AmountSats) / 100000000.0) * quote.ExchangeRate
	}

	c.JSON(http.StatusOK, gin.H{
		"resolution":    res,
		"estimated_kes": fmt.Sprintf("%.2f", estimatedKES),
		"exchange_rate": quote.ExchangeRate,
	})
}

// ConfigureLightning handles POST /api/v1/lightning/config
func (h *PaymentHandler) ConfigureLightning(c *gin.Context) {
	var req LightningConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid configuration payload"})
		return
	}

	var provider service.LightningProvider
	switch req.ProviderType {
	case "lnbits":
		provider = service.NewLNbitsProvider(req.BaseURL, req.APIKey)
	case "lnd_rest":
		provider = service.NewLNDRESTProvider(req.BaseURL, req.MacaroonHex)
	case "blink":
		provider = service.NewBlinkProvider(req.APIKey)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "Unsupported provider type. Choose: lnbits, lnd_rest, blink"})
		return
	}

	// Test connection
	balance, err := provider.GetBalance(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error":   fmt.Sprintf("Failed to connect to %s: %v", provider.Name(), err),
			"details": "Check your endpoint URL and API credentials.",
		})
		return
	}

	if h.lightningEngine != nil {
		h.lightningEngine.SetProvider(provider)
	}

	c.JSON(http.StatusOK, gin.H{
		"message":      fmt.Sprintf("Successfully connected to %s", provider.Name()),
		"provider":     provider.Name(),
		"balance_sats": balance,
		"status":       "CONNECTED",
	})
}

// GetLightningConfig handles GET /api/v1/lightning/config
func (h *PaymentHandler) GetLightningConfig(c *gin.Context) {
	if h.lightningEngine == nil {
		c.JSON(http.StatusOK, gin.H{"status": "NOT_CONFIGURED", "provider": "None"})
		return
	}

	active := h.lightningEngine.GetActiveProvider()
	if active == nil || !active.IsConfigured() {
		c.JSON(http.StatusOK, gin.H{
			"status":   "DEMO_SIMULATION",
			"provider": "Built-in Live Engine",
			"message":  "Connect an LNbits, LND, or Blink wallet to dispatch real on-chain/off-chain sats.",
		})
		return
	}

	balance, err := active.GetBalance(c.Request.Context())
	balanceStr := fmt.Sprintf("%d sats", balance)
	if err != nil {
		balanceStr = "N/A"
	}

	c.JSON(http.StatusOK, gin.H{
		"status":       "CONNECTED",
		"provider":     active.Name(),
		"balance_sats": balance,
		"balance_fmt":  balanceStr,
	})
}

// InstantSettle handles POST /api/v1/payments/instant-settle
func (h *PaymentHandler) InstantSettle(c *gin.Context) {
	var req struct {
		Invoice   string  `json:"invoice" binding:"required"`
		AmountKES float64 `json:"amount_kes"`
		Phone     string  `json:"phone"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invoice is required"})
		return
	}

	txID := uuid.New()
	hash, preimage, err := h.settlementService.ExecuteLightningSettlement(c.Request.Context(), txID, req.Invoice)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error":   "Lightning payment failed",
			"details": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status":               "SETTLED",
		"transaction_id":       txID,
		"lightning_payment_hash": hash,
		"lightning_preimage":   preimage,
		"message":              "Satoshis successfully sent over Lightning Network!",
	})
}
