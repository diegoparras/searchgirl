package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStorePrecedenceEnvFallback(t *testing.T) {
	// Sin config guardada → usa el fallback (env).
	fb := &OpenAICompatible{BaseURL: "http://env", Model: "env-model"}
	s := NewStore(t.TempDir(), fb)
	if !s.Available() || s.Name() != "env-model" {
		t.Fatalf("sin config guardada debe usar el fallback: available=%v name=%q", s.Available(), s.Name())
	}
}

func TestStoreSaveWinsAndPersists(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, Noop{})
	if s.Available() {
		t.Fatal("Noop fallback: no debe estar configurado")
	}
	if err := s.Save(Settings{BaseURL: "http://ollama:11434/v1", Model: "qwen2.5:7b", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if !s.Available() || s.Name() != "qwen2.5:7b" {
		t.Fatalf("tras Save: available=%v name=%q", s.Available(), s.Name())
	}
	// Persiste: un Store nuevo sobre el mismo dir lo lee.
	s2 := NewStore(dir, Noop{})
	if !s2.Available() || s2.Name() != "qwen2.5:7b" {
		t.Fatal("la config no persistió entre instancias")
	}
	// Snapshot nunca expone la key.
	snap := s2.Snapshot()
	if snap["has_key"] != true {
		t.Error("has_key debe ser true")
	}
	if b, _ := json.Marshal(snap); contains(string(b), "\"k\"") {
		t.Errorf("Snapshot filtró la API key: %s", b)
	}
}

func TestStoreBlankKeyKeepsOld(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, Noop{})
	_ = s.Save(Settings{BaseURL: "http://x/v1", Model: "m", APIKey: "secreto"})
	// Guardar de nuevo sin key: debe conservar "secreto".
	_ = s.Save(Settings{BaseURL: "http://x/v1", Model: "m2", APIKey: ""})
	set, _ := s.read()
	if set.APIKey != "secreto" || set.Model != "m2" {
		t.Fatalf("key debía conservarse y model cambiar: %+v", set)
	}
}

func TestStoreClearTurnsOff(t *testing.T) {
	dir := t.TempDir()
	s := NewStore(dir, Noop{})
	_ = s.Save(Settings{BaseURL: "http://x/v1", Model: "m", APIKey: "k"})
	_ = s.Save(Settings{}) // vacío → apaga
	if s.Available() {
		t.Fatal("BaseURL/Model vacíos deben apagar el LLM")
	}
}

func TestModelsFor(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen2.5:7b"},{"id":"nomic-embed-text"},{"id":""}]}`))
	}))
	defer ts.Close()
	s := NewStore(t.TempDir(), Noop{})
	ids, err := s.ModelsFor(context.Background(), ts.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != "qwen2.5:7b" {
		t.Fatalf("models = %v (el id vacío debe filtrarse)", ids)
	}
}

func TestStoreNoPersistence(t *testing.T) {
	s := NewStore("", Noop{}) // sin configDir
	if s.Persisted() {
		t.Fatal("configDir vacío → Persisted false")
	}
	// Save no persiste pero recarga en memoria.
	if err := s.Save(Settings{BaseURL: "http://x/v1", Model: "m"}); err != nil {
		t.Fatalf("Save sin persistencia no debe fallar: %v", err)
	}
	if !s.Available() {
		t.Fatal("Save debe activar el provider aun sin persistencia")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
