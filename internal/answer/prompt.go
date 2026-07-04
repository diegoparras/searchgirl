package answer

import (
	"fmt"
	"strings"
)

// SourceText is one numbered source as the prompt sees it.
type SourceText struct {
	N      int
	Title  string
	Domain string
	Text   string // snippet, or fetched page excerpt
}

// BuildPrompt is deterministic and model-free so it can be tested without a
// provider. The instruction is in Spanish (the suite's voice); the model
// answers in the user's query language.
func BuildPrompt(query string, sources []SourceText) string {
	var b strings.Builder
	b.WriteString(`Sos el modo Respuesta de Searchgirl, el buscador de la Suite Escriba.
Respondé la consulta usando SOLO la información de las fuentes numeradas de abajo.

Reglas:
- Respondé en el mismo idioma de la consulta.
- Citá cada afirmación con el número de su fuente entre corchetes, así: [1] o [2][3].
- No inventes nada que no esté en las fuentes. Si las fuentes no alcanzan para responder, decilo explícitamente.
- Sé conciso: unos pocos párrafos en Markdown, sin encabezados.
- No repitas la lista de fuentes al final; la interfaz ya la muestra.

Consulta: `)
	b.WriteString(query)
	b.WriteString("\n\nFuentes:\n")
	for _, s := range sources {
		fmt.Fprintf(&b, "\n[%d] %s — %s\n%s\n", s.N, s.Title, s.Domain, strings.TrimSpace(s.Text))
	}
	return b.String()
}
