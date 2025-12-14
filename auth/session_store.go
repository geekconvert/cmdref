package auth

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Session struct {
	Token string `json:"token"`
	Email string `json:"email"`
	Name  string `json:"name"`
	CreatedAt string `json:"createdAt"`
}

func sessionPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cmdref", "session.json"), nil
}

func ensureCmdrefDir() error{
	p, err := sessionPath()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Dir(p), 0755)
}

func SaveSession(s Session) error {
	if s.Token == "" {
		return fmt.Errorf("empty token")
	}

	if err := ensureCmdrefDir(); err != nil {
		return err
	}

	p, err := sessionPath()
	if err != nil {
		return err
	}
	tmp := p + ".tmp"

	if s.CreatedAt == "" {
		s.CreatedAt = time.Now().Format(time.RFC3339)
	}

	b, err := json.MarshalIndent(s, "", " ")
	if err != nil {
		return err
	}

	// write tmp with strict permissions
	if err := os.WriteFile(tmp, b, 0600); err != nil {
		return err
	}

	// atomic replace
	return os.Rename(tmp, p)
}

func LoadSession() (*Session, error){
	p, err := sessionPath()
	if err != nil {
		return nil, err
	}

	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // not logged in
		}
		return nil, err
	}

	var s Session
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if s.Token == "" {
		return nil, nil
	}
	return &s, nil
}

func ClearSession() error{
	p, err := sessionPath()
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}