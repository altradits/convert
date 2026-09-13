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

type UserRepository interface {
	CreateOrGetUser(ctx context.Context, phone string) (*domain.User, error)
	GetByPhone(ctx context.Context, phone string) (*domain.User, error)
	GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error)
	SetOTP(ctx context.Context, phone string, otpHash string, expiresAt time.Time) error
	VerifyUser(ctx context.Context, phone string) error
}

type UserRepo struct {
	db      *sql.DB
	mu      sync.RWMutex
	memUser map[string]*domain.User // in-memory fallback
}

func NewUserRepository(db *sql.DB) *UserRepo {
	return &UserRepo{
		db:      db,
		memUser: make(map[string]*domain.User),
	}
}

func (r *UserRepo) CreateOrGetUser(ctx context.Context, phone string) (*domain.User, error) {
	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if u, exists := r.memUser[phone]; exists {
			return u, nil
		}
		newUser := &domain.User{
			ID:          uuid.New(),
			PhoneNumber: phone,
			IsVerified:  true,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		r.memUser[phone] = newUser
		return newUser, nil
	}

	query := `
		INSERT INTO users (phone_number, created_at, updated_at)
		VALUES ($1, NOW(), NOW())
		ON CONFLICT (phone_number) DO UPDATE
		SET updated_at = NOW()
		RETURNING id, phone_number, is_verified, otp_code_hash, otp_expires_at, created_at, updated_at
	`

	var user domain.User
	var otpHash sql.NullString
	var otpExpires sql.NullTime

	err := r.db.QueryRowContext(ctx, query, phone).Scan(
		&user.ID,
		&user.PhoneNumber,
		&user.IsVerified,
		&otpHash,
		&otpExpires,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create or get user: %w", err)
	}

	if otpHash.Valid {
		user.OTPCodeHash = &otpHash.String
	}
	if otpExpires.Valid {
		user.OTPExpiresAt = &otpExpires.Time
	}

	return &user, nil
}

func (r *UserRepo) GetByPhone(ctx context.Context, phone string) (*domain.User, error) {
	if r.db == nil {
		r.mu.RLock()
		defer r.mu.RUnlock()
		return r.memUser[phone], nil
	}

	query := `
		SELECT id, phone_number, is_verified, otp_code_hash, otp_expires_at, created_at, updated_at
		FROM users
		WHERE phone_number = $1
	`

	var user domain.User
	var otpHash sql.NullString
	var otpExpires sql.NullTime

	err := r.db.QueryRowContext(ctx, query, phone).Scan(
		&user.ID,
		&user.PhoneNumber,
		&user.IsVerified,
		&otpHash,
		&otpExpires,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by phone: %w", err)
	}

	if otpHash.Valid {
		user.OTPCodeHash = &otpHash.String
	}
	if otpExpires.Valid {
		user.OTPExpiresAt = &otpExpires.Time
	}

	return &user, nil
}

func (r *UserRepo) GetByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	if r.db == nil {
		r.mu.RLock()
		defer r.mu.RUnlock()
		for _, u := range r.memUser {
			if u.ID == id {
				return u, nil
			}
		}
		return nil, nil
	}

	query := `
		SELECT id, phone_number, is_verified, otp_code_hash, otp_expires_at, created_at, updated_at
		FROM users
		WHERE id = $1
	`

	var user domain.User
	var otpHash sql.NullString
	var otpExpires sql.NullTime

	err := r.db.QueryRowContext(ctx, query, id).Scan(
		&user.ID,
		&user.PhoneNumber,
		&user.IsVerified,
		&otpHash,
		&otpExpires,
		&user.CreatedAt,
		&user.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user by id: %w", err)
	}

	if otpHash.Valid {
		user.OTPCodeHash = &otpHash.String
	}
	if otpExpires.Valid {
		user.OTPExpiresAt = &otpExpires.Time
	}

	return &user, nil
}

func (r *UserRepo) SetOTP(ctx context.Context, phone string, otpHash string, expiresAt time.Time) error {
	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		u, ok := r.memUser[phone]
		if !ok {
			u = &domain.User{ID: uuid.New(), PhoneNumber: phone}
			r.memUser[phone] = u
		}
		u.OTPCodeHash = &otpHash
		u.OTPExpiresAt = &expiresAt
		return nil
	}

	query := `
		INSERT INTO users (phone_number, otp_code_hash, otp_expires_at, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (phone_number) DO UPDATE
		SET otp_code_hash = EXCLUDED.otp_code_hash,
		    otp_expires_at = EXCLUDED.otp_expires_at,
		    updated_at = NOW()
	`
	_, err := r.db.ExecContext(ctx, query, phone, otpHash, expiresAt)
	if err != nil {
		return fmt.Errorf("failed to set user OTP: %w", err)
	}
	return nil
}

func (r *UserRepo) VerifyUser(ctx context.Context, phone string) error {
	if r.db == nil {
		r.mu.Lock()
		defer r.mu.Unlock()
		if u, ok := r.memUser[phone]; ok {
			u.IsVerified = true
			u.OTPCodeHash = nil
			u.OTPExpiresAt = nil
		}
		return nil
	}

	query := `
		UPDATE users
		SET is_verified = TRUE,
		    otp_code_hash = NULL,
		    otp_expires_at = NULL,
		    updated_at = NOW()
		WHERE phone_number = $1
	`
	_, err := r.db.ExecContext(ctx, query, phone)
	if err != nil {
		return fmt.Errorf("failed to verify user: %w", err)
	}
	return nil
}
