package httpapi

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

func TestAttemptWindows(t *testing.T) {
	now := time.Now()
	l := newAttemptLimiter(func() time.Time { return now })
	for i := 0; i < 10; i++ {
		if !l.allow("peer") {
			t.Fatal("early limit")
		}
	}
	if l.allow("peer") {
		t.Fatal("per-peer limit missing")
	}
	for i := 0; i < 50; i++ {
		if !l.allow(string(rune('a' + i))) {
			t.Fatal("early global limit")
		}
	}
	if l.allow("other") {
		t.Fatal("global limit missing")
	}
	now = now.Add(time.Minute)
	if !l.allow("peer") || len(l.peers) != 1 {
		t.Fatal("window did not reset/expire")
	}
}
func TestAttemptsUsePeerAndReturnRetryAfter(t *testing.T) {
	h := newTestHandler(fakeAuth{logout: func(string) error { return nil }})
	// Invalid content still consumes a login attempt.
	for i := 0; i < 10; i++ {
		perform(h, "POST", "/api/v1/auth/login", "", nil)
	}
	w := perform(h, "POST", "/api/v1/auth/login", "", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	assertError(t, w, 429, "rate_limited")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry hint")
	}
	if w := perform(h, "POST", "/api/v1/auth/logout", "", nil); w.Code != 204 {
		t.Fatal("logout was rate limited")
	}
}
func TestConcurrentHashLimit(t *testing.T) {
	l := newAttemptLimiter(time.Now)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var wg sync.WaitGroup
	h := l.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { started <- struct{}{}; <-release; w.WriteHeader(204) }))
	for i := 0; i < 4; i++ {
		wg.Go(func() { h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/", nil)) })
	}
	for i := 0; i < 4; i++ {
		<-started
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/", nil))
	close(release)
	wg.Wait()
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("concurrency limit: %d", w.Code)
	}
}
