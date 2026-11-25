package searx

import "sort"

// Merge suma a main los resultados de extra, que es otra búsqueda de la misma
// consulta (por ejemplo, a un motor que sólo responde sin idioma). Hace lo que
// SearXNG habría hecho con los dos en un solo pedido: un resultado repetido
// queda una vez, con los motores de los dos y la suma de los puntajes; el
// orden vuelve a ser por puntaje. Los motores que fallaron, las respuestas y
// las sugerencias se juntan sin repetir. main no se modifica.
func Merge(main, extra *Response) *Response {
	out := main.Clone()
	if extra == nil {
		return out
	}

	byKey := map[string]int{}
	for i, r := range out.Results {
		byKey[canonicalURL(r.URL)] = i
	}
	for _, r := range extra.Results {
		key := canonicalURL(r.URL)
		if i, ok := byKey[key]; ok {
			got := &out.Results[i]
			got.Engines = mergeEngines(got.Engines, r.Engines)
			got.Score += r.Score
			if got.Snippet == "" {
				got.Snippet = r.Snippet
			}
			if got.Published == "" {
				got.Published = r.Published
			}
			if got.Thumbnail == "" {
				got.Thumbnail = r.Thumbnail
			}
			continue
		}
		r.Engines = append([]string{}, r.Engines...)
		byKey[key] = len(out.Results)
		out.Results = append(out.Results, r)
	}
	sort.SliceStable(out.Results, func(i, j int) bool { return out.Results[i].Score > out.Results[j].Score })
	for i := range out.Results {
		out.Results[i].Rank = i + 1
	}
	out.Meta.Total = len(out.Results)

	out.Meta.EnginesFailed = mergeEngines(out.Meta.EnginesFailed, extra.Meta.EnginesFailed)
	out.Answers = appendNew(out.Answers, extra.Answers)
	out.Suggestions = appendNew(out.Suggestions, extra.Suggestions)
	out.Corrections = appendNew(out.Corrections, extra.Corrections)
	for _, ib := range extra.Infoboxes {
		repetido := false
		for _, mine := range out.Infoboxes {
			if mine.Title == ib.Title {
				repetido = true
				break
			}
		}
		if !repetido {
			out.Infoboxes = append(out.Infoboxes, ib)
		}
	}
	if extra.Meta.TookMs > out.Meta.TookMs {
		out.Meta.TookMs = extra.Meta.TookMs
	}
	return out
}

func appendNew(into, add []string) []string {
	seen := map[string]bool{}
	for _, s := range into {
		seen[s] = true
	}
	for _, s := range add {
		if !seen[s] {
			seen[s] = true
			into = append(into, s)
		}
	}
	return into
}
