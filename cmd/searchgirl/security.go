package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
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
// human plus one agent. Tunable per .env:
//
//	SEARCHGIRL_RATE_RPS         tokens/segundo por IP (default 20; 0 desactiva)
//	SEARCHGIRL_RATE_BURST       ráfaga máxima (default 60)
//	SEARCHGIRL_TRUSTED_PROXIES  IPs o CIDRs del reverse proxy, separados por
//	                            coma (ej. "172.16.0.0/12"). Con el peer en la
//	                            lista, la IP del cliente se toma del
//	                            X-Forwarded-For (de derecha a izquierda,
//	                            salteando proxies confiables).

type ipLimiter struct {
	mu      sync.Mutex
	buckets map[string]*tokenBucket
	rate    float64 // tokens per second
	burst   float64
	trusted []*net.IPNet
}

type tokenBucket struct {
	tokens float64
	last   time.Time
}

func newIPLimiter(rate, burst float64) *ipLimiter {
	return &ipLimiter{buckets: map[string]*tokenBucket{}, rate: rate, burst: burst}
}

func newIPLimiterFromEnv() *ipLimiter {
	l := newIPLimiter(envFloat("SEARCHGIRL_RATE_RPS", 20), envFloat("SEARCHGIRL_RATE_BURST", 60))
	l.trusted = parseTrustedProxies(os.Getenv("SEARCHGIRL_TRUSTED_PROXIES"))
	return l
}

func envFloat(key string, def float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 {
		return def
	}
	return f
}

// parseTrustedProxies accepts bare IPs and CIDRs, comma-separated. Invalid
// entries are ignored (better a stricter limiter than a silent bypass).
func parseTrustedProxies(s string) []*net.IPNet {
	var out []*net.IPNet
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if !strings.Contains(part, "/") {
			if ip := net.ParseIP(part); ip != nil {
				bits := 32
				if ip.To4() == nil {
					bits = 128
				}
				out = append(out, &net.IPNet{IP: ip, Mask: net.CIDRMask(bits, bits)})
			}
			continue
		}
		if _, ipnet, err := net.ParseCIDR(part); err == nil {
			out = append(out, ipnet)
		}
	}
	return out
}

func ipInNets(ipStr string, nets []*net.IPNet) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	for _, n := range nets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// clientIP resolves the real client. If the direct peer is a trusted proxy,
// walk X-Forwarded-For right-to-left and return the first hop that is NOT a
// trusted proxy — the standard way to avoid spoofed XFF from untrusted peers.
func (l *ipLimiter) clientIP(r *http.Request) string {
	peer := hostOf(r.RemoteAddr)
	if len(l.trusted) == 0 || !ipInNets(peer, l.trusted) {
		return peer
	}
	parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
	for i := len(parts) - 1; i >= 0; i-- {
		hop := strings.TrimSpace(parts[i])
		if hop == "" {
			continue
		}
		if !ipInNets(hop, l.trusted) {
			return hop
		}
	}
	return peer
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
	if l.rate <= 0 { // SEARCHGIRL_RATE_RPS=0: sin límite (bajo tu responsabilidad)
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /auth/login entra al límite para frenar fuerza bruta del login local;
		// /thumb porque también hace fetch saliente (aunque tras el guard SSRF).
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/mcp" || r.URL.Path == "/auth/login" || r.URL.Path == "/thumb" {
			if !l.allow(l.clientIP(r), time.Now()) {
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func hostOf(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}
