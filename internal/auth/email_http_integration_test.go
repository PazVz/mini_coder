//go:build integration

package auth_test

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PazVz/mini_coder/internal/auth"
	"github.com/PazVz/mini_coder/internal/httpapi"
)

func TestHTTPRegistrationAndEmailRoundTrip(t *testing.T) {
	accounts, service, pool, sender := newEmailWorkflow(t)
	server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Auth:   accounts,
		Emails: service,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	defer server.Close()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{
		Jar:     jar,
		Timeout: 5 * time.Second,
	}
	request := func(method, path string, input any, want int) []byte {
		t.Helper()
		body, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(string(body)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s = %d %s, want %d", method, path, response.StatusCode, data, want)
		}
		if strings.Contains(string(data), credentials.Password) || strings.Contains(string(data), "token_hash") {
			t.Fatal("email API exposed credentials")
		}
		if path == "/api/v1/auth/register" && len(response.Cookies()) != 0 {
			t.Fatal("registration automatically logged in the new user")
		}
		return data
	}
	request(http.MethodPost, "/api/v1/auth/register/request", map[string]string{"email": "http@example.com"}, http.StatusAccepted)
	token := emailToken(t, sender.last(t))
	request(http.MethodGet, "/auth/register", nil, http.StatusOK)
	var count int
	if err := pool.QueryRow(t.Context(), "SELECT count(*) FROM email_actions").Scan(&count); err != nil || count != 1 {
		t.Fatalf("GET consumed email proof: %d error=%v", count, err)
	}
	data := request(http.MethodPost, "/api/v1/auth/register", auth.RegistrationInput{
		Token:    token,
		Username: "http-user",
		Password: credentials.Password,
	}, http.StatusCreated)
	var user auth.User
	if err := json.Unmarshal(data, &user); err != nil || !user.EmailVerified || user.Email != "http@example.com" {
		t.Fatalf("HTTP registered user = %+v error=%v", user, err)
	}
	request(http.MethodGet, "/api/v1/auth/me", nil, http.StatusUnauthorized)
	request(http.MethodPost, "/api/v1/auth/login", auth.Credentials{
		Username: user.Username,
		Password: credentials.Password,
	}, http.StatusOK)
	request(http.MethodPost, "/api/v1/auth/email/change", auth.EmailChange{
		Email:           "http-new@example.com",
		CurrentPassword: credentials.Password,
	}, http.StatusAccepted)
	token = emailToken(t, sender.last(t))
	request(http.MethodGet, "/auth/email/confirm", nil, http.StatusOK)
	data = request(http.MethodPost, "/api/v1/auth/email/confirm", map[string]string{"token": token}, http.StatusOK)
	if err := json.Unmarshal(data, &user); err != nil || !user.EmailVerified || user.Email != "http-new@example.com" {
		t.Fatalf("HTTP updated user = %+v error=%v", user, err)
	}
	request(http.MethodPost, "/api/v1/auth/email/confirm", map[string]string{"token": token}, http.StatusBadRequest)
	request(http.MethodGet, "/api/v1/auth/me", nil, http.StatusOK)
}
