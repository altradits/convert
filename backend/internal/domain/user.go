package domain

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	ID            uuid.UUID  `json:"id"`
	PhoneNumber   string     `json:"phone_number"`
	IsVerified    bool       `json:"is_verified"`
	OTPCodeHash   *string    `json:"-"`
	OTPExpiresAt  *time.Time `json:"-"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

type OTPRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
}

type OTPVerifyRequest struct {
	PhoneNumber string `json:"phone_number" binding:"required"`
	Code        string `json:"code" binding:"required,len=6"`
}

type AuthResponse struct {
	Token   string `json:"token,omitempty"`
	User    *User  `json:"user"`
	Message string `json:"message"`
}
