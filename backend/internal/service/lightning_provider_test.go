package service

import (
	"context"
	"os"
	"testing"
)

func TestLNDRESTProviderLocalNode(t *testing.T) {
	macaroonHex := os.Getenv("LND_MACAROON_HEX")
	restURL := os.Getenv("LND_REST_URL")

	if restURL == "" || macaroonHex == "" {
		t.Skip("Skipping live local LND REST test: LND_REST_URL and LND_MACAROON_HEX not set")
	}

	provider := NewLNDRESTProvider(restURL, macaroonHex)
	if !provider.IsConfigured() {
		t.Fatalf("Expected LNDRESTProvider to be configured")
	}

	balance, err := provider.GetBalance(context.Background())
	if err != nil {
		t.Fatalf("Failed to query LND REST balance: %v", err)
	}

	t.Logf("LND Local Node Balance: %d sats", balance)
	if balance <= 0 {
		t.Errorf("Expected balance > 0, got %d", balance)
	}
}
