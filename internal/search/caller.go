package search

import (
	"context"
	"strings"
)

// Quién busca. Lo pone el servidor HTTP (o la herramienta MCP) con el nombre
// del token Bearer que trajo el pedido, y el servicio lo usa para decidir qué
// motores privados de SearXNG le habilita. Una sesión de la interfaz o el
// modo stdio no tienen nombre: buscan sin motores privados.

type callerKey struct{}

// WithCaller devuelve un contexto que lleva el nombre del cliente que busca.
func WithCaller(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, callerKey{}, name)
}

// CallerFrom devuelve el nombre del cliente, o "" si no hay.
func CallerFrom(ctx context.Context) string {
	name, _ := ctx.Value(callerKey{}).(string)
	return name
}

// ParseEngineTokens lee SEARCHGIRL_SEARXNG_TOKENS: "nombre:token,nombre:token".
// Cada nombre es el de un token Bearer de Searchgirl (sin distinguir
// mayúsculas) y cada token, uno de los `tokens:` de un motor privado en el
// settings.yml de SearXNG. Un mismo nombre puede repetirse para tener varios.
// Las entradas sin ":" o con una mitad vacía se ignoran.
func ParseEngineTokens(s string) map[string][]string {
	out := map[string][]string{}
	for _, part := range strings.Split(s, ",") {
		name, tok, ok := strings.Cut(strings.TrimSpace(part), ":")
		name, tok = strings.ToLower(strings.TrimSpace(name)), strings.TrimSpace(tok)
		if !ok || name == "" || tok == "" {
			continue
		}
		out[name] = append(out[name], tok)
	}
	return out
}
