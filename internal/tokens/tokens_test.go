package tokens

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCreateVerifyRevoke(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	today := "2026-07-05"

	secret, item, err := s.Create("Claude Code", 0, today)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(secret, "sg_") || item.Label != "Claude Code" || item.ID == "" {
		t.Fatalf("create: secret=%q item=%+v", secret, item)
	}

	// Verify OK con el secreto correcto; falla con otro.
	if label, ok := s.Verify(secret); !ok || label != "Claude Code" {
		t.Fatalf("verify secreto correcto: ok=%v label=%q", ok, label)
	}
	if _, ok := s.Verify("sg_otracosa"); ok {
		t.Error("verify de un secreto inexistente no debe pasar")
	}
	if _, ok := s.Verify("no-empieza-con-sg"); ok {
		t.Error("secreto sin prefijo sg_ no debe pasar")
	}

	// List no expone el hash ni el secreto.
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("list = %d", len(list))
	}
	if b, _ := json.Marshal(list); strings.Contains(string(b), secret) || strings.Contains(string(b), "hash") {
		t.Errorf("List filtró secreto/hash: %s", b)
	}

	// Persiste en disco (hash, no el secreto).
	raw, _ := os.ReadFile(filepath.Join(dir, "tokens.json"))
	if strings.Contains(string(raw), secret) {
		t.Error("tokens.json NO debe contener el secreto en claro")
	}
	if !strings.Contains(string(raw), "hash") {
		t.Error("tokens.json debe guardar el hash")
	}

	// Revocar → deja de verificar.
	if !s.Revoke(item.ID) {
		t.Fatal("revoke debe devolver true")
	}
	if _, ok := s.Verify(secret); ok {
		t.Error("un token revocado no debe verificar")
	}
	if s.Revoke("tok_inexistente") {
		t.Error("revocar un id inexistente debe devolver false")
	}
}

func TestPersistsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	s1 := New(dir)
	secret, _, _ := s1.Create("n8n", 0, "2026-07-05")
	// Una instancia nueva sobre el mismo dir lee el token.
	s2 := New(dir)
	if label, ok := s2.Verify(secret); !ok || label != "n8n" {
		t.Fatal("el token no persistió entre instancias")
	}
}

func TestExpiry(t *testing.T) {
	s := New(t.TempDir())
	today := time.Now().UTC().Format("2006-01-02")
	// Token que ya venció (expires días negativos no aplica; usamos 0 y luego
	// forzamos un expires pasado editando el store no es limpio — mejor creamos
	// con hoy+1 y comprobamos que Verify usa la fecha real).
	secret, _, _ := s.Create("temporal", 1, today) // vence mañana → hoy válido
	if _, ok := s.Verify(secret); !ok {
		t.Error("un token que vence mañana debe valer hoy")
	}
	// Un token con expires en el pasado no vale: lo simulamos creando con
	// today futuro para que expires quede < hoy.
	past := "2020-01-01"
	sec2, _, _ := s.Create("vencido", 1, past) // expires 2020-01-02 < hoy
	if _, ok := s.Verify(sec2); ok {
		t.Error("un token vencido no debe verificar")
	}
}

func TestInMemoryNoConfigDir(t *testing.T) {
	s := New("") // sin persistencia
	if s.Persisted() {
		t.Fatal("sin configDir → Persisted false")
	}
	secret, _, err := s.Create("x", 0, "2026-07-05")
	if err != nil {
		t.Fatalf("create in-memory no debe fallar: %v", err)
	}
	if _, ok := s.Verify(secret); !ok {
		t.Error("in-memory: el token debe verificar en la misma instancia")
	}
	if s.count() != 1 {
		t.Errorf("count = %d", s.count())
	}
}
