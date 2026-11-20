package main

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/diegoparras/searchgirl"
	"github.com/diegoparras/searchgirl/internal/novedades"
)

// Cada deploy dice que cambia: la version del binario (la constante version,
// unica fuente de verdad) tiene que tener su seccion en el CHANGELOG y ser la
// primera.
func TestChangelogTieneLaSeccionDeLaVersionActual(t *testing.T) {
	vs := novedades.Parsear(searchgirl.Changelog)
	v, ok := novedades.Buscar(vs, version)
	if !ok {
		t.Fatalf("falta \"## [%s]\" en CHANGELOG.md", version)
	}
	if len(v.Secciones) == 0 {
		t.Fatalf("la seccion %s del CHANGELOG esta vacia", version)
	}
	if vs[0].Version != version {
		t.Fatalf("la version del binario (%s) tiene que ser la primera del CHANGELOG (es %s)", version, vs[0].Version)
	}
}

func TestCartelDeArranqueConPrefijoYAncho(t *testing.T) {
	var b bytes.Buffer
	cartelArranque(&b, searchgirl.Changelog, version)
	lineas := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lineas) < 4 {
		t.Fatalf("cartel demasiado corto: %q", lineas)
	}
	for _, l := range lineas {
		if !strings.HasPrefix(l, "searchgirl: ") {
			t.Errorf("renglon sin el prefijo de log: %q", l)
		}
		if n := utf8.RuneCountInString(strings.TrimPrefix(l, "searchgirl: ")); n > 80 {
			t.Errorf("renglon de %d columnas (max 80): %q", n, l)
		}
	}
	if !strings.HasPrefix(lineas[1], "searchgirl: SEARCHGIRL v"+version) {
		t.Fatalf("la version va primero: %q", lineas[1])
	}
	if lineas[2] != "searchgirl: Que cambia en esta version:" {
		t.Fatalf("lineas[2] = %q", lineas[2])
	}
}

func TestCartelSinChangelogNoRompe(t *testing.T) {
	var b bytes.Buffer
	cartelArranque(&b, "", "1.2.3")
	if !strings.Contains(b.String(), "searchgirl: SEARCHGIRL v1.2.3\n") ||
		!strings.Contains(b.String(), "searchgirl: Sin notas para esta version en el CHANGELOG.\n") {
		t.Fatalf("cartel sin CHANGELOG:\n%s", b.String())
	}
}

func TestNotasDeLaVersion(t *testing.T) {
	var out, errb bytes.Buffer
	if code := runNotas(searchgirl.Changelog, version, nil, &out, &errb); code != 0 {
		t.Fatalf("runNotas = %d, stderr %q", code, errb.String())
	}
	if !strings.HasPrefix(out.String(), "### ") {
		t.Fatalf("notas en markdown, empezando por una seccion: %q", out.String())
	}

	out.Reset()
	if code := runNotas(searchgirl.Changelog, version, []string{"-version"}, &out, &errb); code != 0 || out.String() != version {
		t.Fatalf("runNotas -version = %d, %q", code, out.String())
	}

	out.Reset()
	errb.Reset()
	if code := runNotas(searchgirl.Changelog, "9.9.9", nil, &out, &errb); code == 0 {
		t.Fatal("sin la seccion de la version tiene que salir con error (lo usa el CI)")
	}
	if !strings.Contains(errb.String(), "## [9.9.9]") {
		t.Fatalf("el error dice que seccion falta: %q", errb.String())
	}
}
