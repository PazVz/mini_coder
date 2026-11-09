package httpapi

import (
	"net"
	"net/http"
	"sync"
	"time"
)

type attemptWindow struct {
	start time.Time
	count int
}
type attemptLimiter struct {
	mu     sync.Mutex
	peers  map[string]attemptWindow
	global attemptWindow
	now    func() time.Time
	slots  chan struct{}
}

func newAttemptLimiter(now func() time.Time) *attemptLimiter {
	return &attemptLimiter{peers: make(map[string]attemptWindow), now: now, slots: make(chan struct{}, 4)}
}

// Fixed one-minute windows: 10 attempts per peer and 60 per process. State is
// bounded; forwarded IP headers are deliberately not trusted.
func (l *attemptLimiter) allow(peer string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	for key, w := range l.peers {
		if now.Sub(w.start) >= time.Minute {
			delete(l.peers, key)
		}
	}
	if now.Sub(l.global.start) >= time.Minute {
		l.global = attemptWindow{start: now}
	}
	w, ok := l.peers[peer]
	if !ok {
		w = attemptWindow{start: now}
	}
	if w.count >= 10 || l.global.count >= 60 {
		return false
	}
	w.count++
	l.global.count++
	l.peers[peer] = w
	return true
}

func (l *attemptLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			peer = r.RemoteAddr
		}
		if !l.allow(peer) {
			w.Header().Set("Retry-After", "60")
			writeError(w, r, 429, "rate_limited", "认证请求过于频繁，请稍后重试")
			return
		}
		select {
		case l.slots <- struct{}{}:
			defer func() { <-l.slots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeError(w, r, 429, "rate_limited", "认证请求过于频繁，请稍后重试")
			return
		}
		next.ServeHTTP(w, r)
	})
}
