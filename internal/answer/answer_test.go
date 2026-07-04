package answer

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/diegoparras/searchgirl/internal/search"
	"github.com/diegoparras/searchgirl/internal/searx"
)

type fakeLLM struct {
	reply  string
	prompt string
}

func (f *fakeLLM) Available() bool { return true }
func (f *fakeLLM) Name() string    { return "fake-model" }
func (f *fakeLLM) Complete(_ context.Context, prompt string) (string, error) {
	f.prompt = prompt
	return f.reply, nil
}

func fakeService(t *testing.T) (*search.Service, func()) {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"query": "q", "results": [
			{"title": "Fuente uno", "url": "https://uno.example/", "content": "dato uno", "engine": "ddg", "score": 5},
			{"title": "Fuente dos", "url": "https://dos.example/", "content": "dato dos", "engine": "brave", "score": 3}
		], "answers": [], "corrections": [], "infoboxes": [], "suggestions": [], "unresponsive_engines": []}`))
	}))
	return &search.Service{Client: searx.New(ts.URL, time.Second), DefaultLanguage: "es"}, ts.Close
}

func TestAnswerBuildsPromptAndSources(t *testing.T) {
	svc, done := fakeService(t)
	defer done()
	prov := &fakeLLM{reply: "Según la primera fuente, el dato es uno [1]. Y también dos [2]."}
	e := &Engine{Search: svc, Provider: prov}

	res, err := e.Answer(context.Background(), Request{Query: "cuál es el dato"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 || res.Sources[0].N != 1 || res.Sources[0].Domain != "uno.example" {
		t.Fatalf("sources = %+v", res.Sources)
	}
	if res.Model != "fake-model" || !strings.Contains(res.Answer, "[1]") {
		t.Fatalf("result = %+v", res)
	}
	for _, want := range []string{"cuál es el dato", "[1] Fuente uno — uno.example", "dato uno", "[2] Fuente dos"} {
		if !strings.Contains(prov.prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prov.prompt)
		}
	}
}

func TestAnswerScrubsOutOfRangeCitations(t *testing.T) {
	svc, done := fakeService(t)
	defer done()
	prov := &fakeLLM{reply: "Cierto [1], dudoso [7], y [2]."}
	e := &Engine{Search: svc, Provider: prov}

	res, err := e.Answer(context.Background(), Request{Query: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.Answer, "[7]") {
		t.Fatalf("out-of-range citation survived: %q", res.Answer)
	}
	if !strings.Contains(res.Answer, "[1]") || !strings.Contains(res.Answer, "[2]") {
		t.Fatalf("valid citations must survive: %q", res.Answer)
	}
}

func TestAnswerWithoutProvider(t *testing.T) {
	svc, done := fakeService(t)
	defer done()
	e := &Engine{Search: svc}
	if _, err := e.Answer(context.Background(), Request{Query: "x"}); err == nil {
		t.Fatal("must fail without provider")
	}
}
