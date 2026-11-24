# Changelog

Formato basado en [Keep a Changelog](https://keepachangelog.com/es/1.1.0/); versionado [SemVer](https://semver.org/lang/es/).

## [0.6.4] — 2026-11-24

### Añadido
- **Más motores en General, todos por el proxy**: `duckduckgo web`, que usa la API d.js del
  navegador y no la versión HTML que tira captcha, y Yahoo, para no depender sólo de Bing.
  Probados desde una IP hogareña: 34 resultados entre DuckDuckGo, DuckDuckGo web, Bing, Yahoo y
  Google CSE, ninguno sin respuesta.

### Corregido
- **Google CSE de Imágenes salía directo desde el VPS** y lo bloqueaban ("unusual traffic"):
  ahora también va por la red `residencial`.

## [0.6.3] — 2026-11-24

### Añadido
- **Varios proxies que se turnan.** La red `residencial` de `searxng/settings.yml` documenta la
  forma de lista (`all://` con varias URLs): SearXNG los usa por turno, pedido a pedido, y el
  reintento sale por el siguiente.

### Corregido
- **El reintento no cubría los bloqueos.** SearXNG sólo reintenta cuando no conecta; si el
  buscador respondía 403 o 429, no reintentaba, al revés de lo que decían el settings.yml y
  DEPLOY.md de 0.6.2. La red `residencial` suma `retry_on_http_error: [403, 429, 503]`.

## [0.6.2] — 2026-11-24

### Corregido
- **General en 0 resultados en un VPS.** DuckDuckGo, Bing y Google CSE rechazan las búsquedas
  que llegan desde la IP de un datacenter y quedaban todos "sin respuesta", mientras Noticias
  andaba. `searxng/settings.yml` los manda ahora por una red `residencial`: así como viene sale
  directo, y en un VPS se le pone el proxy residencial con una línea. Wikipedia y Wikidata salen
  directo. Cómo configurarlo en DEPLOY.md, "General en 0 resultados en un VPS".
- **Wikipedia y Wikidata no cargaban con SearXNG 2026.11.** La versión nueva exige
  `general.contact_url`; el settings.yml trae la URL del proyecto y se cambia con
  `SEARXNG_CONTACT_URL`. A Wikidata se le sube el timeout a 6 s.

### Cambiado
- Los motores de General quedan declarados en el settings.yml en vez de depender de los valores
  por defecto de SearXNG, que cambiaron en 2026.11 (apaga Google, Bing y Brave, desactiva
  Startpage y Mojeek). Google y Brave quedan apagados: responden 403 y 429 incluso desde una IP
  hogareña.
- DEPLOY.md toma el settings.yml del repo en lugar de una copia en línea.

## [0.6.1] — 2026-11-20

### Añadido
- **Cada deploy dice su versión y qué cambia.** Lo primero que escribe el contenedor al levantar
  (EasyPanel > servicio > Registros) es un cartel con la versión, la fecha y las novedades de esa
  versión, sacadas de este CHANGELOG, que ahora va embebido en el binario.
- **Aviso de Novedades en la interfaz**: al entrar después de una actualización aparece una sola
  vez (por navegador) lo que cambió desde la última versión que viste. El historial queda en el
  menú, **Novedades**, y en **Acerca de** con el enlace "Qué cambió". Lo sirve
  `GET /api/novedades`, detrás del mismo acceso que el resto de la API.
- **Cada versión publicada queda en GitHub**: la imagen se etiqueta también `:X.Y.Z` (con el
  label OCI de versión), se crea el Release `vX.Y.Z` con estas notas y el resumen de la corrida
  de Actions las muestra. Sin la sección de la versión en el CHANGELOG, la imagen no se publica.

### Cambiado
- Las acciones del CI pasan a sus versiones que corren sobre Node 24.

### Seguridad
- `golang.org/x/net` a v0.62.0: corrige las vulnerabilidades que govulncheck marcaba en v0.56.0
  (entre ellas GO-2026-6860, en el parser HTML que usa la lectura de URLs).

## [0.6.0] — 2026-07-05

### Añadido
- **Panel "Conexión · API y MCP" en la UI** (solo admin): emitir/revocar tokens Bearer para conectar apps y agentes (Claude Code, n8n, scripts) a la API y al servidor MCP. Muestra la URL del /mcp y de la API, la lista de tokens (etiqueta, creado, vence, revocar), un formulario con etiqueta + expiración (no vence / 30 / 90 / 365 días), y al emitir muestra el secreto una sola vez + el JSON de .mcp.json + el comando `claude mcp add`, cada uno con botón copiar.
- Token store persistente (`internal/tokens`) en el volumen /config (`tokens.json`, guarda solo el hash sha256, nunca el secreto). Los tokens emitidos desde la UI conviven con los de `SEARCHGIRL_MCP_TOKEN` (env): auth valida contra ambos vía un verifier. Sin volumen, viven en memoria hasta reiniciar.

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
