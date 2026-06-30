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
	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
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
	agentClient *clients.AgentClient
}

func NewRegistrationService(
	repo *repository.RegistrationRepository,
	cloudClient *clients.CloudClient,
	agentClient *clients.AgentClient,
) *RegistrationService {
	return &RegistrationService{
		repo:        repo,
		cloudClient: cloudClient,
		agentClient: agentClient,
	}
}

// InitiateRegistration calls cloud-api, persists state, and starts the poll loop in a goroutine.
// Returns the registration state for the CLI to display to the user.
func (s *RegistrationService) InitiateRegistration(ctx context.Context, clusterName, clusterID string) (*RegistrationState, error) {
	logger := utils.LoggerFromContext(ctx).With("cluster_name", clusterName, "cluster_id", clusterID)
	logger.Info("registration: initiating cloud registration")

	// Call cloud-api to initiate device auth flow
	resp, err := s.cloudClient.RegisterDevice(ctx, clients.RegisterDeviceRequest{
		ProposedClusterName: clusterName,
		ProposedClusterID:   clusterID,
	})
	if err != nil {
		logger.Error("registration: register device failure", "error", err)
		return nil, fmt.Errorf("registration: register device: %w", err)
	}

	// Persist state to the repository
	reg := &repository.ClusterRegistration{
		DeviceCode:              resp.DeviceCode,
		UserCode:                resp.UserCode,
		VerificationURIComplete: resp.VerificationURIComplete,
		Status:                  repository.RegistrationStatusPending,
		ExpiresAt:               time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		CreatedAt:               time.Now(),
	}
	if err := s.repo.Create(reg); err != nil {
		logger.Error("registration: persist state failure", "error", err)
		return nil, fmt.Errorf("registration: persist state: %w", err)
	}

	interval := resp.Interval
	if interval <= 0 {
		interval = 5
	}

	// Start background poll loop
	node, err := s.agentClient.GetNode(ctx)
	if err != nil {
		logger.Error("registration: get node failure", "error", err)
		return nil, fmt.Errorf("registration: get node: %w", err)
	}
	logger = logger.With("node_id", node.Id, "node_name", node.Name, "node_ip", node.IpAddress)
	logger.Info("registration: starting poll loop")
	ctx = utils.ContextWithLogger(ctx, logger, nil)
	go s.pollLoop(ctx, resp.DeviceCode, interval, node.Id)

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
func (s *RegistrationService) pollLoop(ctx context.Context, deviceCode string, intervalSecs int, nodeID string) {
	logger := utils.LoggerFromContext(ctx).With("device_code", deviceCode, "interval_secs", intervalSecs)
	cfg := utils.GetConfig(utils.OrchestratorConfig)
	tick := time.NewTicker(time.Duration(intervalSecs) * time.Second)
	defer tick.Stop()

	logger.Debug("registration: poll loop started")
	for range tick.C {
		// create a new context with timeout for each poll request
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		// call cloud-api to poll for token
		tokenResp, errCode, err := s.cloudClient.PollToken(ctx, deviceCode)
		cancel()

		if err != nil {
			logger.Error("registration: poll token failure", "error", err)
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
			logger.Error("registration: unexpected error code", "error_code", errCode)
			_ = s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusFailed, "", "", "")
			return
		}

		// Approved — generate keypair, CSR, and request certificate
		certPEM, keyPEM, err := s.requestCertificate(tokenResp.OneTimeClusterToken, tokenResp.ClusterID, cfg.CertDir, nodeID)
		if err != nil {
			logger.Error("registration: certificate error", "error", err)
			_ = s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusFailed, tokenResp.ClusterID, "", "")
			return
		}
		logger.Debug("registration: certificate successfully obtained")

		if err := s.repo.UpdateStatus(deviceCode, repository.RegistrationStatusRegistered, tokenResp.ClusterID, certPEM, keyPEM); err != nil {
			logger.Error("registration: persist cert error", "error", err)
			return
		}
		logger.Info("registration: cluster successfully registered", "cluster_id", tokenResp.ClusterID)
		return
	}
}

func (s *RegistrationService) requestCertificate(oneTimeToken, clusterID, certDir, nodeID string) (certPEM, keyPEM string, err error) {
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

	resp, err := s.cloudClient.RequestCertificate(ctx, oneTimeToken, csrPEM, nodeID)
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
			_ = os.WriteFile(filepath.Join(certDir, string(types.CertFileNameClusterCert)), []byte(certPEM), 0600)
			_ = os.WriteFile(filepath.Join(certDir, string(types.CertFileNameClusterKey)), []byte(keyPEM), 0600)
			_ = os.WriteFile(filepath.Join(certDir, string(types.CertFileNameCAChain)), []byte(resp.CAChainPEM), 0644)
			_ = os.WriteFile(filepath.Join(certDir, string(types.CertFileNameNodeCert)), []byte(resp.NodeCertificatePEM), 0600)
		}
	}

	return certPEM, keyPEM, nil
}
