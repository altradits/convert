package service

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/lightningnetwork/lnd/lnrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type LightningServiceInterface interface {
	PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error)
	Close() error
}

type MacaroonCredential struct {
	macaroonHex string
}

func (m *MacaroonCredential) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	return map[string]string{"macaroon": m.macaroonHex}, nil
}

func (m *MacaroonCredential) RequireTransportSecurity() bool {
	return true
}

type LNDService struct {
	client lnrpc.LightningClient
	conn   *grpc.ClientConn
}

func NewLNDService(grpcAddress string, tlsCertBytes []byte, macaroonHex string) (*LNDService, error) {
	cp := x509.NewCertPool()
	if !cp.AppendCertsFromPEM(tlsCertBytes) {
		return nil, errors.New("failed to append LND TLS certificate to pool")
	}

	tlsConfig := &tls.Config{
		RootCAs:            cp,
		InsecureSkipVerify: false,
	}

	creds := credentials.NewTLS(tlsConfig)
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		grpc.WithPerRPCCredentials(&MacaroonCredential{macaroonHex: macaroonHex}),
		grpc.WithBlock(),
	}

	conn, err := grpc.Dial(grpcAddress, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to LND at %s: %w", grpcAddress, err)
	}

	return &LNDService{
		client: lnrpc.NewLightningClient(conn),
		conn:   conn,
	}, nil
}

func (l *LNDService) PayInvoice(ctx context.Context, invoice string, maxFeeSats int64) (string, string, error) {
	req := &lnrpc.SendRequest{
		PaymentRequest: invoice,
		FeeLimit: &lnrpc.FeeLimit{
			Limit: &lnrpc.FeeLimit_Fixed{
				Fixed: maxFeeSats,
			},
		},
	}

	resp, err := l.client.SendPaymentSync(ctx, req)
	if err != nil {
		return "", "", fmt.Errorf("LND send payment call failed: %w", err)
	}

	if resp.PaymentError != "" {
		return "", "", fmt.Errorf("payment route error: %s", resp.PaymentError)
	}

	paymentHash := hex.EncodeToString(resp.PaymentHash)
	preimage := hex.EncodeToString(resp.PaymentPreimage)
	return paymentHash, preimage, nil
}

func (l *LNDService) Close() error {
	if l.conn != nil {
		return l.conn.Close()
	}
	return nil
}
