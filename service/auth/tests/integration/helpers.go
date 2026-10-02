//go:build integration

package integration

// helpers.go — HTTP helpers and identity helpers. Auth owns identity, so unlike
// the other services it mints real signed tokens rather than synthesizing
// gateway headers.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// ── HTTP helpers ──

func doJSON(
	method, url string,
	body interface{},
	headers map[string]string,
) (*http.Response, []byte, error) {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = strings.NewReader(string(b))
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	return resp, respBody, err
}

func mustJSON(data []byte, target interface{}) {
	if err := json.Unmarshal(data, target); err != nil {
		panic(fmt.Sprintf("json: %s\nraw: %s", err, string(data)))
	}
}

// ── Response shapes ──

type authLoginData struct {
	Data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	} `json:"data"`
}

type authMeData struct {
	Data struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
			Role  string `json:"role"`
		} `json:"user"`
	} `json:"data"`
}

type authRegisterData struct {
	Data struct {
		User struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Role  string `json:"role"`
		} `json:"user"`
	} `json:"data"`
}

// ── Fixtures ──

// credentials is a registered user's tokens and identity.
type credentials struct {
	AccessToken  string
	RefreshToken string
	UserID       string
	Email        string
	Role         string
}

// registerAndLogin creates a customer or eo through the service API and logs it
// in, so the tokens under test are the ones the endpoints actually issue.
func (env *TestEnv) registerAndLogin(role string) credentials {
	email := fmt.Sprintf("%s_%s@test.com", role, uuid.NewString()[:8])
	pass := "Test123!"

	switch role {
	case "customer":
		resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/register", map[string]interface{}{
			"email": email, "password": pass, "name": role + " Test", "role": role,
		}, nil)
		mustOK(resp, body, err, "register customer")

		var rd authRegisterData
		mustJSON(body, &rd)
		_ = rd

	case "eo":
		resp, body, err := doJSON(http.MethodPost, env.authURL+"/api/auth/register/organizer", map[string]interface{}{
			"email":          email,
			"password":       pass,
			"name":           "EO Organizer",
			"organizer_name": "Org",
			"description":    "desc",
			"profile_link":   "https://org.test",
			"contact_email":  email,
		}, nil)
		mustOK(resp, body, err, "register eo")

	default:
		panic("unknown role: " + role)
	}

	pair, err := env.svc.Login(context.Background(), email, pass)
	if err != nil {
		panic(fmt.Sprintf("login %s: %v", role, err))
	}

	return credentials{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		Email:        email,
		Role:         role,
	}
}

// loginAdmin logs in the seeded administrator.
func (env *TestEnv) loginAdmin() credentials {
	pair, err := env.svc.Login(context.Background(), adminEmail, adminPassword)
	if err != nil {
		panic(fmt.Sprintf("admin login: %v", err))
	}
	claims, err := env.tokenSvc.VerifyAccessToken(pair.AccessToken)
	if err != nil {
		panic(fmt.Sprintf("verify admin token: %v", err))
	}
	return credentials{
		AccessToken:  pair.AccessToken,
		RefreshToken: pair.RefreshToken,
		UserID:       claims.UserID,
		Email:        claims.Email,
		Role:         "admin",
	}
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

func mustOK(resp *http.Response, body []byte, err error, what string) {
	if err != nil {
		panic(fmt.Sprintf("%s: %v", what, err))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		panic(fmt.Sprintf("%s: status %d body %s", what, resp.StatusCode, body))
	}
}
