package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSSRFGuardRefusesPrivate(t *testing.T) {
	// httptest listens on 127.0.0.1 — exactly what the guard must refuse
	// when AllowPrivate is off.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("the request must never reach the server")
	}))
	defer ts.Close()

	r := &Reader{MaxBytes: 1 << 20, AllowPrivate: false}
	_, err := r.Read(context.Background(), ts.URL, 0, false)
	if err == nil || !strings.Contains(err.Error(), "non-public") {
		t.Fatalf("loopback fetch must be refused, got: %v", err)
	}
}

func TestSSRFGuardRejectsSchemes(t *testing.T) {
	r := &Reader{AllowPrivate: true}
	for _, bad := range []string{"ftp://example.org/x", "file:///etc/passwd", "javascript:alert(1)", "//example.org", ""} {
		if _, err := r.Read(context.Background(), bad, 0, false); err == nil {
			t.Errorf("scheme must be refused: %q", bad)
		}
	}
}

func TestReadHTMLExtracts(t *testing.T) {
	page := `<!doctype html><html><head><title>Página de prueba</title></head><body>
	<nav><a href="https://nav.example/x">menu que no va</a></nav>
	<article>
	  <h1>Un título</h1>
	  <p>Primer <strong>párrafo</strong> con <a href="https://example.org/link">un link</a>.</p>
	  <ul><li>uno</li><li>dos</li></ul>
	  <pre><code>x := 1</code></pre>
	</article>
	<footer>pie que no va</footer>
	</body></html>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer ts.Close()

	r := &Reader{MaxBytes: 1 << 20, AllowPrivate: true}
	doc, err := r.Read(context.Background(), ts.URL, 0, false)
	if err != nil {
		t.Fatal(err)
	}
	if doc.Title != "Página de prueba" {
		t.Errorf("title = %q", doc.Title)
	}
	md := doc.Markdown
	for _, want := range []string{"# Un título", "**párrafo**", "[un link](https://example.org/link)", "- uno", "```"} {
		if !strings.Contains(md, want) {
			t.Errorf("markdown missing %q:\n%s", want, md)
		}
	}
	for _, banned := range []string{"menu que no va", "pie que no va"} {
		if strings.Contains(md, banned) {
			t.Errorf("markdown must drop chrome %q:\n%s", banned, md)
		}
	}
}

func TestReadTruncatesByLength(t *testing.T) {
	body := strings.Repeat("palabra ", 5000)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(body))
	}))
	defer ts.Close()

	r := &Reader{MaxBytes: 1 << 20, AllowPrivate: true}
	doc, err := r.Read(context.Background(), ts.URL, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	if !doc.Truncated {
		t.Error("must be marked truncated")
	}
	if n := len([]rune(doc.Markdown)); n > 130 {
		t.Errorf("markdown length = %d runes", n)
	}
}

func TestServeThumbRefusesPrivate(t *testing.T) {
	// El backend es loopback: el guard debe rechazarlo con AllowPrivate off.
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("la request nunca debe llegar al backend privado")
	}))
	defer backend.Close()

	r := &Reader{MaxBytes: 1 << 20, AllowPrivate: false}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/thumb?u="+url.QueryEscape(backend.URL+"/x.jpg"), nil)
	r.ServeThumb(rec, req)
	if rec.Code == http.StatusOK {
		t.Fatalf("thumb de dirección privada debe fallar, dio %d", rec.Code)
	}
}

func TestServeThumbValidatesScheme(t *testing.T) {
	r := &Reader{AllowPrivate: true}
	for _, bad := range []string{"", "ftp://x/y.jpg", "file:///etc/passwd", "javascript:alert(1)"} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/thumb?u="+url.QueryEscape(bad), nil)
		r.ServeThumb(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("esquema %q debe dar 400, dio %d", bad, rec.Code)
		}
	}
}

func TestServeThumbOnlyServesImages(t *testing.T) {
	// Content-Type no-imagen: se rechaza aunque el fetch sea exitoso (evita
	// usar /thumb como proxy genérico de contenido).
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html>no soy una imagen</html>"))
	}))
	defer backend.Close()

	r := &Reader{MaxBytes: 1 << 20, AllowPrivate: true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/thumb?u="+url.QueryEscape(backend.URL+"/page"), nil)
	r.ServeThumb(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("contenido no-imagen debe dar 404, dio %d", rec.Code)
	}
}

func TestServeThumbServesImage(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write([]byte{0xFF, 0xD8, 0xFF, 0xE0}) // cabecera JPEG mínima
	}))
	defer backend.Close()

	r := &Reader{MaxBytes: 1 << 20, AllowPrivate: true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/thumb?u="+url.QueryEscape(backend.URL+"/pic.jpg"), nil)
	r.ServeThumb(rec, req)
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("imagen válida: code=%d ct=%q", rec.Code, rec.Header().Get("Content-Type"))
	}
	if rec.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("falta X-Content-Type-Options nosniff en el thumb")
	}
}

func TestReadRefusesBinary(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte{0x00, 0x01})
	}))
	defer ts.Close()
	r := &Reader{AllowPrivate: true}
	if _, err := r.Read(context.Background(), ts.URL, 0, false); err == nil {
		t.Fatal("binary content must be refused")
	}
}
