package searx

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestCanonicalURL(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"https://go.dev/", "https://go.dev/#main", true},
		{"https://go.dev/", "http://GO.DEV", true},
		{"https://go.dev/doc", "https://go.dev/blog", false},
		{"https://go.dev/?q=1", "https://go.dev/", false},
	}
	for _, c := range cases {
		if got := canonicalURL(c.a) == canonicalURL(c.b); got != c.same {
			t.Errorf("canonical(%q) vs canonical(%q): same=%v, want %v", c.a, c.b, got, c.same)
		}
	}
}

func TestDomainOf(t *testing.T) {
	if d := domainOf("https://www.Example.COM/path"); d != "example.com" {
		t.Errorf("domainOf = %q", d)
	}
	if d := domainOf("not a url at all ::"); d != "" {
		t.Errorf("bad URL should give empty domain, got %q", d)
	}
}

func TestIsoDate(t *testing.T) {
	cases := map[string]string{
		"2026-05-01T10:30:00":  "2026-05-01",
		"2026-05-01T10:30:00Z": "2026-05-01",
		"2026-05-01":           "2026-05-01",
		"2026-05-01 10:30:00":  "2026-05-01",
		"yesterday":            "",
		"":                     "",
	}
	for in, want := range cases {
		if got := isoDate(in); got != want {
			t.Errorf("isoDate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClipRespectsRunes(t *testing.T) {
	long := ""
	for i := 0; i < 400; i++ {
		long += "ñ"
	}
	got := clip(long, 320)
	if len([]rune(got)) > 321 { // 320 + ellipsis
		t.Errorf("clip too long: %d runes", len([]rune(got)))
	}
}

func TestNormalizeEmptyPayload(t *testing.T) {
	resp := normalize(&rawResponse{}, Params{Query: "x", Category: "general", Page: 0})
	if resp.Results == nil || resp.Answers == nil || resp.Suggestions == nil || resp.Infoboxes == nil || resp.Corrections == nil {
		t.Fatal("slices must be non-nil so JSON serializes [] instead of null")
	}
	if resp.Page != 1 {
		t.Errorf("page 0 must normalize to 1, got %d", resp.Page)
	}
	if resp.Query != "x" {
		t.Errorf("query fallback = %q", resp.Query)
	}
}

func TestCloneKeepsEmptySlicesNonNil(t *testing.T) {
	// Regresión: Clone() con base nil colapsaba los slices vacíos a nil, que
	// serializa como "null" y rompía a la UI (for...of sobre null). El path
	// real de búsqueda SIEMPRE pasa por Clone (caché), así que se testea acá.
	orig := normalize(&rawResponse{}, Params{Query: "x", Category: "general"})
	cl := orig.Clone()
	for name, s := range map[string][]string{
		"answers": cl.Answers, "suggestions": cl.Suggestions,
		"corrections": cl.Corrections, "engines_failed": cl.Meta.EnginesFailed,
	} {
		if s == nil {
			t.Errorf("Clone().%s es nil; debe ser [] para serializar como array", name)
		}
	}
	if cl.Results == nil || cl.Infoboxes == nil {
		t.Error("Clone(): results/infoboxes no deben ser nil")
	}

	// Y debe serializar como [] en JSON, no null.
	b, _ := json.Marshal(cl)
	for _, k := range []string{`"answers":[]`, `"results":[]`, `"infoboxes":[]`, `"suggestions":[]`, `"corrections":[]`} {
		if !strings.Contains(string(b), k) {
			t.Errorf("JSON del Clone no contiene %s:\n%s", k, b)
		}
	}
}

func TestFlexiString(t *testing.T) {
	if s := flexiString([]byte(`"plain"`)); s != "plain" {
		t.Errorf("bare string = %q", s)
	}
	if s := flexiString([]byte(`{"answer": "obj"}`), "answer"); s != "obj" {
		t.Errorf("object = %q", s)
	}
	if s := flexiString([]byte(`{"other": 3}`), "answer"); s != "" {
		t.Errorf("missing key = %q", s)
	}
}
