# Changelog

Formato basado en [Keep a Changelog](https://keepachangelog.com/es/1.1.0/); versionado [SemVer](https://semver.org/lang/es/).

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
