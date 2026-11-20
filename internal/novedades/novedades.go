// Package novedades lee las notas de cada version del CHANGELOG.md (una sola
// fuente de verdad) y las presenta en tres lugares: el cartel del log de
// arranque (lo primero que se ve en los registros del deploy en EasyPanel),
// la UI (aviso "Novedades" al entrar despues de una actualizacion) y el
// Release de GitHub que crea el CI.
//
// Formato esperado (Keep a Changelog):
//
//	## [X.Y.Z] - AAAA-MM-DD      (acepta guion o raya: "-", "–", "—")
//	### Seccion
//	- item (las lineas siguientes con sangria continuan el item)
package novedades

import (
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Seccion es un bloque "### Titulo" con sus items.
type Seccion struct {
	Titulo string   `json:"titulo"`
	Items  []string `json:"items"`
}

// Version es una entrada "## [X.Y.Z]" del CHANGELOG.
type Version struct {
	Version   string    `json:"version"`
	Fecha     string    `json:"fecha,omitempty"`
	Secciones []Seccion `json:"secciones"`
}

var (
	reVersion = regexp.MustCompile(`^## \[(\d+\.\d+\.\d+)\](?:\s*[-–—]\s*(\d{4}-\d{2}-\d{2}))?`)
	reSeccion = regexp.MustCompile(`^### (.+)`)
	reItem    = regexp.MustCompile(`^- (.*)`)
	reSigue   = regexp.MustCompile(`^\s+\S`)
	reNegrita = regexp.MustCompile(`\*\*(.+?)\*\*`)
	reCodigo  = regexp.MustCompile("`([^`]+)`")
)

// Parsear lee el texto del CHANGELOG, de la version mas nueva a la mas vieja
// (en el orden del archivo). Nunca falla: un texto vacio da una lista vacia.
func Parsear(texto string) []Version {
	vs := []Version{}
	var v *Version
	var sec *Seccion
	for _, linea := range strings.Split(strings.ReplaceAll(texto, "\r\n", "\n"), "\n") {
		if m := reVersion.FindStringSubmatch(linea); m != nil {
			vs = append(vs, Version{Version: m[1], Fecha: m[2], Secciones: []Seccion{}})
			v, sec = &vs[len(vs)-1], nil
			continue
		}
		if v == nil {
			continue
		}
		if strings.HasPrefix(linea, "## ") {
			v, sec = nil, nil
			continue
		}
		if m := reSeccion.FindStringSubmatch(linea); m != nil {
			v.Secciones = append(v.Secciones, Seccion{Titulo: strings.TrimSpace(m[1]), Items: []string{}})
			sec = &v.Secciones[len(v.Secciones)-1]
			continue
		}
		if sec == nil {
			continue
		}
		if m := reItem.FindStringSubmatch(linea); m != nil {
			sec.Items = append(sec.Items, strings.TrimSpace(m[1]))
			continue
		}
		if reSigue.MatchString(linea) && len(sec.Items) > 0 {
			sec.Items[len(sec.Items)-1] += " " + strings.TrimSpace(linea)
		}
	}
	for i := range vs {
		llenas := []Seccion{}
		for _, s := range vs[i].Secciones {
			if len(s.Items) > 0 {
				llenas = append(llenas, s)
			}
		}
		vs[i].Secciones = llenas
	}
	return vs
}

// Comparar compara dos versiones X.Y.Z como numeros (no como texto).
func Comparar(a, b string) int {
	pa, pb := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		x, y := parte(pa, i), parte(pb, i)
		if x != y {
			return x - y
		}
	}
	return 0
}

func parte(p []string, i int) int {
	if i >= len(p) {
		return 0
	}
	n, _ := strconv.Atoi(p[i])
	return n
}

// Desde devuelve las versiones posteriores a vista y hasta actual inclusive.
// Sin vista (primera vez en ese navegador), solo la actual.
func Desde(vs []Version, vista, actual string) []Version {
	out := []Version{}
	for _, v := range vs {
		if vista == "" {
			if v.Version == actual {
				out = append(out, v)
			}
			continue
		}
		if Comparar(v.Version, vista) > 0 && Comparar(v.Version, actual) <= 0 {
			out = append(out, v)
		}
	}
	return out
}

// Hasta devuelve todas las versiones hasta actual inclusive (historial).
func Hasta(vs []Version, actual string) []Version {
	out := []Version{}
	for _, v := range vs {
		if Comparar(v.Version, actual) <= 0 {
			out = append(out, v)
		}
	}
	return out
}

// Buscar devuelve la entrada de una version.
func Buscar(vs []Version, version string) (Version, bool) {
	for _, v := range vs {
		if v.Version == version {
			return v, true
		}
	}
	return Version{}, false
}

func plano(s string) string {
	return reCodigo.ReplaceAllString(reNegrita.ReplaceAllString(s, "$1"), "$1")
}

// LineasLog da una linea por item, sin markdown, para el log del contenedor.
func LineasLog(v Version) []string {
	out := []string{}
	for _, s := range v.Secciones {
		for _, it := range s.Items {
			out = append(out, s.Titulo+": "+plano(it))
		}
	}
	return out
}

// cortar parte un texto en renglones de hasta ancho columnas; el primero
// lleva pre y los siguientes sangria.
func cortar(texto string, ancho int, pre, sangria string) []string {
	out := []string{}
	linea, base := pre, true
	for _, palabra := range strings.Fields(texto) {
		if !base && utf8.RuneCountInString(linea)+1+utf8.RuneCountInString(palabra) > ancho {
			out = append(out, linea)
			linea, base = sangria, true
		}
		if !base {
			linea += " "
		}
		linea += palabra
		base = false
	}
	return append(out, linea)
}

// Cartel es lo que se imprime PRIMERO al levantar el contenedor (lo que se
// ve en los registros de EasyPanel): version, fecha y que cambia, cortado al
// ancho para leerlo sin scroll lateral. Sin la seccion de la version (o sin
// CHANGELOG) lo dice, y la app arranca igual.
func Cartel(nombre, version string, vs []Version, ancho int) []string {
	raya := strings.Repeat("=", min(ancho, 60))
	v, ok := Buscar(vs, version)
	titulo := nombre + " v" + version
	if ok && v.Fecha != "" {
		titulo += "  (" + v.Fecha + ")"
	}
	lineas := []string{raya, titulo}
	if !ok || len(v.Secciones) == 0 {
		lineas = append(lineas, "Sin notas para esta version en el CHANGELOG.")
	} else {
		lineas = append(lineas, "Que cambia en esta version:")
		for _, l := range LineasLog(v) {
			lineas = append(lineas, cortar(l, ancho, "- ", "    ")...)
		}
	}
	return append(lineas, raya)
}

// NotasMarkdown arma las notas en markdown para el Release de GitHub.
func NotasMarkdown(v Version) string {
	bloques := make([]string, 0, len(v.Secciones))
	for _, s := range v.Secciones {
		b := "### " + s.Titulo
		for _, it := range s.Items {
			b += "\n- " + it
		}
		bloques = append(bloques, b)
	}
	return strings.Join(bloques, "\n\n") + "\n"
}
