package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

type GoogleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token"`

	Error            string `json:"error"`
	ErrorDescription string `json:"error_description"`
}

func LoginWithGooglePKCE(clientID string) (*CmdrefAuthResponse, error) {
	verifier, err := randomBase64URL(64) // 43..128 chars (64 is fine)
	if err != nil {
		return nil, err
	}
	challenge := codeChallengeS256(verifier)
	state, err := randomBase64URL(32)

	if err != nil {
		return nil, err
	}

	// 1) Start local callback server on 127.0.0.1:<random_port>
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	ln, err := net.Listen("tcp", "127.0.0.1:0") // random free port
	if err != nil {
		return nil, err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		fmt.Println("query:", q)
		if q.Get("state") != state {
			http.Error(w, "Invalid state", http.StatusBadRequest)
			return
		}
		
		if errStr := q.Get("error"); errStr != "" {
			desc := q.Get("error_description")
			http.Error(w, "Login error: "+errStr+" "+desc, http.StatusBadRequest)
			errCh <- fmt.Errorf("oauth error: %s (%s)", errStr, desc)
			return
		}
		code := q.Get("code")
		fmt.Println("code:", code)
		if code == "" {
			http.Error(w, "Missing code", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, "<h3>Login successful.</h3><p>You can close this window and return to the terminal.</p>")
		codeCh <- code
	})

	go func() {
		// server will stop after we get code
		if err := srv.Serve(ln); err != nil && !strings.Contains(err.Error(), "Server closed") {
			errCh <- err
		}
	}()

	// 2) Build auth URL and open browser
	authURL := buildGoogleAuthURL(clientID, redirectURI, state, challenge)
	fmt.Println("Opening browser for Google login...")
	fmt.Println("authURL: ",authURL) // fallback in case browser open fails

	_ = openBrowser(authURL)

	// 3) Wait for callback or timeout
	var code string
	select {
	case code = <-codeCh:
		// got it
	case e := <-errCh:
		_ = srv.Shutdown(context.Background())
		return nil, e
	case <-time.After(3 * time.Minute):
		_ = srv.Shutdown(context.Background())
		return nil, fmt.Errorf("login timed out")
	}

	// Stop local server
	_ = srv.Shutdown(context.Background())

	// 4) Exchange code for tokens
	//return exchangeCodeForTokens(clientID, code, verifier, redirectURI)
	return exchangeViaBackend(code, verifier, redirectURI)
}

func buildGoogleAuthURL(clientID, redirectURI, state, challenge string) string {
	u, _ := url.Parse("https://accounts.google.com/o/oauth2/v2/auth")
	q := u.Query()
	q.Set("client_id", clientID)
	q.Set("redirect_uri", redirectURI)
	q.Set("response_type", "code")
	q.Set("scope", "openid email profile")
	q.Set("state", state)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("access_type", "offline") // may provide refresh_token (Google sometimes only gives once)
	q.Set("prompt", "consent")      // ensures refresh_token more reliably
	u.RawQuery = q.Encode()
	return u.String()
}

func exchangeCodeForTokens(clientID, code, verifier, redirectURI string) (*GoogleTokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("code", code)
	form.Set("code_verifier", verifier)
	form.Set("redirect_uri", redirectURI)
	form.Set("grant_type", "authorization_code")
	// form.Set("client_secret", "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")

	fmt.Printf("headers: " + verifier + " " + redirectURI + " " + clientID + "\n")

	req, _ := http.NewRequest("POST", "https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	
	fmt.Println("req:", req)
	
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	fmt.Println("response body:", string(body))

	var tr GoogleTokenResponse
	_ = json.Unmarshal(body, &tr)

	if res.StatusCode >= 300 || tr.Error != "" {
		if tr.Error != "" {
			return nil, fmt.Errorf("token error: %s (%s)", tr.Error, tr.ErrorDescription)
		}
		return nil, fmt.Errorf("token http error: %s", string(body))
	}

	return &tr, nil
}

func codeChallengeS256(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func randomBase64URL(nBytes int) (string, error) {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	case "linux":
		return exec.Command("xdg-open", url).Start()
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	default:
		return fmt.Errorf("unsupported OS: %s", runtime.GOOS)
	}
}
