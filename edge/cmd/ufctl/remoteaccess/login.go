package remoteaccess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// DefaultCloudURL is the production cloud API base URL.
const DefaultCloudURL = "https://underleafapp.com/api/v2/cloud"

const deviceGrant = "urn:ietf:params:oauth:grant-type:device_code"

// CheckCloudURL refuses a cloud URL that would carry the device key in the
// clear: https is required, except for a loopback host (local development).
func CheckCloudURL(raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return nil, fmt.Errorf("invalid cloud URL %q", raw)
	}
	if u.Scheme == "https" {
		return u, nil
	}
	if u.Scheme == "http" {
		switch u.Hostname() {
		case "localhost", "127.0.0.1", "::1":
			return u, nil
		}
	}
	return nil, fmt.Errorf("cloud URL %q must use https (plain http is only allowed for localhost)", raw)
}

// LoginOptions configures Login.
type LoginOptions struct {
	CloudURL   string
	DeviceName string
	Out        io.Writer
	HTTP       *http.Client
	// Sleep waits between polls; nil means a context-aware time.Sleep.
	Sleep func(ctx context.Context, d time.Duration) error
	Now   func() time.Time
}

type deviceAuth struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

type tokenResp struct {
	DeviceKeyID string `json:"device_key_id"`
	DeviceKey   string `json:"device_key"`
	Error       string `json:"error"`
}

// Login runs the device flow and stores the device key (0600).
func Login(ctx context.Context, o LoginOptions) error {
	if o.CloudURL == "" {
		o.CloudURL = DefaultCloudURL
	}
	base, err := CheckCloudURL(o.CloudURL)
	if err != nil {
		return err
	}
	if o.HTTP == nil {
		o.HTTP = &http.Client{Timeout: 20 * time.Second}
	}
	if o.Out == nil {
		o.Out = io.Discard
	}
	if o.Sleep == nil {
		o.Sleep = sleepCtx
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.DeviceName == "" {
		o.DeviceName, _ = os.Hostname()
	}
	baseStr := strings.TrimRight(base.String(), "/")

	var da deviceAuth
	if _, err := postJSON(ctx, o.HTTP, baseStr+"/ufctl/device", map[string]string{"device_name": o.DeviceName}, &da); err != nil {
		return fmt.Errorf("start login: %w", err)
	}
	if da.DeviceCode == "" || da.UserCode == "" {
		return errors.New("start login: the cloud returned an incomplete device authorization")
	}
	uri := da.VerificationURIComplete
	if uri == "" {
		uri = da.VerificationURI
	}
	fmt.Fprintf(o.Out, "Open %s\nand confirm the code %s\n", uri, da.UserCode)

	interval := time.Duration(da.Interval) * time.Second
	if interval <= 0 {
		interval = 5 * time.Second
	}
	deadline := o.Now().Add(time.Duration(da.ExpiresIn) * time.Second)
	if da.ExpiresIn <= 0 {
		deadline = o.Now().Add(15 * time.Minute)
	}
	for {
		if err := o.Sleep(ctx, interval); err != nil {
			return err
		}
		if o.Now().After(deadline) {
			return errors.New("login expired before it was approved")
		}
		var tr tokenResp
		status, err := postJSON(ctx, o.HTTP, baseStr+"/ufctl/token", map[string]string{"grant_type": deviceGrant, "device_code": da.DeviceCode}, &tr)
		if err != nil && status != http.StatusBadRequest {
			return fmt.Errorf("poll login: %w", err)
		}
		switch tr.Error {
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "expired_token":
			return errors.New("login expired before it was approved")
		case "access_denied":
			return errors.New("login was denied")
		case "":
		default:
			return fmt.Errorf("login failed: %s", tr.Error)
		}
		if tr.DeviceKey == "" {
			return errors.New("poll login: the cloud returned no device key")
		}
		if err := Save(Credentials{CloudURL: baseStr, DeviceKeyID: tr.DeviceKeyID, DeviceKey: tr.DeviceKey, CreatedAt: o.Now().UTC()}); err != nil {
			return fmt.Errorf("store device key: %w", err)
		}
		fmt.Fprintln(o.Out, "Logged in.")
		return nil
	}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// postJSON posts body as JSON and decodes a JSON response (success or error
// body) into out. It returns the HTTP status; a non-2xx status is an error but
// out is still filled when the body parses, so callers can read an RFC 8628
// error code.
func postJSON(ctx context.Context, hc *http.Client, target string, body, out any) (int, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(raw))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := hc.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	_ = json.Unmarshal(b, out)
	if resp.StatusCode/100 != 2 {
		return resp.StatusCode, fmt.Errorf("status %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}
