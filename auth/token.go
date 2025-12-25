package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const googleTokenEndpoint = "https://oauth2.googleapis.com/token"

type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`

	// Error fields (device flow)
	Error     string `json:"error"`
	ErrorDesc string `json:"error_description"`
	ErrorURI  string `json:"error_uri"`
}

// Poll Google until user approves (or timeout / denial)
func pollForToken(clientID, deviceCode string, intervalSec, expiresInSec int) (*TokenResponse, error) {

	fmt.Println(clientID, deviceCode, intervalSec, expiresInSec)

	interval := time.Duration(intervalSec) * time.Second
	fmt.Println(interval)

	if interval < 5*time.Second {
		interval = 5 * time.Second
	}

	deadline := time.Now().Add(time.Duration(expiresInSec) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(interval)

		form := url.Values{}
		form.Set("client_id", clientID)
		form.Set("device_code", deviceCode)
		form.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		req, _ := http.NewRequest("POST", googleTokenEndpoint, bytes.NewBufferString(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		res, err := http.DefaultClient.Do(req)
		fmt.Println("err : ", err)

		if err != nil {
			return nil, err
		}

		body, _ := io.ReadAll(res.Body)
		fmt.Println("body : ", string(body))
		res.Body.Close()

		var tr TokenResponse
		_ = json.Unmarshal(body, &tr)

		fmt.Println(tr)

		// Success case
		if tr.AccessToken != "" && tr.Error == "" {
			return &tr, nil
		}

		// Expected device-flow states
		switch tr.Error {
		case "authorization_pending":
			// user hasn't approved yet; keep polling
			continue
		case "slow_down":
			// back off a bit
			interval += 5 * time.Second
			continue
		case "access_denied":
			return nil, fmt.Errorf("login cancelled by user")
		case "expired_token":
			return nil, fmt.Errorf("device code expired; run login again")
		case "invalid_client":
			return nil, fmt.Errorf("invalid client_id (check Google OAuth client type)")
		case "":
			// Sometimes you may get non-JSON errors; surface raw body
			return nil, fmt.Errorf("unexpected token response: %s", string(body))
		default:
			return nil, fmt.Errorf("token error: %s (%s)", tr.Error, tr.ErrorDesc)
		}
	}

	return nil, fmt.Errorf("login timed out; run login again")
}
