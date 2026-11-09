//go:build integration

package auth_test

import (
	"encoding/json"
	"fmt"
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

func TestHTTPUserManagementRoundTrip(t *testing.T) {
	service, _ := newService(t)
	admin := setup(t, service)
	server := httptest.NewServer(httpapi.NewHandler(httpapi.Config{
		Auth:   service,
		Users:  service,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}))
	defer server.Close()
	newClient := func() *http.Client {
		jar, err := cookiejar.New(nil)
		if err != nil {
			t.Fatal(err)
		}
		return &http.Client{
			Jar:     jar,
			Timeout: 5 * time.Second,
		}
	}
	adminClient, memberClient, anonymous := newClient(), newClient(), newClient()
	request := func(client *http.Client, method, path, body string, status int, code string) []byte {
		t.Helper()
		req, err := http.NewRequestWithContext(t.Context(), method, server.URL+path, strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", server.URL)
		res, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		data, err := io.ReadAll(res.Body)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != status {
			t.Fatalf("%s %s = %d %s, want %d", method, path, res.StatusCode, data, status)
		}
		if code != "" {
			var failure httpapi.ErrorResponse
			if err := json.Unmarshal(data, &failure); err != nil {
				t.Fatalf("decode failure response: %v; body=%s", err, data)
			}
			if failure.Code != code {
				t.Fatalf("failure code = %q, want %q", failure.Code, code)
			}
			if failure.RequestID == "" {
				t.Fatal("failure response is missing request_id")
			}
		}
		if strings.Contains(string(data), credentials.Password) || strings.Contains(string(data), "password_hash") || strings.Contains(string(data), "token_hash") {
			t.Fatalf("sensitive data in response: %s", data)
		}
		return data
	}
	adminBody := fmt.Sprintf(`{"username":%q,"password":%q}`, admin.Username, credentials.Password)
	request(adminClient, http.MethodPost, "/api/v1/auth/login", adminBody, http.StatusOK, "")
	request(adminClient, http.MethodPatch, fmt.Sprintf("/api/v1/users/%d/role", admin.ID), `{"role":"member"}`, http.StatusConflict, "last_admin")
	memberBody := fmt.Sprintf(`{"username":"alice","password":%q}`, credentials.Password)
	data := request(adminClient, http.MethodPost, "/api/v1/users", memberBody, http.StatusCreated, "")
	var member auth.User
	if err := json.Unmarshal(data, &member); err != nil {
		t.Fatalf("decode created user: %v; body=%s", err, data)
	}
	if member.Role != "member" {
		t.Fatalf("created user role = %q, want member", member.Role)
	}
	if member.Status != "active" {
		t.Fatalf("created user status = %q, want active", member.Status)
	}
	request(adminClient, http.MethodPost, "/api/v1/users", memberBody, http.StatusConflict, "username_taken")
	request(memberClient, http.MethodPost, "/api/v1/auth/login", memberBody, http.StatusOK, "")
	rolePath := fmt.Sprintf("/api/v1/users/%d/role", member.ID)
	statusPath := fmt.Sprintf("/api/v1/users/%d/status", member.ID)
	for _, tc := range []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodGet, "/api/v1/users", ""},
		{http.MethodPost, "/api/v1/users", memberBody},
		{http.MethodPatch, rolePath, `{"role":"admin"}`},
		{http.MethodPatch, statusPath, `{"status":"disabled"}`},
	} {
		request(anonymous, tc.method, tc.path, tc.body, http.StatusUnauthorized, "unauthenticated")
		request(memberClient, tc.method, tc.path, tc.body, http.StatusForbidden, "forbidden")
	}
	request(adminClient, http.MethodGet, "/api/v1/users?limit=0", "", http.StatusBadRequest, "validation_failed")
	request(adminClient, http.MethodPatch, rolePath, `{}`, http.StatusBadRequest, "validation_failed")
	request(adminClient, http.MethodPatch, "/api/v1/users/999999/status", `{"status":"disabled"}`, http.StatusNotFound, "not_found")
	request(adminClient, http.MethodPatch, rolePath, `{"role":"admin"}`, http.StatusOK, "")
	request(memberClient, http.MethodGet, "/api/v1/users", "", http.StatusOK, "")
	request(adminClient, http.MethodPatch, rolePath, `{"role":"member"}`, http.StatusOK, "")
	request(memberClient, http.MethodGet, "/api/v1/users", "", http.StatusForbidden, "forbidden")
	request(adminClient, http.MethodPatch, statusPath, `{"status":"disabled"}`, http.StatusOK, "")
	request(memberClient, http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized, "unauthenticated")
	request(adminClient, http.MethodPatch, statusPath, `{"status":"active"}`, http.StatusOK, "")
	request(memberClient, http.MethodGet, "/api/v1/auth/me", "", http.StatusUnauthorized, "unauthenticated")
	request(memberClient, http.MethodPost, "/api/v1/auth/login", memberBody, http.StatusOK, "")
	request(memberClient, http.MethodGet, "/api/v1/auth/me", "", http.StatusOK, "")
}
