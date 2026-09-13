package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"yebente/internal/domain"
	"yebente/internal/repository/postgres"
)

type AuthHandler struct {
	userRepo postgres.UserRepository
}

func NewAuthHandler(userRepo postgres.UserRepository) *AuthHandler {
	return &AuthHandler{userRepo: userRepo}
}

func hashOTP(code string) string {
	hash := sha256.Sum256([]byte(code))
	return hex.EncodeToString(hash[:])
}

// RequestOTP handles POST /api/v1/auth/request-otp
func (h *AuthHandler) RequestOTP(c *gin.Context) {
	var req domain.OTPRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	// Generate 6-digit OTP
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	otp := fmt.Sprintf("%06d", rng.Intn(1000000))
	otpHash := hashOTP(otp)
	expiresAt := time.Now().Add(5 * time.Minute)

	if err := h.userRepo.SetOTP(c.Request.Context(), req.PhoneNumber, otpHash, expiresAt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to save verification code"})
		return
	}

	// In production, integrate SMS provider (e.g. Africa's Talking / Twilio / Safaricom SMS Gateway)
	// For dev/test: return OTP in response or log it
	c.JSON(http.StatusOK, gin.H{
		"message":      fmt.Sprintf("Verification code sent to %s", req.PhoneNumber),
		"phone_number": req.PhoneNumber,
		"dev_otp":      otp, // Helpful for local sandbox testing
		"expires_in":   300,
	})
}

// VerifyOTP handles POST /api/v1/auth/verify-otp
func (h *AuthHandler) VerifyOTP(c *gin.Context) {
	var req domain.OTPVerifyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload", "details": err.Error()})
		return
	}

	user, err := h.userRepo.GetByPhone(c.Request.Context(), req.PhoneNumber)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check verification"})
		return
	}

	if user == nil || user.OTPCodeHash == nil || user.OTPExpiresAt == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "No pending verification found for this number"})
		return
	}

	if time.Now().After(*user.OTPExpiresAt) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Verification code has expired"})
		return
	}

	incomingHash := hashOTP(req.Code)
	if *user.OTPCodeHash != incomingHash {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid verification code"})
		return
	}

	if err := h.userRepo.VerifyUser(c.Request.Context(), req.PhoneNumber); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update user verification status"})
		return
	}

	user.IsVerified = true
	c.JSON(http.StatusOK, domain.AuthResponse{
		Token:   "dummy_jwt_token_" + user.ID.String(),
		User:    user,
		Message: "Phone number verified successfully",
	})
}
