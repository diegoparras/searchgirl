<p align="center"><img src="internal/web/assets/searchgirl.svg" width="80" alt="Searchgirl"></p>

# Searchgirl

**El buscador de la Suite Escriba. By SearXNG.**

Metabúsqueda privada con cuatro caras en un solo binario: **interfaz web**, **API REST**,
**servidor MCP** para agentes/LLMs, y un modo **Respuesta IA** opcional que sintetiza
respuestas con citas. Sin tracking, sin perfiles: los motores ven a tu servidor, nunca a vos.

## Qué es (y qué no es)

Searchgirl es una **capa de valor sobre [SearXNG](https://github.com/searxng/searxng)**, el
metabuscador open source que agrega resultados de decenas de motores. SearXNG corre como
contenedor oficial **sin modificar**, interno y no expuesto; Searchgirl lo consume por HTTP y
le suma la API normalizada, el MCP, la UI de la familia Escriba y la síntesis con LLM.

No es un fork: SearXNG sigue siendo AGPL-3.0 en su contenedor, y Searchgirl (MIT) solo habla
con él por la red. Si algún día modificás SearXNG, esas modificaciones sí son AGPL.

## Arranque rápido (1 minuto)

Necesitás Docker (Docker Desktop en Windows/Mac, o docker-ce en un VPS).

```bash
git clone https://github.com/diegoparras/searchgirl
cd searchgirl
docker compose up -d --build
```

Abrí **http://localhost:8089** y listo: buscador privado andando.

## Las cuatro caras

### 1. Interfaz web

`http://localhost:8089` — home con sugerencias, resultados por categoría (General, Noticias,
Imágenes, Videos, Ciencia, IT…), filtros de idioma/fecha/SafeSearch, respuestas directas e
infoboxes, tema claro/oscuro. Las miniaturas pasan por el proxy propio (`/thumb`): tu
navegador nunca toca los hosts de los motores.

### 2. API REST

| Endpoint | Qué hace |
|---|---|
| `GET /api/search?q=...` | Búsqueda. Params: `category`, `language`, `time_range` (day/week/month/year), `safesearch` (0-2), `page`, `engines` |
| `GET /api/suggest?q=...` | Autocompletado |
| `POST /api/answer` | Síntesis IA con citas: `{"query": "...", "fetch_pages": true}` — 503 sin LLM |
| `POST /api/read` | URL → Markdown: `{"url": "https://..."}` |
| `GET /api/engines` · `/api/categories` | Catálogo de motores/categorías |
| `GET /api/config` | Versión, modo de auth, LLM disponible |
| `GET /healthz` | Vida |

```bash
curl "http://localhost:8089/api/search?q=searxng&category=news&time_range=week"
```

La respuesta es un shape **normalizado y estable** (dedup por URL, score, dominio, fechas
ISO), independiente del JSON crudo de SearXNG.

### 3. MCP (para Claude Code, Claude Desktop o cualquier cliente MCP)

El servidor MCP corre en `http://localhost:8089/mcp` (transporte streamable HTTP). Tools:

- **`search`** — metabúsqueda con `category`, `language`, `time_range`, `max_results`.
- **`url_read`** — trae una URL pública como Markdown (con guarda SSRF).
- **`answer`** — búsqueda + síntesis con citas `[n]` (solo aparece si hay LLM configurado).

```bash
# Claude Code:
claude mcp add --transport http searchgirl http://localhost:8089/mcp
```

¿Stdio? El mismo binario: `searchgirl serve` (sin `-http`) habla MCP por stdio; necesita
alcanzar SearXNG (descomentá el mapeo `127.0.0.1:8090:8080` en el compose y exportá
`SEARXNG_URL=http://localhost:8090`). Para lo cotidiano, el `/mcp` HTTP es lo recomendado.

### 4. Respuesta IA (opcional, OFF por defecto)

Con un modelo configurado, aparece el botón **Respuesta IA** en la UI, el endpoint
`POST /api/answer` y la tool MCP `answer`: busca, toma las mejores fuentes y redacta una
respuesta corta **citando [1][2]**, con la lista de fuentes al pie. Sin modelo, todo lo demás
funciona igual.

```bash
# Anthropic (nativo):
ANTHROPIC_API_KEY=sk-ant-...            # opcional: ANTHROPIC_MODEL

# o cualquier endpoint OpenAI-compatible — Ollama local, OpenRouter, DeepSeek:
LLM_BASE_URL=http://host.docker.internal:11434/v1
LLM_MODEL=qwen2.5:7b
```

## Configuración

| Variable | Default | Qué controla |
|---|---|---|
| `SEARXNG_URL` | `http://searxng:8080` | URL del SearXNG interno |
| `SEARXNG_TIMEOUT` | `10s` | Timeout por búsqueda |
| `SEARXNG_SECRET` | — | `secret_key` del contenedor SearXNG |
| `SEARCHGIRL_DEFAULT_LANGUAGE` | `es` | Idioma por defecto |
| `SEARCHGIRL_SAFESEARCH` | `0` | SafeSearch por defecto (0/1/2) |
| `SEARCHGIRL_CACHE_TTL` | `5m` | TTL de caché de búsquedas (`0` la apaga) |
| `SEARCHGIRL_CACHE_MAX` | `512` | Tope de entradas de caché |
| `SEARCHGIRL_FETCH_MAX_BYTES` | `2097152` | Tope de descarga en `url_read` |
| `SEARCHGIRL_FETCH_ALLOW_PRIVATE` | `0` | Permitir IPs privadas en `url_read` (guarda SSRF) |
| `ANTHROPIC_API_KEY` / `ANTHROPIC_MODEL` | — | Proveedor Anthropic (prioridad si está) |
| `LLM_BASE_URL` / `LLM_MODEL` / `LLM_API_KEY` | — | Proveedor OpenAI-compatible |
| `SEARCHGIRL_USER` / `SEARCHGIRL_PASS` | — | Login local standalone: un usuario, pantalla de entrada estándar Escriba |
| `SEARCHGIRL_MCP_TOKEN` | — | Bearer para proteger API+MCP en un VPS (sin OIDC) |
| `AUTH_MODE` | vacío | `federado` activa OIDC con Lockatus |
| `LOCKATUS_ISSUER` / `LOCKATUS_CLIENT_ID` / `LOCKATUS_REDIRECT_URI` | — | Requeridas en federado |
| `SECRET_KEY` | aleatoria | HMAC de la cookie de sesión |
| `COOKIE_SECURE` | `0` | `1` detrás de HTTPS |
| `SEARCHGIRL_ALLOW_INSECURE` | `0` | Permite servir sin auth en interfaz pública (bajo tu responsabilidad) |

**Login local (standalone):** para una instalación propia con pantalla de entrada, definí en
un `.env` junto al compose:

```bash
SEARCHGIRL_USER=diego
SEARCHGIRL_PASS=una-clave-larga
SECRET_KEY=un-secreto-para-la-cookie   # opcional; si falta, las sesiones se reinician con el contenedor
```

Aparece el login estándar de la familia Escriba (card con ojito en la clave) y todo — UI, API,
MCP, miniaturas — queda gateado hasta iniciar sesión. Es un único usuario, sin base de datos.
En modo **federado estas variables se ignoran**: el contrato de la suite prohíbe un login local
conviviendo con el SSO.

**Fail-safe:** si el binario escucha en una interfaz no-loopback sin ninguna auth, se niega a
arrancar (un buscador abierto es un proxy gratis para cualquiera). Docker publica el puerto
desde la red interna, así que el compose de este repo ya arranca bien; en un VPS usá
`SEARCHGIRL_USER`+`SEARCHGIRL_PASS`, `SEARCHGIRL_MCP_TOKEN`, federación, o
`SEARCHGIRL_ALLOW_INSECURE=1` si el puerto ya está firewalleado.

## Modo suite (federado con Lockatus)

En el `docker-compose.suite.yml` de la Suite Escriba, Searchgirl entra en el puerto **8089**
con `AUTH_MODE=federado`: login único vía Lockatus (PKCE S256, cookie HMAC), sin login local.
El acceso se gobierna desde la matriz del hub (roles `admin`/`usuario`). El seed
(`lockatus/scripts/seed-suite.mjs`) ya registra el cliente `searchgirl`.

## Deploy en paneles

Funciona igual en **Dokploy**, **Easypanel**, **Coolify** o **Portainer**: pegá el
`docker-compose.yml` de este repo, seteá `SEARXNG_SECRET` (y `COOKIE_SECURE=1` si el panel te
da HTTPS) y publicá el puerto 8089. Con dominio propio: poné el reverse proxy del panel
delante y listo.

## SearXNG por dentro

La config vive en [`searxng/settings.yml`](searxng/settings.yml):

- `search.formats: [html, json]` — **imprescindible**: sin `json` la API devuelve 403.
- `server.limiter: false` — SearXNG no está expuesto; el rate limit lo pone Searchgirl.
- `autocomplete: duckduckgo` — habilita `/api/suggest`.

¿Motores? SearXNG trae ~200 activos por defecto. Para curarlos, agregá al `settings.yml`:

```yaml
use_default_settings:
  engines:
    remove: [qwant, startpage]   # los que te fallen seguido
```

y `docker compose restart searxng`.

## Desarrollo

```bash
go test ./...          # unit tests (cliente searx, normalización, MCP, auth, SSRF, caché)
go build ./cmd/searchgirl
```

Estructura: `internal/searx` (cliente+normalización) · `internal/search` (servicio+caché) ·
`internal/api` · `internal/mcpsrv` · `internal/answer` (síntesis con citas) · `internal/llm`
(Anthropic + OpenAI-compatible) · `internal/fetch` (URL→Markdown, SSRF) · `internal/auth`
(OIDC Lockatus) · `internal/web` (SPA embebida).

## Licencia y créditos

Searchgirl es **MIT** (ver [LICENSE](LICENSE)).

El motor de metabúsqueda es **[SearXNG](https://github.com/searxng/searxng)** (AGPL-3.0),
consumido como servicio separado y sin modificaciones — gracias a su comunidad por años de
trabajo en búsqueda privada. Searchgirl no estaría acá sin ellos.

---

Diego Parras · Ecosistema Escriba
