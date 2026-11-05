package pki

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// JoinPath is the path of the join endpoint on the cluster listener's join side.
const JoinPath = "/v1/join"

// JoinNode is the host information a joining node reports. None of it is used
// for authentication; the node id comes from the CSR.
type JoinNode struct {
	Name   string `json:"name"`
	IPAddr string `json:"ip_address"`
	OS     string `json:"os"`
	Arch   string `json:"arch"`
}

// JoinRequest is the body of POST JoinPath.
type JoinRequest struct {
	Secret string   `json:"secret"` // base64url of the 32-byte secret
	CSRPEM string   `json:"csr_pem"`
	Node   JoinNode `json:"node"`
}

// JoinResponse is the manager's answer: the node certificate and the CA bundle.
type JoinResponse struct {
	NodeID      string    `json:"node_id"`
	CertPEM     string    `json:"cert_pem"`
	CABundlePEM string    `json:"ca_bundle_pem"`
	NotAfter    time.Time `json:"not_after"`
}

// ErrJoinRejected means the manager answered but refused the join (the
// secret is wrong, used, or expired). The manager deliberately does not say
// which.
var ErrJoinRejected = errors.New("the manager rejected the join secret (invalid, already used, or expired): ask for a fresh one with `orcli nodes add`")

// JoinResult is a completed enrollment, ready for SaveMember.
type JoinResult struct {
	NodeID   string
	Key      *ecdsa.PrivateKey
	CertPEM  []byte
	Bundle   []byte
	NotAfter time.Time
}

// Join performs the node side of the join protocol (RFD 5, "Join flow"):
//
//  1. TLS-connect to the manager and verify its certificate against the CA
//     fingerprint from the join string. This happens in the handshake, i.e.
//     before the secret is sent.
//  2. Send the secret and a CSR whose CN is nodeID.
//  3. Check what comes back really is a certificate for nodeID, for our key,
//     issued under the pinned CA.
func Join(ctx context.Context, info JoinInfo, nodeID string, node JoinNode) (*JoinResult, error) {
	key, csrPEM, err := GenerateNodeKey(nodeID)
	if err != nil {
		return nil, err
	}
	body, err := json.Marshal(JoinRequest{
		Secret: base64.RawURLEncoding.EncodeToString(info.Secret),
		CSRPEM: string(csrPEM),
		Node:   node,
	})
	if err != nil {
		return nil, err
	}

	hc := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig:   PinnedClientTLS(info.Fingerprint),
			DisableKeepAlives: true,
			ForceAttemptHTTP2: false,
			TLSNextProto:      map[string]func(string, *tls.Conn) http.RoundTripper{},
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+info.Addr+JoinPath, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("could not join %s: %w", info.Addr, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrJoinRejected
	default:
		return nil, fmt.Errorf("join failed: manager answered %s: %s", resp.Status, bytes.TrimSpace(raw))
	}
	var jr JoinResponse
	if err := json.Unmarshal(raw, &jr); err != nil {
		return nil, fmt.Errorf("join: malformed response: %w", err)
	}
	return verifyIssued(info, nodeID, key, jr)
}

func verifyIssued(info JoinInfo, nodeID string, key *ecdsa.PrivateKey, jr JoinResponse) (*JoinResult, error) {
	if jr.NodeID != nodeID {
		return nil, fmt.Errorf("join: manager answered for node %q, expected %q", jr.NodeID, nodeID)
	}
	certs, err := ParseCertificates([]byte(jr.CertPEM))
	if err != nil {
		return nil, fmt.Errorf("join: issued certificate: %w", err)
	}
	leaf := certs[0]
	if leaf.Subject.CommonName != nodeID {
		return nil, fmt.Errorf("join: issued certificate is for %q, not %q", leaf.Subject.CommonName, nodeID)
	}
	pub, ok := leaf.PublicKey.(*ecdsa.PublicKey)
	if !ok || !pub.Equal(&key.PublicKey) {
		return nil, errors.New("join: issued certificate is for a different key")
	}
	bundle, err := ParseCertificates([]byte(jr.CABundlePEM))
	if err != nil {
		return nil, fmt.Errorf("join: CA bundle: %w", err)
	}
	var pinned bool
	for _, c := range bundle {
		if Fingerprint(c) == info.Fingerprint {
			pinned = true
		}
	}
	if !pinned {
		return nil, errors.New("join: the returned CA bundle does not contain the pinned CA")
	}
	pool, err := CertPool([]byte(jr.CABundlePEM))
	if err != nil {
		return nil, err
	}
	if _, err := leaf.Verify(verifyClientOpts(pool)); err != nil {
		return nil, fmt.Errorf("join: issued certificate does not verify against the CA bundle: %w", err)
	}
	return &JoinResult{
		NodeID:   nodeID,
		Key:      key,
		CertPEM:  []byte(jr.CertPEM),
		Bundle:   []byte(jr.CABundlePEM),
		NotAfter: leaf.NotAfter,
	}, nil
}
