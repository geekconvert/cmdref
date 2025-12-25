package api

import (
	"bytes"
	"commandref/auth"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
)

type Client struct {
	BaseURL string
}

func New() *Client {
	base := os.Getenv("COMMANDREF_API_BASE")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	return &Client{BaseURL: base}
}

func (c *Client) DoJSON(method, path string, in any, out any) error {
	sess, err := auth.LoadSession()
	if err != nil {
		return err
	}
	if sess == nil || sess.Token == "" {
		return fmt.Errorf("not logged in. run: commandref login")
	}

	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}

	req, _ := http.NewRequest(method, c.BaseURL+path, body)
	req.Header.Set("Authorization", "Bearer "+sess.Token)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	respBody, _ := io.ReadAll(res.Body)
	if res.StatusCode >= 300 {
		return fmt.Errorf("%s", string(respBody))
	}
	if out != nil {
		return json.Unmarshal(respBody, out)
	}
	return nil
}
