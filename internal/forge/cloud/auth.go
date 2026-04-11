package cloud

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ---------------------------------------------------------------------------
// Token storage
// ---------------------------------------------------------------------------

// Credentials holds the authentication state for Forge Cloud.
type Credentials struct {
	AccessToken  string    `json:"accessToken"`
	RefreshToken string    `json:"refreshToken,omitempty"`
	ExpiresAt    time.Time `json:"expiresAt,omitempty"`
	UserID       string    `json:"userId"`
	Email        string    `json:"email"`
	TeamID       string    `json:"teamId,omitempty"`
	CloudURL     string    `json:"cloudUrl"`
}

// IsExpired returns true if the access token has expired.
func (c Credentials) IsExpired() bool {
	if c.ExpiresAt.IsZero() {
		return false // no expiry set, assume valid
	}
	return time.Now().After(c.ExpiresAt)
}

// NeedsRefresh returns true if the token should be refreshed soon.
func (c Credentials) NeedsRefresh() bool {
	if c.ExpiresAt.IsZero() {
		return false
	}
	// Refresh if less than 5 minutes remaining
	return time.Now().Add(5 * time.Minute).After(c.ExpiresAt)
}

// ---------------------------------------------------------------------------
// Credential file management
// ---------------------------------------------------------------------------

// credentialsPath returns the path to the credentials file.
func credentialsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".forge", "credentials.json"), nil
}

// LoadCredentials reads stored credentials from disk.
func LoadCredentials() (Credentials, error) {
	path, err := credentialsPath()
	if err != nil {
		return Credentials{}, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Credentials{}, fmt.Errorf("not logged in (run: forge login)")
		}
		return Credentials{}, err
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return Credentials{}, fmt.Errorf("corrupted credentials file: %w", err)
	}

	return creds, nil
}

// SaveCredentials writes credentials to disk.
func SaveCredentials(creds Credentials) error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o600)
}

// ClearCredentials removes stored credentials (logout).
func ClearCredentials() error {
	path, err := credentialsPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// ---------------------------------------------------------------------------
// Login flow
// ---------------------------------------------------------------------------

// LoginResult is returned after successful authentication.
type LoginResult struct {
	Credentials Credentials `json:"credentials"`
	Message     string      `json:"message"`
}

// DeviceCodeResponse is returned when initiating device code flow.
type DeviceCodeResponse struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}

// LoginWithDeviceCode initiates the device code OAuth flow.
// This is the recommended flow for CLI tools - user visits a URL and enters a code.
func (c *Client) LoginWithDeviceCode() (*DeviceCodeResponse, error) {
	var resp DeviceCodeResponse
	err := c.post("/auth/device/code", nil, &resp)
	if err != nil {
		return nil, fmt.Errorf("initiating device code flow: %w", err)
	}
	return &resp, nil
}

// PollForToken polls the auth server until the user completes authentication.
func (c *Client) PollForToken(deviceCode string, interval int) (*Credentials, error) {
	type tokenRequest struct {
		DeviceCode string `json:"device_code"`
		GrantType  string `json:"grant_type"`
	}

	type tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		UserID       string `json:"user_id"`
		Email        string `json:"email"`
		TeamID       string `json:"team_id"`
		Error        string `json:"error"`
	}

	req := tokenRequest{
		DeviceCode: deviceCode,
		GrantType:  "urn:ietf:params:oauth:grant-type:device_code",
	}

	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	timeout := time.After(5 * time.Minute)

	for {
		select {
		case <-timeout:
			return nil, fmt.Errorf("login timed out")
		case <-ticker.C:
			var resp tokenResponse
			err := c.post("/auth/device/token", req, &resp)
			if err != nil {
				// Network error, keep trying
				continue
			}

			switch resp.Error {
			case "":
				// Success
				creds := &Credentials{
					AccessToken:  resp.AccessToken,
					RefreshToken: resp.RefreshToken,
					ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
					UserID:       resp.UserID,
					Email:        resp.Email,
					TeamID:       resp.TeamID,
					CloudURL:     c.baseURL,
				}
				return creds, nil
			case "authorization_pending":
				// User hasn't completed auth yet, keep polling
				continue
			case "slow_down":
				// Back off
				time.Sleep(5 * time.Second)
				continue
			case "expired_token":
				return nil, fmt.Errorf("device code expired, please try again")
			case "access_denied":
				return nil, fmt.Errorf("access denied by user")
			default:
				return nil, fmt.Errorf("auth error: %s", resp.Error)
			}
		}
	}
}

// RefreshAccessToken uses the refresh token to get a new access token.
func (c *Client) RefreshAccessToken(refreshToken string) (*Credentials, error) {
	type refreshRequest struct {
		RefreshToken string `json:"refresh_token"`
		GrantType    string `json:"grant_type"`
	}

	type tokenResponse struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}

	req := refreshRequest{
		RefreshToken: refreshToken,
		GrantType:    "refresh_token",
	}

	var resp tokenResponse
	if err := c.post("/auth/token/refresh", req, &resp); err != nil {
		return nil, fmt.Errorf("refreshing token: %w", err)
	}

	// Load existing creds to preserve user info
	existing, _ := LoadCredentials()

	creds := &Credentials{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second),
		UserID:       existing.UserID,
		Email:        existing.Email,
		TeamID:       existing.TeamID,
		CloudURL:     c.baseURL,
	}

	return creds, nil
}
