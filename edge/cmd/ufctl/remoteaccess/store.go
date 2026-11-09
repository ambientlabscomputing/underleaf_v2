package remoteaccess

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EnvHome overrides the directory that holds the device key.
const EnvHome = "UNDERLEAF_UFCTL_HOME"

// Credentials is what `ufctl login` stores.
type Credentials struct {
	// CloudURL is the cloud API base URL the key was issued by. The key is only
	// ever sent there.
	CloudURL    string    `json:"cloud_url"`
	DeviceKeyID string    `json:"device_key_id"`
	DeviceKey   string    `json:"device_key"`
	CreatedAt   time.Time `json:"created_at"`
}

// ErrNotLoggedIn means there is no stored device key.
var ErrNotLoggedIn = errors.New("not logged in: run `ufctl login`")

// Dir is where the device key lives: $UNDERLEAF_UFCTL_HOME, or
// <user config dir>/underleaf/ufctl.
func Dir() (string, error) {
	if d := os.Getenv(EnvHome); d != "" {
		return d, nil
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "underleaf", "ufctl"), nil
}

func credPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "device.json"), nil
}

// Save writes the credentials owner-only: directory 0700, file 0600, replaced
// atomically so a crash never leaves a half-written key.
func Save(c Credentials) error {
	p, err := credPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	if err := os.Chmod(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".device-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), p)
}

// Load reads the stored credentials. It refuses a key file that other users can
// read, rather than silently using a leaked secret.
func Load() (*Credentials, error) {
	p, err := credPath()
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotLoggedIn
	}
	if err != nil {
		return nil, err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is accessible to other users (mode %o); fix with chmod 600 or run `ufctl login` again", p, st.Mode().Perm())
	}
	raw, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var c Credentials
	if err := json.Unmarshal(raw, &c); err != nil || c.DeviceKey == "" || c.CloudURL == "" {
		return nil, fmt.Errorf("%s is corrupt; run `ufctl login` again", p)
	}
	return &c, nil
}

// Delete removes the stored credentials (logout). Missing is not an error.
func Delete() error {
	p, err := credPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}
