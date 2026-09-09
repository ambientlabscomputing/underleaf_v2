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

// generateKeyAndCSR creates a fresh RSA keypair and a CSR for it with the
// given CN. Shared by initial registration and renewal so both produce
// identical CSR shapes.
func generateKeyAndCSR(commonName string) (privKey *rsa.PrivateKey, csrPEM string, err error) {
	privKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, "", fmt.Errorf("generate key: %w", err)
	}

	csrTemplate := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}
	csrDER, err := x509.CreateCertificateRequest(rand.Reader, csrTemplate, privKey)
	if err != nil {
		return nil, "", fmt.Errorf("create CSR: %w", err)
	}
	csrPEM = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csrDER}))
	return privKey, csrPEM, nil
}

// persistCertFiles writes the four PEM files that make up a cluster's mTLS
// identity to certDir. Shared by initial registration and renewal.
func persistCertFiles(certDir, certPEM, keyPEM, caChainPEM, nodeCertPEM string) error {
	if certDir == "" {
		return nil
	}
	if err := os.MkdirAll(certDir, 0700); err != nil {
		return fmt.Errorf("create cert dir: %w", err)
	}

	files := []struct {
		name string
		data string
		perm os.FileMode
	}{
		{string(types.CertFileNameClusterCert), certPEM, 0600},
		{string(types.CertFileNameClusterKey), keyPEM, 0600},
		{string(types.CertFileNameCAChain), caChainPEM, 0644},
		{string(types.CertFileNameNodeCert), nodeCertPEM, 0600},
	}
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(certDir, f.name), []byte(f.data), f.perm); err != nil {
			return fmt.Errorf("write %s: %w", f.name, err)
		}
	}
	return nil
}

func (s *RegistrationService) requestCertificate(oneTimeToken, clusterID, certDir, nodeID string) (certPEM, keyPEM string, err error) {
	privKey, csrPEM, err := generateKeyAndCSR(clusterID)
	if err != nil {
		return "", "", err
	}

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

	if err := persistCertFiles(certDir, certPEM, keyPEM, resp.CAChainPEM, resp.NodeCertificatePEM); err != nil {
		return "", "", fmt.Errorf("persist cert: %w", err)
	}

	return certPEM, keyPEM, nil
}

// certRenewalThreshold is how far ahead of a cert's expiry CheckAndRenewCertificate
// attempts renewal. Renewal authenticates with the *existing* client cert (see
// CloudClient.RenewCertificate), so this must fire well before actual expiry —
// once a cert has expired, nginx's TLS handshake rejects the caller before any
// renewal request can even be made. Certs are issued with 730-day validity
// (cert_lib.py), so a 30-day threshold leaves a wide margin.
const certRenewalThreshold = 30 * 24 * time.Hour

// CheckAndRenewCertificate re-issues the cluster's mTLS certificate if it's
// within certRenewalThreshold of expiring. Safe to call on a timer: it's a
// no-op both when no cert has been issued yet (nothing to renew) and when
// the existing cert isn't due for renewal yet.
func (s *RegistrationService) CheckAndRenewCertificate(ctx context.Context, certDir string) error {
	logger := utils.LoggerFromContext(ctx)

	certPEM, err := os.ReadFile(filepath.Join(certDir, string(types.CertFileNameClusterCert)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read cluster cert: %w", err)
	}

	block, _ := pem.Decode(certPEM)
	if block == nil {
		return fmt.Errorf("decode cluster cert: no PEM block found")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("parse cluster cert: %w", err)
	}

	if time.Until(cert.NotAfter) > certRenewalThreshold {
		return nil
	}
	logger.Info("registration: certificate renewal due", "not_after", cert.NotAfter)

	node, err := s.agentClient.GetNode(ctx)
	if err != nil {
		return fmt.Errorf("get node: %w", err)
	}

	privKey, csrPEM, err := generateKeyAndCSR(cert.Subject.CommonName)
	if err != nil {
		return err
	}

	resp, err := s.cloudClient.RenewCertificate(ctx, csrPEM, node.Id)
	if err != nil {
		return fmt.Errorf("renew certificate: %w", err)
	}

	keyPEM := string(pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	}))

	if err := persistCertFiles(certDir, resp.CertificatePEM, keyPEM, resp.CAChainPEM, resp.NodeCertificatePEM); err != nil {
		return fmt.Errorf("persist renewed cert: %w", err)
	}

	logger.Info("registration: certificate renewed successfully")
	return nil
}
