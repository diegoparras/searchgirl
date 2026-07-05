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

  # Opcional — Bearer para agentes (MCP/API por token, compone con el login).
  # Varios tokens con nombre; revocar uno = sacarlo de la lista y redeployar:
  # SEARCHGIRL_MCP_TOKEN=claude:<token largo>,n8n:<otro token>

  # Detrás del proxy del panel (Traefik/nginx interno de Docker): con esto el
  # rate limit distingue clientes reales en vez de ver todo como una sola IP:
  SEARCHGIRL_TRUSTED_PROXIES=172.16.0.0/12
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

## Federación con Lockatus en Easypanel

En vez del login local del paso 3, Searchgirl puede usar **SSO con Lockatus** (login único de
la Suite Escriba). En producción es incluso más simple que en local: cada servicio tiene su
dominio HTTPS real, así que no hace falta el truco de `host.docker.internal` — la URL pública
de Lockatus resuelve igual desde el navegador (front-channel) y desde el contenedor de
Searchgirl (back-channel, para validar los tokens).

### Prerequisito: Lockatus accesible por HTTPS

Lockatus tiene que estar desplegado con su propio dominio (p.ej. `https://auth.tu-dominio.com`),
su Postgres, y `LOCKATUS_ISSUER` = ese mismo dominio público. Ver el `DEPLOY.md` de Lockatus
(imagen `ghcr.io/diegoparras/lockatus:latest`). **Ese dominio es el que va en `LOCKATUS_ISSUER`
de Searchgirl** — sin `https` accesible no hay federación.

### 1. Registrar Searchgirl como cliente en Lockatus

En el admin de Lockatus (o por API), declará la app y su redirect_uri **exacto**:

- **slug**: `searchgirl`
- **redirect_uri**: `https://buscar.tu-dominio.com/auth/callback` (tu dominio de Searchgirl + `/auth/callback`)
- **roles**: `admin`, `usuario`

Por API (con la cookie de admin del hub):
```bash
curl -X PUT https://auth.tu-dominio.com/api/admin/apps/searchgirl \
  -H "Content-Type: application/json" -b cookies.txt \
  -d '{"name":"Searchgirl","roles":["admin","usuario"],
       "redirect_uris":["https://buscar.tu-dominio.com/auth/callback"]}'
```
Después, **asignale un rol** a tu usuario para la app `searchgirl` en la matriz de accesos del
hub (sin rol = `access_denied` al entrar).

### 2. Servicio `app` con `AUTH_MODE=federado`

Igual que el paso 3 de arriba, pero cambiás el bloque de auth. **No pongas `SEARCHGIRL_USER`/
`SEARCHGIRL_PASS`** — en federado se ignoran (el contrato de la suite prohíbe login local junto
al SSO):

```
SEARXNG_URL=http://searchgirl_searxng:8080
SEARCHGIRL_DEFAULT_LANGUAGE=es

# Federación Lockatus (cliente público + PKCE, sin secret):
AUTH_MODE=federado
LOCKATUS_ISSUER=https://auth.tu-dominio.com
LOCKATUS_CLIENT_ID=searchgirl
LOCKATUS_REDIRECT_URI=https://buscar.tu-dominio.com/auth/callback
SECRET_KEY=<64 hex al azar>            # firma la cookie de sesión local
COOKIE_SECURE=1                        # HTTPS del panel
SEARCHGIRL_TRUSTED_PROXIES=172.16.0.0/12

# Opcional — Respuesta IA (ver más abajo), y token Bearer para agentes:
# LLM_BASE_URL=... / ANTHROPIC_API_KEY=... / SEARCHGIRL_MCP_TOKEN=claude:<token>
```

El token Bearer (`SEARCHGIRL_MCP_TOKEN`) **compone** con la federación: los humanos entran por
Lockatus, los agentes (MCP/API) usan el token. Útil para conectar Claude Code sin pasar por SSO.

### 3. Verificar el SSO

```bash
curl https://buscar.tu-dominio.com/auth/me     # → {"mode":"federated","authenticated":false,...}
```
En el navegador: la card de login muestra **solo** "Entrar con Lockatus" → te lleva al hub →
volvés autenticado. `/auth/me` pasa a `authenticated:true` con tu email y rol.

> **El `redirect_uri` debe coincidir carácter por carácter** en tres lugares: el registro en
> Lockatus, la env `LOCKATUS_REDIRECT_URI`, y el dominio real de Searchgirl. Un `/` de más o
> `http` vs `https` = `redirect_uri mismatch`.

---

## Respuesta IA: elegí el modelo desde la UI

El modo "Respuesta IA" (síntesis con citas) es opcional. La forma cómoda de configurarlo es
**desde la propia UI**: entrás como admin, menú ⋮ → **Modelo IA**, elegís un preset (Ollama /
OpenRouter), "cargar modelos", elegís uno y guardás. La elección persiste si el servicio tiene
un **volumen `/config`** (ya viene en el compose; en Easypanel: servicio `app` → **Mounts →
Volume**, mount path `/config`). Sin ese volumen, la elección se pierde al reiniciar.

Presets y a qué apuntan:
- **Ollama** → `http://ollama:11434/v1` (si Ollama corre como servicio en el **mismo proyecto**
  de Easypanel; ajustá el hostname al de tu servicio Ollama). Sin API key.
- **OpenRouter** → `https://openrouter.ai/api/v1` + tu API key. Los modelos se listan solos.

También podés fijarlo por env vars (útil para arrancar ya configurado, sin tocar la UI); lo que
guardes desde la UI tiene prioridad sobre las env:
```
# OpenRouter:
LLM_BASE_URL=https://openrouter.ai/api/v1
LLM_MODEL=deepseek/deepseek-chat
LLM_API_KEY=sk-or-...
# ...o Ollama (servicio en el mismo proyecto):
LLM_BASE_URL=http://ollama:11434/v1
LLM_MODEL=qwen2.5:7b
```

Sin ningún modelo (ni UI ni env), Searchgirl anda igual — solo no aparece el botón Respuesta IA.
El panel "Modelo IA" solo lo ve un admin; el resto de los usuarios solo busca.

---

## Modo suite local (docker-compose)

Si corrés toda la Suite Escriba en una máquina, no uses esta guía: Searchgirl ya entra por el
`docker-compose.suite.yml` con `AUTH_MODE=federado` contra el Lockatus local
(`host.docker.internal:8081`). Ver la sección "Modo suite" del [README](README.md).
