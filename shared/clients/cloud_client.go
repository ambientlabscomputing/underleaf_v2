package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// CloudClient is an HTTP client for the cloud-api registration endpoints.
type CloudClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewCloudClient() *CloudClient {
	cfg := utils.GetConfig(utils.OrchestratorConfig)
	return &CloudClient{
		baseURL: cfg.CloudAPIBaseURL,
		httpClient: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// ---- Request / Response types ----

type RegisterDeviceRequest struct {
	ProposedClusterName string `json:"proposed_cluster_name"`
	ProposedClusterID   string `json:"proposed_cluster_id"`
}

type DeviceAuthResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type PollTokenRequest struct {
	GrantType  string `json:"grant_type"`
	DeviceCode string `json:"device_code"`
}

type PollTokenResponse struct {
	OneTimeClusterToken string `json:"one_time_cluster_token"`
	ClusterID           string `json:"cluster_id"`
}

// ErrorResponse is the RFC 8628 error body returned by /registration/token.
type ErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

type IssueCertificateRequest struct {
	CSRPEM string `json:"csr_pem"`
}

type IssueCertificateResponse struct {
	CertificatePEM string `json:"certificate_pem"`
	CAChainPEM     string `json:"ca_chain_pem"`
}

// ---- Client methods ----

// RegisterDevice calls POST /api/v2/registration/device.
func (c *CloudClient) RegisterDevice(ctx context.Context, req RegisterDeviceRequest) (*DeviceAuthResponse, error) {
	var resp DeviceAuthResponse
	if err := c.post(ctx, "/api/v2/registration/device", req, &resp, ""); err != nil {
		return nil, fmt.Errorf("cloud: register device: %w", err)
	}
	return &resp, nil
}

// PollToken calls POST /api/v2/registration/token and returns (response, errorCode, error).
// errorCode is the RFC 8628 error string if the server returned 400; empty on success.
func (c *CloudClient) PollToken(ctx context.Context, deviceCode string) (*PollTokenResponse, string, error) {
	req := PollTokenRequest{
		GrantType:  "urn:ietf:params:oauth:grant-type:device_code",
		DeviceCode: deviceCode,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, "", err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v2/registration/token", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, "", fmt.Errorf("cloud: poll token: %w", err)
	}
	defer httpResp.Body.Close()
	respBody, _ := io.ReadAll(httpResp.Body)

	if httpResp.StatusCode == http.StatusOK {
		var tok PollTokenResponse
		if err := json.Unmarshal(respBody, &tok); err != nil {
			return nil, "", fmt.Errorf("cloud: poll token decode: %w", err)
		}
		return &tok, "", nil
	}

	if httpResp.StatusCode == http.StatusBadRequest {
		var errResp struct {
			Detail ErrorResponse `json:"detail"`
		}
		if err := json.Unmarshal(respBody, &errResp); err == nil && errResp.Detail.Error != "" {
			return nil, errResp.Detail.Error, nil
		}
	}

	return nil, "", fmt.Errorf("cloud: poll token: unexpected status %d: %s", httpResp.StatusCode, respBody)
}

// RequestCertificate calls POST /api/v2/registration/certificate with the one-time token.
func (c *CloudClient) RequestCertificate(ctx context.Context, oneTimeToken string, csrPEM string) (*IssueCertificateResponse, error) {
	var resp IssueCertificateResponse
	if err := c.post(ctx, "/api/v2/registration/certificate", IssueCertificateRequest{CSRPEM: csrPEM}, &resp, oneTimeToken); err != nil {
		return nil, fmt.Errorf("cloud: request certificate: %w", err)
	}
	return &resp, nil
}

// ---- Internal helpers ----

func (c *CloudClient) post(ctx context.Context, path string, reqBody any, respBody any, clusterToken string) error {
	b, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if clusterToken != "" {
		req.Header.Set("X-Cluster-Token", clusterToken)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, respBody)
}
