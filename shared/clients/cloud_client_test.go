package clients

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ambientlabscomputing/underleaf_v2/shared/types"
)

func TestRequestCertificate_SendsExpectedRequest(t *testing.T) {
	const wantToken = "one-time-token-abc"
	const wantCSR = "-----BEGIN CERTIFICATE REQUEST-----\nabc\n-----END CERTIFICATE REQUEST-----\n"
	const wantNodeID = "node_123"

	var gotPath, gotToken, gotMethod string
	var gotBody IssueCertificateRequest

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotToken = r.Header.Get("X-Cluster-Token")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(IssueCertificateResponse{
			CertificatePEM:     "cluster-cert-pem",
			NodeCertificatePEM: "node-cert-pem",
			CAChainPEM:         "ca-chain-pem",
		})
	}))
	defer srv.Close()

	c := &CloudClient{baseURL: srv.URL, httpClient: srv.Client()}

	resp, err := c.RequestCertificate(context.Background(), wantToken, wantCSR, wantNodeID)
	if err != nil {
		t.Fatalf("RequestCertificate returned error: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/registration/certificate" {
		t.Errorf("path = %q, want /registration/certificate", gotPath)
	}
	if gotToken != wantToken {
		t.Errorf("X-Cluster-Token = %q, want %q", gotToken, wantToken)
	}
	if gotBody.CSRPEM != wantCSR {
		t.Errorf("csr_pem = %q, want %q", gotBody.CSRPEM, wantCSR)
	}
	if gotBody.NodeID != wantNodeID {
		t.Errorf("node_id = %q, want %q", gotBody.NodeID, wantNodeID)
	}
	if resp.CertificatePEM != "cluster-cert-pem" || resp.NodeCertificatePEM != "node-cert-pem" || resp.CAChainPEM != "ca-chain-pem" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestRequestCertificate_PropagatesServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"detail":"invalid one-time token"}`))
	}))
	defer srv.Close()

	c := &CloudClient{baseURL: srv.URL, httpClient: srv.Client()}

	_, err := c.RequestCertificate(context.Background(), "bad-token", "csr", "node_123")
	if err == nil {
		t.Fatal("expected an error for a non-2xx response, got nil")
	}
}

func TestSyncNodes_PostsTheWholeNodeSetUnderTheClusterID(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody struct {
		Nodes []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			ClusterID string `json:"cluster_id"`
		} `json:"nodes"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_, _ = w.Write([]byte(`{"id":"cluster_1","name":"c","principal_account_id":"p"}`))
	}))
	defer srv.Close()
	c := &CloudClient{baseURL: srv.URL, httpClient: srv.Client()}

	err := c.SyncNodes(context.Background(), "cluster_1", []types.Node{{ID: "node_a", Name: "a"}, {ID: "node_b", Name: "b"}})
	if err != nil {
		t.Fatalf("SyncNodes: %v", err)
	}
	if gotMethod != http.MethodPost || gotPath != "/clusters/cluster_1/nodes/sync" {
		t.Errorf("%s %s", gotMethod, gotPath)
	}
	if len(gotBody.Nodes) != 2 || gotBody.Nodes[1].ID != "node_b" || gotBody.Nodes[0].ClusterID != "cluster_1" {
		t.Errorf("body = %+v", gotBody)
	}
}

func TestSyncNodes_ReportsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"detail":"Cluster not found"}`, http.StatusNotFound)
	}))
	defer srv.Close()
	c := &CloudClient{baseURL: srv.URL, httpClient: srv.Client()}
	if err := c.SyncNodes(context.Background(), "cluster_1", nil); err == nil {
		t.Fatal("expected an error")
	}
}
