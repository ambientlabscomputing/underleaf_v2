package clients

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
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

func NewCloudClientWithCert(cfg utils.Config) (*CloudClient, error) {
	hc, err := HttpClientWithCert(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client with cert: %w", err)
	}
	return &CloudClient{
		baseURL:    cfg.CloudAPIBaseURL,
		httpClient: hc,
	}, nil
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
	NodeID string `json:"node_id"`
}

type IssueCertificateResponse struct {
	CertificatePEM     string `json:"certificate_pem"`
	NodeCertificatePEM string `json:"node_certificate_pem"`
	CAChainPEM         string `json:"ca_chain_pem"`
}

// ---- Client methods ----

// RegisterDevice calls POST /registration/device.
func (c *CloudClient) RegisterDevice(ctx context.Context, req RegisterDeviceRequest) (*DeviceAuthResponse, error) {
	var resp DeviceAuthResponse
	if err := c.postWithClusterToken(ctx, "/registration/device", req, &resp, ""); err != nil {
		return nil, fmt.Errorf("cloud: register device: %w", err)
	}
	return &resp, nil
}

// PollToken calls POST /registration/token and returns (response, errorCode, error).
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

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/registration/token", bytes.NewReader(body))
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

// RequestCertificate calls POST /registration/certificate with the one-time token.
func (c *CloudClient) RequestCertificate(ctx context.Context, oneTimeToken string, csrPEM, nodeID string) (*IssueCertificateResponse, error) {
	var resp IssueCertificateResponse
	if err := c.postWithClusterToken(
		ctx,
		"/registration/certificate",
		IssueCertificateRequest{
			CSRPEM: csrPEM,
			NodeID: nodeID,
		},
		&resp,
		oneTimeToken,
	); err != nil {
		return nil, fmt.Errorf("cloud: request certificate: %w", err)
	}
	return &resp, nil
}

func (c *CloudClient) CreateConnection(ctx context.Context, req types.CreateConnectionRequest) (*types.Connection, error) {
	var resp types.Connection
	if err := c.post(ctx, "/connections", req, &resp); err != nil {
		return nil, fmt.Errorf("cloud: create connection: %w", err)
	}
	return &resp, nil
}

func (c *CloudClient) GetConnection(ctx context.Context, connectionID string) (*types.Connection, error) {
	var resp types.Connection
	if err := c.get(ctx, fmt.Sprintf("/connections/%s", connectionID), &resp, nil); err != nil {
		return nil, fmt.Errorf("cloud: get connection: %w", err)
	}
	return &resp, nil
}

func (c *CloudClient) TerminateConnection(ctx context.Context, connectionID string) error {
	if err := c.delete(ctx, fmt.Sprintf("/connections/%s", connectionID), nil); err != nil {
		return fmt.Errorf("cloud: terminate connection: %w", err)
	}
	return nil
}

func (c *CloudClient) ListConnections(ctx context.Context, query types.QueryConnectionsRequest) ([]types.Connection, error) {
	params := make(map[string]string)
	if query.NodeID != nil {
		params["node_id"] = *query.NodeID
	}
	if query.Name != nil {
		params["name"] = *query.Name
	}
	var listResp types.QueryConnectionsResponse
	if err := c.get(ctx, "/connections", &listResp, params); err != nil {
		return nil, fmt.Errorf("cloud: list connections: %w", err)
	}
	conns := make([]types.Connection, len(listResp.Items))
	for i, c := range listResp.Items {
		conns[i] = types.Connection{
			ID:        c.ID,
			NodeID:    c.NodeID,
			Name:      c.Name,
			State:     c.State,
			Status:    c.Status,
			CreatedAt: c.CreatedAt,
		}
	}
	return conns, nil
}

func (c *CloudClient) NewStream(ctx context.Context, req types.NewStreamRequest) (*types.Stream, error) {
	var resp types.Stream
	if err := c.post(ctx, "/streams", req, &resp); err != nil {
		return nil, fmt.Errorf("cloud: new stream: %w", err)
	}
	return &resp, nil
}

func (c *CloudClient) CloseStream(ctx context.Context, streamID string) error {
	if err := c.delete(ctx, fmt.Sprintf("/streams/%s", streamID), nil); err != nil {
		return fmt.Errorf("cloud: close stream: %w", err)
	}
	return nil
}

// ---- Internal helpers ----
func (c *CloudClient) post(ctx context.Context, path string, reqBody any, respBody any) error {
	return c.httpRequest(ctx, http.MethodPost, path, reqBody, respBody, nil)
}

func (c *CloudClient) patch(ctx context.Context, path string, reqBody any, respBody any) error {
	return c.httpRequest(ctx, http.MethodPatch, path, reqBody, respBody, nil)
}

func (c *CloudClient) put(ctx context.Context, path string, reqBody any, respBody any) error {
	return c.httpRequest(ctx, http.MethodPut, path, reqBody, respBody, nil)
}

func (c *CloudClient) get(ctx context.Context, path string, respBody any, params map[string]string) error {
	return c.httpRequest(ctx, http.MethodGet, path, nil, respBody, params)
}

func (c *CloudClient) delete(ctx context.Context, path string, respBody any) error {
	return c.httpRequest(ctx, http.MethodDelete, path, nil, respBody, nil)
}

// httpRequest sends a HTTP request, it is assumed that the http client uses mTLS, so no auth headers are needed.
func (c *CloudClient) httpRequest(ctx context.Context, httpMethod, path string, reqBody any, respBody any, params map[string]string) error {
	logger := utils.LoggerFromContext(ctx).With("method", httpMethod, "path", path)
	if len(params) > 0 {
		q := "?"
		for k, v := range params {
			q += fmt.Sprintf("%s=%s&", k, v)
		}
		path += q[:len(q)-1]
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		logger.Error("failed to marshal request body", "error", err)
		return err
	}
	logger.Debug("sending HTTP request", "body", string(b))
	req, err := http.NewRequestWithContext(ctx, httpMethod, c.baseURL+path, bytes.NewReader(b))
	if err != nil {
		logger.Error("failed to create HTTP request", "error", err)
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		logger.Error("failed to do HTTP request", "error", err)
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	logger.Debug("received HTTP response", "status", resp.StatusCode, "body", string(body))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Error("HTTP request failed", "status", resp.StatusCode, "body", string(body))
		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	return json.Unmarshal(body, respBody)
}

// postWithClusterToken sends a post with the given cluster token (or no token if empty) and decodes the JSON response into respBody.
// Token is ONLY for the onboarding flow; normal requests should use the standard post() method.
func (c *CloudClient) postWithClusterToken(ctx context.Context, path string, reqBody any, respBody any, clusterToken string) error {
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
