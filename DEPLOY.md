# Desplegar Searchgirl

Searchgirl son **dos contenedores**: la app (imagen pública `ghcr.io/diegoparras/searchgirl`)
y su SearXNG interno (imagen oficial, nunca expuesto). Esta guía es punta a punta para
**Easypanel**; al final hay atajos para Dokploy/Coolify/Portainer y VPS pelado.

---

## Easypanel, punta a punta

Partís de un VPS con Easypanel andando (`curl -sSL https://get.easypanel.io | sh`).

### 1. Crear el proyecto

Easypanel → **Create Project** → nombre `searchgirl`.

> Dentro de un proyecto, los servicios se ven entre sí por DNS interno con el hostname
> `<proyecto>_<servicio>` (p.ej. `searchgirl_searxng`). Lo usamos en el paso 3.

### 2. Servicio `searxng` (el motor, interno)

**+ Service → App**, nombre `searxng`:

- **Source → Docker Image**: `ghcr.io/searxng/searxng:latest`
- **Environment**:
  ```
  SEARXNG_SECRET=<64 hex al azar>        # openssl rand -hex 32
  ```
- **Mounts → File Mount**, ruta `/etc/searxng/settings.yml`, contenido:
  ```yaml
  use_default_settings: true
  server:
    limiter: false          # interno: el rate limit lo pone Searchgirl
    image_proxy: true
  search:
    formats:
      - html
      - json                # IMPRESCINDIBLE: sin esto la API devuelve 403
    autocomplete: duckduckgo
    safe_search: 0
    default_lang: auto
  ui:
    static_use_hash: true
  ```
- **Domains**: **ninguno** (no lo expongas — es el backend interno).
- **Deploy**.

### 3. Servicio `app` (Searchgirl)

**+ Service → App**, nombre `app`:

- **Source → Docker Image**: `ghcr.io/diegoparras/searchgirl:latest`
- **Environment** (mínimo + login local):
  ```
  SEARXNG_URL=http://searchgirl_searxng:8080
  SEARCHGIRL_DEFAULT_LANGUAGE=es

  # Login local (pantalla estándar Escriba). Sin usuario/clave queda ABIERTO.
  SEARCHGIRL_USER=diego
  SEARCHGIRL_PASS=<una clave larga>
  SECRET_KEY=<64 hex al azar>            # firma la cookie de sesión
  COOKIE_SECURE=1                        # Easypanel te da HTTPS

  # Opcional — modo Respuesta IA (elegí UNO):
  # ANTHROPIC_API_KEY=sk-ant-...
  # ANTHROPIC_MODEL=claude-haiku-4-5-20251001
  # ...o cualquier endpoint OpenAI-compatible:
  # LLM_BASE_URL=https://openrouter.ai/api/v1
  # LLM_MODEL=deepseek/deepseek-chat
  # LLM_API_KEY=sk-or-...

  # Opcional — Bearer para agentes (MCP/API por token, compone con el login):
  # SEARCHGIRL_MCP_TOKEN=<token largo>
  ```
- **Domains → Add Domain**: tu dominio (p.ej. `buscar.tu-dominio.com`), **HTTPS on**,
  puerto interno **8080**. Easypanel emite el certificado solo.
- **Deploy**.

> Si `searchgirl_searxng` no resuelve (Easypanel viejo), probá `searxng` a secas como
> hostname, o mirá el hostname que muestra la pestaña del servicio.

### 4. Verificar

```bash
curl https://buscar.tu-dominio.com/healthz          # → ok
curl https://buscar.tu-dominio.com/auth/me          # → {"mode":"local","authenticated":false,...}
```

En el navegador: aparece el login (card Escriba con ojito) → entrás con tu usuario del paso 3
→ buscás algo → en el pie de resultados dice "Resultados vía SearXNG". Si configuraste LLM,
aparece el botón **Respuesta IA** junto a los filtros.

MCP para Claude Code (si seteaste `SEARCHGIRL_MCP_TOKEN`):

```bash
claude mcp add --transport http searchgirl https://buscar.tu-dominio.com/mcp \
  --header "Authorization: Bearer <tu token>"
```

### 5. Actualizar

Easypanel → servicio `app` → **Deploy** (fuerza el pull de `latest`). Lo mismo para `searxng`
cuando quieras su imagen nueva. Cero estado que migrar: Searchgirl no tiene base de datos.

### Troubleshooting

| Síntoma | Causa y arreglo |
|---|---|
| Búsquedas fallan con "searxng returned 403" | El File Mount de `settings.yml` no está o le falta `json` en `search.formats`. Revisá la ruta exacta `/etc/searxng/settings.yml` y redeployá `searxng`. |
| "searxng unreachable" | `SEARXNG_URL` no resuelve: verificá el hostname interno (`searchgirl_searxng` vs `searxng`) y que ambos servicios estén en el MISMO proyecto. |
| El login no persiste entre deploys | Falta `SECRET_KEY`: sin ella la clave de la cookie es efímera y las sesiones mueren con el contenedor. |
| Entra sin pedir login | No están `SEARCHGIRL_USER`/`SEARCHGIRL_PASS` en el servicio `app` (o quedaron en el servicio equivocado). |
| `refusing to serve on a non-loopback address` en los logs | Estás corriendo el binario SIN auth fuera de Docker/loopback. Poné usuario+clave, token, o `SEARCHGIRL_ALLOW_INSECURE=1` si el puerto ya está firewalleado. |
| Respuesta IA no aparece | `/api/config` → `llm.available` debe ser `true`; revisá la API key y mirá los logs del servicio `app`. |

---

## Otros paneles / VPS pelado

- **Dokploy / Coolify / Portainer**: los tres aceptan Docker Compose — pegá el
  [`docker-compose.yml`](docker-compose.yml) del repo tal cual (ya monta `searxng/` y no
  expone el motor), definí en el panel las mismas env del paso 3 y poné su reverse proxy
  delante del puerto 8089 con HTTPS (`COOKIE_SECURE=1`).
- **VPS pelado**:
  ```bash
  git clone https://github.com/diegoparras/searchgirl && cd searchgirl
  printf 'SEARCHGIRL_USER=diego\nSEARCHGIRL_PASS=%s\nSECRET_KEY=%s\nSEARXNG_SECRET=%s\n' \
    "$(openssl rand -hex 16)" "$(openssl rand -hex 32)" "$(openssl rand -hex 32)" > .env
  cat .env                     # guardá la clave generada
  docker compose up -d
  ```
  y un Caddy/nginx adelante para TLS (después: `COOKIE_SECURE=1` en `.env` y `docker compose up -d`).

## Modo suite (federado)

Si ya corrés la Suite Escriba con Lockatus, no uses esta guía: Searchgirl entra por el
`docker-compose.suite.yml` de la suite con `AUTH_MODE=federado` (SSO, sin login local). Ver
sección "Modo suite" del [README](README.md).
