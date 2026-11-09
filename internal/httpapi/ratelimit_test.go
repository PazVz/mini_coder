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
	limiter := newAttemptLimiter(func() time.Time { return now })
	for i := 0; i < 10; i++ {
		if !limiter.allow("peer") {
			t.Fatal("early limit")
		}
	}
	if limiter.allow("peer") {
		t.Fatal("per-peer limit missing")
	}
	for i := 0; i < 50; i++ {
		if !limiter.allow(string(rune('a' + i))) {
			t.Fatal("early global limit")
		}
	}
	if limiter.allow("other") {
		t.Fatal("global limit missing")
	}
	now = now.Add(time.Minute)
	if !limiter.allow("peer") || len(limiter.peers) != 1 {
		t.Fatal("window did not reset/expire")
	}
}

func TestAttemptsUsePeerAndReturnRetryAfter(t *testing.T) {
	h := newTestHandler(fakeAuth{logout: func(string) error { return nil }})
	// Invalid content still consumes a login attempt.
	for i := 0; i < 10; i++ {
		perform(h, http.MethodPost, "/api/v1/auth/login", "", nil)
	}
	w := perform(h, http.MethodPost, "/api/v1/auth/login", "", map[string]string{"X-Forwarded-For": "203.0.113.9"})
	assertError(t, w, http.StatusTooManyRequests, "rate_limited")
	if w.Header().Get("Retry-After") == "" {
		t.Fatal("missing retry hint")
	}
	if w := perform(h, http.MethodPost, "/api/v1/auth/logout", "", nil); w.Code != http.StatusNoContent {
		t.Fatal("logout was rate limited")
	}
}

func TestConcurrentHashLimit(t *testing.T) {
	limiter := newAttemptLimiter(time.Now)
	started := make(chan struct{}, 4)
	release := make(chan struct{})
	var wg sync.WaitGroup
	h := limiter.middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started <- struct{}{}
		<-release
		w.WriteHeader(http.StatusNoContent)
	}))
	for i := 0; i < 4; i++ {
		wg.Go(func() { h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", nil)) })
	}
	for i := 0; i < 4; i++ {
		<-started
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/", nil))
	close(release)
	wg.Wait()
	if w.Code != http.StatusTooManyRequests || w.Header().Get("Retry-After") != "1" {
		t.Fatalf("concurrency limit: %d", w.Code)
	}
}
