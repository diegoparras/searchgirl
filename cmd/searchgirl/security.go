package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/diegoparras/searchgirl/internal/auth"
)

// checkExposure is the fail-safe: refuse to serve on a public interface with
// no authentication — an open Searchgirl is a free search/fetch proxy. An
// operator whose port is already private (firewall, SSH tunnel, VPN)
// overrides with SEARCHGIRL_ALLOW_INSECURE=1.
func checkExposure(addr string, a *auth.Auth) error {
	if a.Enabled() || isLoopback(addr) || os.Getenv("SEARCHGIRL_ALLOW_INSECURE") == "1" {
		return nil
	}
	return fmt.Errorf("refusing to serve on a non-loopback address (%s) with NO authentication.\n"+
		"  Anyone who can reach this port could search and fetch URLs through your server.\n"+
		"  Fix one of:\n"+
		"    - SEARCHGIRL_MCP_TOKEN=<secret>    Bearer auth for the MCP/API (recommended for a VPS)\n"+
		"    - AUTH_MODE=federado + LOCKATUS_*  OIDC login (Lockatus)\n"+
		"    - bind 127.0.0.1 and reach it over an SSH tunnel or VPN\n"+
		"  Or, if this port is already firewalled/private, set SEARCHGIRL_ALLOW_INSECURE=1 to override.\n"+
		"  (Docker publica el puerto solo en tu máquina: en docker-compose.yml esto ya viene resuelto)", addr)
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false // e.g. a bare ":8080" -> all interfaces -> not loopback
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// securityHeaders sets conservative defaults on every response. HSTS only
// when behind TLS (COOKIE_SECURE=1), so it never traps a plain-HTTP dev run.
func securityHeaders(next http.Handler, tls bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		if tls {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// --- per-IP rate limit (token bucket) on the sensitive paths ----------------
// Caps abuse of the search/fetch/answer endpoints. Generous enough for a
// human plus one agent.

type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64 // tokens per second
	burst   float64
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func newIPLimiter(rate, burst float64) *ipLimiter {
	return &ipLimiter{buckets: map[string]*tokenBucket{}, rate: rate, burst: burst}
}

func (l *ipLimiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[ip]
	if b == nil {
		l.buckets[ip] = &tokenBucket{tokens: l.burst - 1, last: now}
		return true
	}
	b.tokens += now.Sub(b.last).Seconds() * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (l *ipLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /auth/login entra al límite para frenar fuerza bruta del login local.
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" || r.URL.Path == "/auth/login" {
			if !l.allow(clientIP(r), time.Now()) {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
