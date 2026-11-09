package clients

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
	"github.com/ambientlabscomputing/underleaf_v2/shared/utils"
)

// maxKeySetBytes bounds what an assertion key set fetch will read.
const maxKeySetBytes = 64 * 1024

// FetchAssertionKeys calls GET /assertion-keys and returns the raw JWKS body
// (the caller parses and validates it with assertion.ParseKeySet).
//
// A key set fetched over a network an attacker can influence would let them
// mint assertions, so the base URL must be https, or plain http to a loopback
// host (a local development cloud). Anything else is refused.
func (c *CloudClient) FetchAssertionKeys(ctx context.Context) ([]byte, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("cloud: bad base URL: %w", err)
	}
	if u.Scheme != "https" && !(u.Scheme == "http" && isLoopbackName(u.Hostname())) {
		return nil, fmt.Errorf("cloud: refusing to fetch assertion keys over %s://%s: https required", u.Scheme, u.Host)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.baseURL, "/")+"/assertion-keys", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cloud: fetch assertion keys: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cloud: fetch assertion keys: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxKeySetBytes+1))
	if err != nil {
		return nil, fmt.Errorf("cloud: read assertion keys: %w", err)
	}
	if len(body) > maxKeySetBytes {
		return nil, fmt.Errorf("cloud: assertion key set larger than %d bytes", maxKeySetBytes)
	}
	return body, nil
}

func isLoopbackName(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// NewPinnedCloudClient builds a CloudClient for fetching the cloud's assertion
// key set. Its connections are authenticated against the CA chain pinned at
// registration in cfg.CertDir (see PinnedTLSConfig). It needs no client
// certificate. The base URL is cfg.AssertionKeysBaseURL (the cloud's dedicated
// pinned listener) when set, else cfg.CloudAPIBaseURL.
func NewPinnedCloudClient(cfg utils.Config) (*CloudClient, error) {
	hc, err := NewPinnedHTTPClient(cfg.CertDir+"/"+string(types.CertFileNameCAChain), 15*time.Second)
	if err != nil {
		return nil, err
	}
	base := cfg.AssertionKeysBaseURL
	if base == "" {
		base = cfg.CloudAPIBaseURL
	}
	return NewCloudClientWithBaseURL(base, hc), nil
}
