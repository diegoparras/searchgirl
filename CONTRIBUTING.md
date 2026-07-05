# Contribuir a Searchgirl

Gracias por el interés. Searchgirl es parte del ecosistema Escriba y sigue sus convenciones.

## Desarrollo

Requisitos: Go 1.25+ y Docker (para probar con SearXNG real).

```bash
git clone https://github.com/diegoparras/searchgirl && cd searchgirl
go build ./cmd/searchgirl
go test ./...
```

Para probar la app completa (app + SearXNG interno):

```bash
docker compose up -d --build
# http://localhost:8089
```

## Antes de abrir un PR

- `go vet ./...` y `go test ./...` en verde.
- `gofmt -l .` sin salida (código formateado).
- Los cambios de seguridad (auth, guard SSRF, rate limit) **deben** venir con tests.
- Comentarios y mensajes de UI en español; identificadores de código en inglés.

## Estilo

- Go idiomático; sin dependencias nuevas salvo que sean imprescindibles (el binario es un estático liviano a propósito).
- La UI cumple el contrato de diseño de la Suite Escriba: acento propio (fucsia `#D6336C`), resto compartido, sin emojis, iconos SVG line, tema claro/oscuro.
- SearXNG se consume **solo por HTTP** — nunca se linkea ni se forkea su código (mantiene limpio el límite AGPL).

## Reportar bugs / seguridad

Bugs: [issues](https://github.com/diegoparras/searchgirl/issues). Vulnerabilidades: ver [SECURITY.md](SECURITY.md) (canal privado).

## Licencia

Al contribuir aceptás que tu aporte se licencie bajo [MIT](LICENSE), igual que el resto del proyecto.
