package main

import (
	"net/http/httptest"
	"testing"
)

func TestParseTrustedProxies(t *testing.T) {
	nets := parseTrustedProxies("172.16.0.0/12, 10.0.0.5, basura, ::1")
	if len(nets) != 3 {
		t.Fatalf("nets = %d, want 3 (la entrada inválida se ignora)", len(nets))
	}
	for ip, want := range map[string]bool{
		"172.18.3.4": true, "10.0.0.5": true, "::1": true,
		"10.0.0.6": false, "8.8.8.8": false,
	} {
		if got := ipInNets(ip, nets); got != want {
			t.Errorf("ipInNets(%s) = %v, want %v", ip, got, want)
		}
	}
}

func TestClientIPBehindTrustedProxy(t *testing.T) {
	l := newIPLimiter(20, 60)
	l.trusted = parseTrustedProxies("172.16.0.0/12")

	// Peer directo NO confiable: se usa el peer, el XFF se ignora (spoofing).
	r := httptest.NewRequest("GET", "/api/x", nil)
	r.RemoteAddr = "8.8.8.8:1234"
	r.Header.Set("X-Forwarded-For", "1.2.3.4")
	if ip := l.clientIP(r); ip != "8.8.8.8" {
		t.Errorf("peer no confiable: ip = %s, want 8.8.8.8", ip)
	}

	// Peer confiable (el proxy): se toma el hop más a la derecha NO confiable.
	r2 := httptest.NewRequest("GET", "/api/x", nil)
	r2.RemoteAddr = "172.18.0.2:1234"
	r2.Header.Set("X-Forwarded-For", "203.0.113.7, 172.18.0.9")
	if ip := l.clientIP(r2); ip != "203.0.113.7" {
		t.Errorf("tras proxy: ip = %s, want 203.0.113.7", ip)
	}

	// XFF vacío detrás del proxy: cae al peer.
	r3 := httptest.NewRequest("GET", "/api/x", nil)
	r3.RemoteAddr = "172.18.0.2:1234"
	if ip := l.clientIP(r3); ip != "172.18.0.2" {
		t.Errorf("sin XFF: ip = %s", ip)
	}

	// Sin proxies configurados: siempre el peer.
	l2 := newIPLimiter(20, 60)
	if ip := l2.clientIP(r2); ip != "172.18.0.2" {
		t.Errorf("sin trusted: ip = %s, want peer", ip)
	}
}
