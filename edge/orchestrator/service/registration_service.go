package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/edge/orchestrator/repository"
	"github.com/ambientlabscomputing/underleaf_v2/shared/clients"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

const (
	registrationPollMaxRetries = 120 // ~10 min at 5s interval
)

// RegistrationState is the result of InitiateRegistration, returned to the CLI.
type RegistrationState struct {
	UserCode                string
	VerificationURIComplete string
	ExpiresIn               int
	Interval                int
}

// RegistrationService handles the device-auth flow with cloud-api.
type RegistrationService struct {
	repo        *repository.RegistrationRepository
	cloudClient *clients.CloudClient
}

func NewRegistrationService(repo *repository.RegistrationRepository, cloudClient *clients.CloudClient) *RegistrationService {
	return &RegistrationService{
		repo:        repo,
		cloudClient: cloudClient,
	}
}

// InitiateRegistration calls cloud-api, persists state, and starts the poll loop in a goroutine.
// Returns the registration state for the CLI to display to the user.
func (s *RegistrationService) InitiateRegistration(ctx context.Context, clusterName, clusterID string) (*RegistrationState, error) {
	resp, err := s.cloudClient.RegisterDevice(ctx, clients.RegisterDeviceRequest{
		ProposedClusterName: clusterName,
		ProposedClusterID:   clusterID,
	})
	if err != nil {
		return nil, fmt.Errorf("registration: register device: %w", err)
	}

	reg := &repository.ClusterRegistration{
		DeviceCode:              resp.DeviceCode,
		UserCode:                resp.UserCode,
		VerificationURIComplete: resp.VerificationURIComplete,
		Status:                  repository.RegistrationStatusPending,
		ExpiresAt:               time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		CreatedAt:               time.Now(),
	}
	if err := s.repo.Create(reg); err != nil {
		return nil, fmt.Errorf("registration: persist state: %w", err)
	}

	interval := resp.Interval
	if interval <= 0 {
		interval = 5
	}

	// Start background poll loop
	go s.pollLoop(resp.DeviceCode, interval)

	return &RegistrationState{
		UserCode:                resp.UserCode,
		VerificationURIComplete: resp.VerificationURIComplete,
		ExpiresIn:               resp.ExpiresIn,
		Interval:                interval,
	}, nil
}

// GetStatus returns the current registration status.
func (s *RegistrationService) GetStatus() (string, error) {
	reg, err := s.repo.GetLatest()
	if err != nil {
		return "", err
	}
	if reg == nil {
		return "not_started", nil
	}
	return reg.Status, nil
}

// pollLoop polls cloud-api until approval, then requests a cert.
func (s *RegistrationService) pollLoop(deviceCode string, intervalSecs int) {
	cfg := utils.GetConfig(utils.OrchestratorConfig)
	tick := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer tick.Stop()

	for range tick.C {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		tokenResp, errCode, err := s.cloudClient.PollToken(ctx, deviceCode)
		cancel()

		if err != nil {
			fmt.Printf("[registration] poll error: %v\n", err)
			_ = s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusFailed, "", "", "")
			return
		}

		switch errCode {
		case "authorization_pending":
			continue
		case "slow_down":
			tick.Reset(time.Duration(intervalSecs+5) * time.Second)
			continue
		case "expired_token":
			_ = s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusExpired, "", "", "")
			return
		case "":
			// success — fall through
		default:
			fmt.Printf("[registration] unexpected error code: %s\n", errCode)
			_ = s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusFailed, "", "", "")
			return
		}

		// Approved — generate keypair, CSR, and request certificate
		certPEM, keyPEM, err := s.requestCertificate(tokenResp.OneTimeClusterToken, tokenResp.ClusterID, cfg.CertDir)
		if err != nil {
			fmt.Printf("[registration] certificate error: %v\n", err)
			_ = s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusFailed, tokenResp.ClusterID, "", "")
			return
		}

		if err := s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusRegistered, tokenResp.ClusterID, certPEM, keyPEM); err != nil {
			fmt.Printf("[registration] persist cert error: %v\n", err)
			return
		}
		fmt.Printf("[registration] cluster %s successfully registered\n", tokenResp.ClusterID)
		return
	}
}

func (s *RegistrationService) requestCertificate(oneTimeToken, clusterID, certDir string) (certPEM, keyPEM string, err error) {
	// Generate RSA keypair
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", "", fmt.Errorf("generate key: %w", err)
	}

	// Build CSR
	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: clusterID},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, privKey)
	if err != nil {
		return "", "", fmt.Errorf("create CSR: %w", err)
	}
	csrPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	resp, err := s.cloudClient.RequestCertificate(ctx, oneTimeToken, csrPEM)
	if err != nil {
		return "", "", fmt.Errorf("request certificate: %w", err)
	}

	certPEM = resp.CertificatePEM
	keyPEM = string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	}))

	// Persist cert and key to disk
	if certDir != "" {
		if err := os.MkdirAll(certDir, 0700); err == nil {
			_ = os.WriteFile(filepath.Join(certDir, "cluster_cert.pem"), []byte(certPEM), 0600)
			_ = os.WriteFile(filepath.Join(certDir, "cluster_key.pem"), []byte(keyPEM), 0600)
			_ = os.WriteFile(filepath.Join(certDir, "ca_chain.pem"), []byte(resp.CAChainPEM), 0644)
		}
	}

	return certPEM, keyPEM, nil
}
