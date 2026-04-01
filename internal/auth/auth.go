package auth

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	KEYCLOAK = "https://user.mobilitaetsverbuende.at"
	REALM    = "dbp-public"
)

type DBPAuth struct {
	Username string
	Password string

	Token  string
	Expiry time.Time
	mu     sync.Mutex
}

func NewAuth(username, password string) *DBPAuth {
	return &DBPAuth{Username: username, Password: password}
}

func (a *DBPAuth) GetToken() (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.Token != "" && time.Now().Before(a.Expiry) {
		return a.Token, nil
	}

	form := url.Values{}
	form.Set("client_id", "dbp-public-ui")
	form.Set("username", a.Username)
	form.Set("password", a.Password)
	form.Set("grant_type", "password")
	form.Set("scope", "openid")

	resp, err := http.Post(
		KEYCLOAK+"/auth/realms/"+REALM+"/protocol/openid-connect/token",
		"application/x-www-form-urlencoded",
		strings.NewReader(form.Encode()),
	)

	if err != nil {
		log.Fatalf("GetToken() failed due to %v", err)
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		log.Fatalf("GetToken() failed due to %v", err)
		return "", err
	}

	a.Token = data.AccessToken
	a.Expiry = time.Now().Add(time.Duration(data.ExpiresIn-30) * time.Second)

	return a.Token, nil
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
