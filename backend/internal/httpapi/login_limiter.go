package httpapi

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

type loginAttempt struct {
	Count        int
	WindowStart  time.Time
	BlockedUntil time.Time
}

type loginLimiter struct {
	mu        sync.Mutex
	attempts  map[string]loginAttempt
	limit     int
	window    time.Duration
	blockFor  time.Duration
	lastSweep time.Time
}

func newLoginLimiter(limit int, window, blockFor time.Duration) *loginLimiter {
	return &loginLimiter{
		attempts: make(map[string]loginAttempt),
		limit:    limit,
		window:   window,
		blockFor: blockFor,
	}
}

func (l *loginLimiter) Allow(ip, username string) (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.sweepExpired(now)
	key := loginAttemptKey(ip, username)
	entry, ok := l.attempts[key]
	if !ok {
		return 0, true
	}

	if !entry.BlockedUntil.IsZero() && now.Before(entry.BlockedUntil) {
		return time.Until(entry.BlockedUntil), false
	}

	if now.Sub(entry.WindowStart) >= l.window {
		delete(l.attempts, key)
		return 0, true
	}

	return 0, true
}

func (l *loginLimiter) RecordFailure(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now()
	l.sweepExpired(now)
	key := loginAttemptKey(ip, username)
	entry := l.attempts[key]

	if entry.WindowStart.IsZero() || now.Sub(entry.WindowStart) >= l.window {
		entry = loginAttempt{
			Count:       1,
			WindowStart: now,
		}
	} else {
		entry.Count++
	}

	if entry.Count >= l.limit {
		entry.BlockedUntil = now.Add(l.blockFor)
	}

	l.attempts[key] = entry
}

func (l *loginLimiter) RecordSuccess(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.sweepExpired(time.Now())
	delete(l.attempts, loginAttemptKey(ip, username))
}

func loginAttemptKey(ip, username string) string {
	return strings.ToLower(strings.TrimSpace(username)) + "|" + strings.TrimSpace(ip)
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
			return strings.TrimSpace(parts[0])
		}
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}

	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func formatRetryAfter(duration time.Duration) string {
	if duration <= 0 {
		return "60"
	}

	seconds := int(duration.Round(time.Second).Seconds())
	if seconds < 1 {
		seconds = 1
	}
	return fmt.Sprintf("%d", seconds)
}

func (l *loginLimiter) sweepExpired(now time.Time) {
	if !l.lastSweep.IsZero() && now.Sub(l.lastSweep) < time.Minute {
		return
	}

	for key, entry := range l.attempts {
		expiresAt := entry.WindowStart.Add(l.window)
		if entry.BlockedUntil.After(expiresAt) {
			expiresAt = entry.BlockedUntil
		}
		if !expiresAt.IsZero() && !now.Before(expiresAt) {
			delete(l.attempts, key)
		}
	}

	l.lastSweep = now
}
