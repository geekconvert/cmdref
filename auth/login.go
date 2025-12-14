package auth

import (
	"fmt"
	"os"
)

const defaultGoogleClientID = "YOUR_DESKTOP_CLIENT_ID.apps.googleusercontent.com"

func Login() error {
	clientID := os.Getenv("CMDREF_GOOGLE_CLIENT_ID")

	if clientID == "" {
		clientID = defaultGoogleClientID
	}

	resp, err := LoginWithGooglePKCE(clientID)
	if err != nil { return err }

	if err := SaveSession(Session{
		Token: resp.Token,
		Email: resp.Email,
		Name:  resp.Name,
	}); err != nil {
		return err
	}

	fmt.Println("Approved ✅")
	fmt.Println("Logged in as:", resp.Email)
	fmt.Println("Got cmdref token:", resp.Token != "")

	// NEXT: send tokens.IDToken to your backend, get your JWT, store it.
	return nil
}
