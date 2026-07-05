# Changelog

Formato basado en [Keep a Changelog](https://keepachangelog.com/es/1.1.0/); versionado [SemVer](https://semver.org/lang/es/).

## [0.5.2] — 2026-07-05

### Cambiado
- Federado: la pantalla de login vuelve a ser la card con branding Searchgirl ("Entrar con Lockatus", colores fucsia), pero ahora se sirve del lado del servidor apenas se detecta que no hay sesión — así aparece de inmediato, sin el flash del buscador que había antes ni el redirect directo al hub de v0.5.1.

## [0.5.1] — 2026-07-05

### Corregido
- Federado: una navegación de página sin sesión ahora redirige en el servidor directo a `/auth/login` (302) en vez de servir la SPA y decidir en el cliente. Elimina el flash del buscador antes de la pantalla de Lockatus. Los assets y las llamadas a la API no se ven afectados; `/auth/login` no entra en loop.

## [0.5.0] — 2026-07-05

### Añadido
- **Panel "Modelo IA" en la UI** (solo admin): elegí proveedor y modelo del modo Respuesta IA en runtime, sin redeploy. Dropdown de presets (Ollama / OpenRouter / Personalizado) que autocompleta la base URL, botón "cargar modelos" que lista los del servicio agrupados en recomendados/todos, campo API key con ojito, botones Probar y Guardar. El proveedor pasa a ser runtime-switchable (`llm.Store` envuelve al activo y cae a las env vars cuando no hay nada guardado).
- Persistencia de la elección en un volumen `/config` (`SEARCHGIRL_CONFIG_DIR`, `llm.json`); sin volumen, la elección vive en memoria hasta el reinicio (la UI lo avisa).
- Gate por rol: el panel y sus endpoints (`/api/settings`, `/api/settings/test`, `/api/settings/models`) solo los ve/usa un admin (federado `role==admin`; login local; standalone en loopback). `auth.IsAdmin`, `api` expone `llm.can_configure`.

## [0.4.2] — 2026-07-05

### Corregido
- **Bug de raíz**: `Response.Clone()` (usado por la caché en cada búsqueda) construía los slices con base `nil`, así que un `answers`/`infoboxes`/`corrections` vacío colapsaba a `nil` y serializaba como `null` en el JSON. La UI hacía `for...of` sobre eso y fallaba con "answers is not iterable" en búsquedas sin respuestas directas. Ahora `Clone()` usa base `[]T{}` y preserva los arrays vacíos como `[]`. Test de regresión por el path real (a través de `Clone`).
- Blindaje adicional en el frontend (defensa en profundidad): la vista de resultados coacciona a array cualquier lista del backend antes de iterar, para degradar en vez de romperse ante un shape inesperado.

## [0.4.1] — 2026-07-05

Ronda dinámica de auditoría (instancia viva) sobre v0.4.0: DAST (nuclei, 0 hallazgos en 1160 templates), carga (k6, p95 37ms bajo 30 VUs, rate limiter activo), mutation testing (gremlins en `internal/searx`, 70% de eficacia) y accesibilidad (axe WCAG 2.2 AA).

### Corregido
- Contraste de color (WCAG 2.2 AA): los títulos de resultados y los links de acento sobre fondo claro usaban el fucsia `#d6336c` (4.42:1, por debajo del 4.5 requerido) y el texto de motores tenía `opacity` que lo aclaraba. Se introdujo el token `--accent-ink` (fucsia oscurecido para texto sobre claro; acento normal en oscuro) y se quitó la opacity. Home y resultados quedan en 0 violaciones AA en claro y oscuro.

## [0.4.0] — 2026-07-04

Endurecimiento tras auditoría de seguridad multidimensional (0 hallazgos críticos; remediación de 1 alto + 5 medios + varios bajos).

### Añadido
- Tests del flujo OIDC completo (`handleCallback`): state desconocido/expirado, nonce mismatch, audiencia incorrecta, happy path y anti-replay del state.
- Tests de `ServeThumb` (guard SSRF del proxy de miniaturas): rechaza direcciones privadas, valida el esquema y sirve solo imágenes.
- `govulncheck` en el pipeline de CI (con toolchain `stable` para escanear contra la stdlib parcheada).
- Rangos reservados extra en el guard SSRF: Carrier-Grade NAT (`100.64.0.0/10`) y `240.0.0.0/4`.
- `SECURITY.md`, `CONTRIBUTING.md`, `CHANGELOG.md` y `.github/dependabot.yml`.

### Cambiado
- El servidor HTTP ahora usa `http.Server` con `ReadHeaderTimeout`/`ReadTimeout`/`WriteTimeout`/`IdleTimeout` y `MaxHeaderBytes` (mitiga Slowloris).
- Los mensajes de error de fallos de backend se sanitizan: el cliente recibe un mensaje genérico y el detalle se loguea server-side (no se filtran URL interna de SearXNG, lógica SSRF ni motivos de rechazo OIDC).
- El banner de arranque ya no imprime la URL interna de SearXNG.
- El endpoint `/thumb` entra al rate limiter por IP.
- Las GitHub Actions están pinneadas por SHA; `packages: write` acotado al job que publica la imagen.
- La imagen base del Dockerfile está pinneada por digest.

## [0.3.0] — 2026-07-04

### Añadido
- Tokens Bearer múltiples con nombre (`SEARCHGIRL_MCP_TOKEN=claude:...,n8n:...`) con revocación individual; `/auth/me` reporta `token_name`.
- `SEARCHGIRL_TRUSTED_PROXIES` para resolver la IP real del cliente vía `X-Forwarded-For` detrás de un reverse proxy; `SEARCHGIRL_RATE_RPS`/`SEARCHGIRL_RATE_BURST` configurables.

## [0.2.0] — 2026-07-04

### Añadido
- Login local standalone (`SEARCHGIRL_USER`/`SEARCHGIRL_PASS`) con la pantalla de entrada estándar Escriba. Ignorado en modo federado por contrato de la suite.

## [0.1.0] — 2026-07-04

### Añadido
- Primera versión: buscador de la Suite Escriba sobre SearXNG con interfaz web, API REST, servidor MCP (`search`, `url_read`, `answer`) y modo Respuesta IA opcional con citas.
- Cuatro modos de auth: abierto (loopback), login local, token Bearer y federado con Lockatus (OIDC PKCE).
- Guard SSRF con verificación de IP en tiempo de dial; caché in-memory con TTL y singleflight.
