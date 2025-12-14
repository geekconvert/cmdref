package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type CmdrefAuthResponse struct {
	Token   string `json:"token"`   // your cmdref JWT
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

func exchangeViaBackend(code, verifier, redirectURI string) (*CmdrefAuthResponse, error) {
	apiBase := os.Getenv("CMDREF_API_BASE")
	if apiBase == "" {
		apiBase = "http://127.0.0.1:8080"
	}

	payload := map[string]string{
		"code":          code,
		"code_verifier": verifier,
		"redirect_uri":  redirectURI,
	}
	b, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST", apiBase+"/v1/auth/google/exchange", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	body, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("backend exchange failed: %s", string(body))
	}

	var out CmdrefAuthResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.Token == "" {
		return nil, fmt.Errorf("backend returned empty token")
	}

	fmt.Println("out.Token : " , out.Token)
	
	return &out, nil
}
