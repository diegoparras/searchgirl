package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/diegoparras/searchgirl/internal/novedades"
	"github.com/diegoparras/searchgirl/internal/search"
)

const changelogPrueba = "## [1.2.0] - 2026-11-21\n### Añadido\n- Tres.\n\n" +
	"## [1.1.0] - 2026-11-20\n### Añadido\n- Dos.\n\n" +
	"## [1.0.0] - 2026-11-19\n### Añadido\n- Uno.\n"

func getNovedades(t *testing.T, query string) (int, string, []string) {
	t.Helper()
	srv := &Server{Search: &search.Service{}, Version: "1.1.0", Novedades: novedades.Parsear(changelogPrueba)}
	mux := http.NewServeMux()
	srv.Mount(mux)
	app := httptest.NewServer(mux)
	defer app.Close()
	resp, err := http.Get(app.URL + "/api/novedades" + query)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Version   string              `json:"version"`
		Versiones []novedades.Version `json:"versiones"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	vs := []string{}
	for _, v := range out.Versiones {
		vs = append(vs, v.Version)
	}
	return resp.StatusCode, out.Version, vs
}

func TestNovedadesDesdeLaUltimaVista(t *testing.T) {
	casos := map[string][]string{
		"":                  {"1.1.0"},          // primera vez: solo la actual
		"?desde=1.0.0":      {"1.1.0"},          // nunca versiones posteriores a la que corre
		"?desde=1.1.0":      {},                 // ya vista
		"?desde=0.9.0":      {"1.1.0", "1.0.0"}, // todo lo que se perdio
		"?desde=<script>":   {"1.1.0"},          // desde invalido: como primera vez
		"?todas=1":          {"1.1.0", "1.0.0"}, // historial (menu Novedades)
		"?todas=1&desde=x1": {"1.1.0", "1.0.0"},
	}
	for q, want := range casos {
		code, ver, got := getNovedades(t, q)
		if code != http.StatusOK || ver != "1.1.0" || !reflect.DeepEqual(got, want) {
			t.Errorf("GET /api/novedades%s = %d %q %v, want 200 1.1.0 %v", q, code, ver, got, want)
		}
	}
}
