CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

DO $$ BEGIN
    CREATE TYPE transaction_status AS ENUM (
        'PENDING_STK',
        'MPESA_CONFIRMED',
        'MPESA_FAILED',
        'LIGHTNING_PENDING',
        'SETTLED',
        'SETTLEMENT_FAILED'
    );
EXCEPTION
    WHEN duplicate_object THEN null;
END $$;

CREATE TABLE IF NOT EXISTS users (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    phone_number VARCHAR(15) UNIQUE NOT NULL, -- e.g., +254707172370
    is_verified BOOLEAN DEFAULT FALSE,
    otp_code_hash VARCHAR(255),
    otp_expires_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS transactions (
    id UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    merchant_request_id VARCHAR(64) UNIQUE,
    checkout_request_id VARCHAR(64) UNIQUE,
    mpesa_receipt_number VARCHAR(64) UNIQUE,
    amount_kes NUMERIC(10, 2) NOT NULL,
    amount_sats BIGINT NOT NULL,
    lightning_invoice TEXT NOT NULL,
    lightning_payment_hash VARCHAR(64) UNIQUE,
    lightning_preimage VARCHAR(64),
    status transaction_status DEFAULT 'PENDING_STK' NOT NULL,
    failure_reason TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_transactions_checkout_request ON transactions(checkout_request_id);
CREATE INDEX IF NOT EXISTS idx_transactions_status ON transactions(status);
