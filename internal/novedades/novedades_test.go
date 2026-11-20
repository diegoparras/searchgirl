package novedades

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// Novedades por version: salen del CHANGELOG.md (una sola fuente) y se ven en
// el log de arranque (deploy en EasyPanel), en la UI tras una actualizacion y
// en el Release de GitHub.

const muestra = "# Changelog\n\n" +
	"Formato basado en Keep a Changelog.\n\n" +
	"## [0.3.0] — 2026-11-21\n\n" +
	"Un parrafo suelto antes de las secciones, que no es un item.\n\n" +
	"### Añadido\n" +
	"- **Novedades** en la UI: se ven al entrar\n" +
	"  despues de una actualizacion (`GET /api/novedades`).\n\n" +
	"### Corregido\n" +
	"- Un arreglo corto.\n\n" +
	"### Vacia\n\n" +
	"## [0.2.1] - 2026-11-20\n\n" +
	"### Corregido\n" +
	"- Otro arreglo.\n\n" +
	"## [0.2.0]\n\n" +
	"### Añadido\n" +
	"- Algo grande.\n"

func versionesDe(vs []Version) []string {
	out := []string{}
	for _, v := range vs {
		out = append(out, v.Version)
	}
	return out
}

func TestParsearVersionesFechasSeccionesEItemsMultilinea(t *testing.T) {
	vs := Parsear(muestra)
	if got, want := versionesDe(vs), []string{"0.3.0", "0.2.1", "0.2.0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("versiones = %v, want %v", got, want)
	}
	if vs[0].Fecha != "2026-11-21" || vs[1].Fecha != "2026-11-20" || vs[2].Fecha != "" {
		t.Fatalf("fechas = %q %q %q", vs[0].Fecha, vs[1].Fecha, vs[2].Fecha)
	}
	var titulos []string
	for _, s := range vs[0].Secciones {
		titulos = append(titulos, s.Titulo)
	}
	if want := []string{"Añadido", "Corregido"}; !reflect.DeepEqual(titulos, want) {
		t.Fatalf("secciones = %v, want %v (las vacias se descartan)", titulos, want)
	}
	want := []string{"**Novedades** en la UI: se ven al entrar despues de una actualizacion (`GET /api/novedades`)."}
	if got := vs[0].Secciones[0].Items; !reflect.DeepEqual(got, want) {
		t.Fatalf("items = %q, want %q", got, want)
	}
}

func TestParsearTextoVacioNoRompe(t *testing.T) {
	if vs := Parsear(""); len(vs) != 0 {
		t.Fatalf("Parsear(\"\") = %v, want vacio", vs)
	}
}

func TestDesdeLaUltimaVistaHastaLaActual(t *testing.T) {
	vs := Parsear(muestra)
	casos := []struct {
		vista, actual string
		want          []string
		por           string
	}{
		{"0.2.0", "0.3.0", []string{"0.3.0", "0.2.1"}, "de la mas nueva a la mas vieja"},
		{"0.3.0", "0.3.0", []string{}, "ya vista: nada que mostrar"},
		{"", "0.3.0", []string{"0.3.0"}, "primera vez: solo la actual"},
		{"0.2.0", "0.2.1", []string{"0.2.1"}, "nunca muestra versiones posteriores a la que corre"},
		{"0.10.0", "0.3.0", []string{}, "compara como version, no como texto"},
	}
	for _, c := range casos {
		if got := versionesDe(Desde(vs, c.vista, c.actual)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Desde(%q, %q) = %v, want %v (%s)", c.vista, c.actual, got, c.want, c.por)
		}
	}
}

func TestHastaDevuelveTodasHastaLaActual(t *testing.T) {
	vs := Parsear(muestra)
	if got, want := versionesDe(Hasta(vs, "0.2.1")), []string{"0.2.1", "0.2.0"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Hasta = %v, want %v", got, want)
	}
}

func TestLineasLogEnTextoPlanoYNotasMarkdown(t *testing.T) {
	v := Parsear(muestra)[0]
	want := []string{
		"Añadido: Novedades en la UI: se ven al entrar despues de una actualizacion (GET /api/novedades).",
		"Corregido: Un arreglo corto.",
	}
	if got := LineasLog(v); !reflect.DeepEqual(got, want) {
		t.Fatalf("LineasLog = %q, want %q", got, want)
	}
	md := NotasMarkdown(v)
	if !regexp.MustCompile(`^### Añadido\n- \*\*Novedades\*\* en la UI`).MatchString(md) {
		t.Fatalf("NotasMarkdown sin la primera seccion en markdown:\n%s", md)
	}
	if !strings.Contains(md, "### Corregido\n- Un arreglo corto.") {
		t.Fatalf("NotasMarkdown sin la segunda seccion:\n%s", md)
	}
}

func TestCartelArranqueVersionPrimeroYCortadoAlAncho(t *testing.T) {
	vs := Parsear(muestra)
	l := Cartel("SEARCHGIRL", "0.3.0", vs, 60)
	for _, x := range l {
		if utf8.RuneCountInString(x) > 60 {
			t.Errorf("renglon de %d columnas (max 60): %q", utf8.RuneCountInString(x), x)
		}
		if strings.Contains(x, "**") || strings.Contains(x, "`") {
			t.Errorf("renglon con markdown: %q", x)
		}
	}
	raya := regexp.MustCompile(`^=+$`)
	if !raya.MatchString(l[0]) || !raya.MatchString(l[len(l)-1]) {
		t.Fatalf("el cartel va entre rayas: %q ... %q", l[0], l[len(l)-1])
	}
	if l[1] != "SEARCHGIRL v0.3.0  (2026-11-21)" {
		t.Fatalf("l[1] = %q", l[1])
	}
	if l[2] != "Que cambia en esta version:" {
		t.Fatalf("l[2] = %q", l[2])
	}
	if l[3] != "- Añadido: Novedades en la UI: se ven al entrar despues de" {
		t.Fatalf("l[3] = %q", l[3])
	}
	if !strings.HasPrefix(l[4], "    una actualizacion") {
		t.Fatalf("la continuacion queda sangrada: %q", l[4])
	}

	sin := Cartel("SEARCHGIRL", "9.9.9", vs, 60)
	if sin[1] != "SEARCHGIRL v9.9.9" || sin[2] != "Sin notas para esta version en el CHANGELOG." {
		t.Fatalf("sin seccion: %q", sin)
	}
	vacio := Cartel("SEARCHGIRL", "1.0.0", nil, 80)
	if vacio[2] != "Sin notas para esta version en el CHANGELOG." {
		t.Fatalf("sin CHANGELOG: %q", vacio)
	}
}

func TestBuscar(t *testing.T) {
	vs := Parsear(muestra)
	if v, ok := Buscar(vs, "0.2.1"); !ok || v.Version != "0.2.1" {
		t.Fatalf("Buscar(0.2.1) = %v, %v", v, ok)
	}
	if _, ok := Buscar(vs, "7.7.7"); ok {
		t.Fatal("Buscar(7.7.7) encontro algo")
	}
}
