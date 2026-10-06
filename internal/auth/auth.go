package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	keycloak = "https://user.mobilitaetsverbuende.at"
	realm    = "dbp-public"
)

type DBPAuth struct {
	Username string
	Password string

	token  string
	expiry time.Time
	mu     sync.Mutex
	client *http.Client
}

func NewAuth(username, password string) *DBPAuth {
	return &DBPAuth{
		Username: username,
		Password: password,
		client:   &http.Client{Timeout: 30 * time.Second},
	}
}

func (a *DBPAuth) GetToken() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.token != "" && time.Now().Before(a.expiry) {
		return a.token, nil
	}

	form := url.Values{}
	form.Set("client_id", "dbp-public-ui")
	form.Set("username", a.Username)
	form.Set("password", a.Password)
	form.Set("grant_type", "password")
	form.Set("scope", "openid")

	resp, err := a.client.Post(
		keycloak+"/auth/realms/"+realm+"/protocol/openid-connect/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("token request returned status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("token decode failed: %w", err)
	}

	a.token = data.AccessToken
	a.expiry = time.Now().Add(time.Duration(data.ExpiresIn-30) * time.Second)

	return a.token, nil
}

// InvalidateToken clears the cached token, forcing a refresh on the next call.
func (a *DBPAuth) InvalidateToken() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.token = ""
	a.expiry = time.Time{}
}

func (a *DBPAuth) Header() (http.Header, error) {
	token, err := a.GetToken()
	if err != nil {
		return nil, err
	}

	h := http.Header{}
	h.Set("Authorization", "Bearer "+token)
	return h, nil
}
