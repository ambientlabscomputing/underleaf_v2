package remoteaccess

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ConnectOptions configures Connect.
type ConnectOptions struct {
	// Cred is the stored login; the proxy only ever talks to Cred.CloudURL.
	Cred      Credentials
	ClusterID string
	// Port is the local port; 0 picks a free one.
	Port int
	Out  io.Writer
	// Transport overrides the HTTP transport to the cloud (tests).
	Transport http.RoundTripper
	// Ready, if set, receives the listen address once the proxy is serving.
	Ready func(addr string)
}

// Connect serves http://localhost:<port> until ctx is cancelled. It binds
// 127.0.0.1 only: there is deliberately no option to listen elsewhere.
func Connect(ctx context.Context, o ConnectOptions) error {
	if o.ClusterID == "" || strings.ContainsAny(o.ClusterID, "/?#% ") {
		return fmt.Errorf("invalid cluster id %q", o.ClusterID)
	}
	if _, err := CheckCloudURL(o.Cred.CloudURL); err != nil {
		return err
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(o.Port)))
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	h, err := NewProxyHandler(o.Cred, o.ClusterID, port, o.Transport, o.Out)
	if err != nil {
		ln.Close()
		return err
	}
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 15 * time.Second}
	addr := ln.Addr().String()
	fmt.Fprintf(o.Out, "Connected to %s through the cloud.\nOpen http://localhost:%d/  (Ctrl-C to stop)\n", o.ClusterID, port)
	if o.Ready != nil {
		o.Ready(addr)
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewProxyHandler returns the handler that forwards a browser's requests for
// the Orchestrator UI/API to {cloud}/clusters/{id}/remote/..., authenticated
// with the device key. localPort is the port it is served on (for the Host and
// Origin checks).
func NewProxyHandler(c Credentials, clusterID string, localPort int, rt http.RoundTripper, logw io.Writer) (http.Handler, error) {
	cloud, err := CheckCloudURL(c.CloudURL)
	if err != nil {
		return nil, err
	}
	prefix := strings.TrimRight(cloud.Path, "/") + "/clusters/" + url.PathEscape(clusterID) + "/remote"
	ownHosts := map[string]bool{
		fmt.Sprintf("127.0.0.1:%d", localPort): true,
		fmt.Sprintf("localhost:%d", localPort): true,
		fmt.Sprintf("[::1]:%d", localPort):     true,
	}
	if rt == nil {
		rt = http.DefaultTransport
	}

	rp := &httputil.ReverseProxy{
		Transport:     rt,
		FlushInterval: -1, // stream log tails and WebSockets as they arrive
		Rewrite: func(r *httputil.ProxyRequest) {
			out := r.Out
			out.URL.Scheme = cloud.Scheme
			out.URL.Host = cloud.Host
			out.URL.Path = prefix + r.In.URL.Path
			out.URL.RawPath = ""
			out.Host = cloud.Host
			// Nothing the browser sent may act as a credential or claim an
			// identity upstream: the only credential is the device key.
			for _, h := range []string{
				"Authorization", "Cookie", "Origin", "Referer",
				"X-Underleaf-Assertion", "X-Subject-Id",
				"X-Forwarded-For", "X-Forwarded-Host", "X-Forwarded-Proto", "Forwarded", "X-Real-Ip",
			} {
				out.Header.Del(h)
			}
			out.Header.Set("Authorization", "Bearer "+c.DeviceKey)
		},
		ModifyResponse: func(resp *http.Response) error {
			if resp.StatusCode == http.StatusUnauthorized && logw != nil {
				fmt.Fprintln(logw, "the cloud rejected the device key; run `ufctl login` again")
			}
			// The cloud's cookies belong to the cloud, not to localhost.
			resp.Header.Del("Set-Cookie")
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			if logw != nil {
				fmt.Fprintf(logw, "proxy error: %v\n", err)
			}
			http.Error(w, "bad gateway: could not reach the cloud", http.StatusBadGateway)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// DNS rebinding: only our own loopback names.
		if !ownHosts[strings.ToLower(r.Host)] {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		// CSRF / cross-site access: a page on another origin must not be able
		// to drive the owner's cluster through this proxy.
		if o := r.Header.Get("Origin"); o != "" && !ownHosts[strings.ToLower(strings.TrimPrefix(o, "http://"))] {
			http.Error(w, "origin not allowed", http.StatusForbidden)
			return
		}
		switch r.Header.Get("Sec-Fetch-Site") {
		case "", "same-origin", "none":
		default:
			http.Error(w, "cross-site request not allowed", http.StatusForbidden)
			return
		}
		// Keep every request under .../remote: no dot segments, no encoded
		// slashes or backslashes.
		raw := strings.ToLower(r.URL.EscapedPath())
		bad := strings.Contains(raw, "%2f") || strings.Contains(raw, "%2e") || strings.Contains(raw, "%5c")
		for _, seg := range strings.Split(r.URL.Path, "/") {
			if seg == ".." || seg == "." {
				bad = true
			}
		}
		if bad {
			http.Error(w, "bad path", http.StatusBadRequest)
			return
		}
		if r.URL.Path == "" {
			r.URL.Path = "/"
		}
		rp.ServeHTTP(w, r)
	}), nil
}
