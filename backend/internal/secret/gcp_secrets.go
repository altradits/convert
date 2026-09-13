package secret

import (
	"context"
	"fmt"

	secretmanager "cloud.google.com/go/secretmanager/apiv1"
	"cloud.google.com/go/secretmanager/apiv1/secretmanagerpb"
)

type SecretManagerClient struct {
	client    *secretmanager.Client
	projectID string
}

func NewSecretManagerClient(ctx context.Context, projectID string) (*SecretManagerClient, error) {
	client, err := secretmanager.NewClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create secretmanager client: %w", err)
	}
	return &SecretManagerClient{client: client, projectID: projectID}, nil
}

func (s *SecretManagerClient) AccessSecret(ctx context.Context, secretName string) (string, error) {
	name := fmt.Sprintf("projects/%s/secrets/%s/versions/latest", s.projectID, secretName)
	req := &secretmanagerpb.AccessSecretVersionRequest{Name: name}
	result, err := s.client.AccessSecretVersion(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to access secret %s: %w", secretName, err)
	}
	return string(result.Payload.Data), nil
}

func (s *SecretManagerClient) Close() {
	if s.client != nil {
		_ = s.client.Close()
	}
}
