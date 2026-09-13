package config

import (
	"context"
	"encoding/hex"
	"log"
	"os"
	"strings"

	"yebente/internal/secret"
)

type Config struct {
	Port        string
	Environment string
	DatabaseURL string

	// GCP Configuration
	GCPProjectID string

	// Daraja Settings
	DarajaBaseURL     string
	DarajaConsumerKey string
	DarajaConsumerSecret string
	DarajaPasskey     string
	DarajaShortCode   string
	DarajaCallbackURL string

	// LND Settings
	LNDGRPCAddress string
	LNDTLSCert     []byte
	LNDMacaroonHex string

	// Exchange Rate Settings (KES per BTC)
	DefaultKESPerBTC float64
}

// loadDotEnv reads key-value pairs from .env files if they exist and sets them into os.Environ
func loadDotEnv() {
	envFiles := []string{".env", "../.env", "backend/.env", "../../.env"}
	for _, file := range envFiles {
		content, err := os.ReadFile(file)
		if err == nil {
			lines := strings.Split(string(content), "\n")
			for _, line := range lines {
				trimmed := strings.TrimSpace(line)
				if trimmed == "" || strings.HasPrefix(trimmed, "#") {
					continue
				}
				parts := strings.SplitN(trimmed, "=", 2)
				if len(parts) == 2 {
					key := strings.TrimSpace(parts[0])
					val := strings.TrimSpace(parts[1])
					val = strings.Trim(val, `"'`)
					if os.Getenv(key) == "" {
						os.Setenv(key, val)
					}
				}
			}
			log.Printf("Loaded environment configuration from %s", file)
			break
		}
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func LoadConfig(ctx context.Context) (*Config, error) {
	loadDotEnv()
	cfg := &Config{
		Port:             getEnv("PORT", "8080"),
		Environment:      getEnv("ENVIRONMENT", "development"),
		DatabaseURL:      getEnv("DATABASE_URL", "postgres://yebente:password@localhost:5432/yebente_db?sslmode=disable"),
		GCPProjectID:     getEnv("GCP_PROJECT_ID", ""),
		DarajaBaseURL:    getEnv("DARAJA_BASE_URL", "https://sandbox.safaricom.co.ke"),
		DarajaConsumerKey: getEnv("DARAJA_CONSUMER_KEY", ""),
		DarajaConsumerSecret: getEnv("DARAJA_CONSUMER_SECRET", ""),
		DarajaPasskey:    getEnv("DARAJA_PASSKEY", ""),
		DarajaShortCode:  getEnv("DARAJA_SHORTCODE", "174379"),
		DarajaCallbackURL: getEnv("DARAJA_CALLBACK_URL", "https://api.yebente.com/v1/mpesa/callback"),
		LNDGRPCAddress:   getEnv("LND_GRPC_ADDRESS", "localhost:10009"),
		DefaultKESPerBTC: 8500000.0, // Default 8.5M KES / BTC (~$65k)
	}

	// Read direct env vars for LND if provided
	if certEnv := os.Getenv("LND_TLS_CERT"); certEnv != "" {
		cfg.LNDTLSCert = []byte(certEnv)
	} else if certPath := os.Getenv("LND_TLS_CERT_PATH"); certPath != "" {
		if certBytes, err := os.ReadFile(certPath); err == nil {
			cfg.LNDTLSCert = certBytes
		}
	}

	if macEnv := os.Getenv("LND_MACAROON_HEX"); macEnv != "" {
		cfg.LNDMacaroonHex = strings.TrimSpace(macEnv)
	} else if macPath := os.Getenv("LND_MACAROON_PATH"); macPath != "" {
		if macBytes, err := os.ReadFile(macPath); err == nil {
			cfg.LNDMacaroonHex = hex.EncodeToString(macBytes)
		}
	}

	// If GCP Project ID is set, optionally resolve secrets from Secret Manager
	if cfg.GCPProjectID != "" {
		secretClient, err := secret.NewSecretManagerClient(ctx, cfg.GCPProjectID)
		if err == nil {
			defer secretClient.Close()

			if secretName := os.Getenv("SECRET_DARAJA_CONSUMER_KEY"); secretName != "" {
				if val, err := secretClient.AccessSecret(ctx, secretName); err == nil {
					cfg.DarajaConsumerKey = val
				}
			}
			if secretName := os.Getenv("SECRET_DARAJA_CONSUMER_SECRET"); secretName != "" {
				if val, err := secretClient.AccessSecret(ctx, secretName); err == nil {
					cfg.DarajaConsumerSecret = val
				}
			}
			if secretName := os.Getenv("SECRET_DARAJA_PASSKEY"); secretName != "" {
				if val, err := secretClient.AccessSecret(ctx, secretName); err == nil {
					cfg.DarajaPasskey = val
				}
			}
			if secretName := os.Getenv("SECRET_LND_MACAROON_HEX"); secretName != "" {
				if val, err := secretClient.AccessSecret(ctx, secretName); err == nil {
					cfg.LNDMacaroonHex = strings.TrimSpace(val)
				}
			}
			if secretName := os.Getenv("SECRET_LND_TLS_CERT"); secretName != "" {
				if val, err := secretClient.AccessSecret(ctx, secretName); err == nil {
					cfg.LNDTLSCert = []byte(val)
				}
			}
		} else {
			log.Printf("GCP Secret Manager client not initialized: %v (falling back to env vars)", err)
		}
	}

	return cfg, nil
}
