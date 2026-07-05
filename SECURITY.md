# Política de seguridad

## Reportar una vulnerabilidad

Si encontrás una vulnerabilidad en Searchgirl, reportala de forma **privada**:

- Abrí un [security advisory](https://github.com/diegoparras/searchgirl/security/advisories/new) en GitHub (preferido), o
- Escribí a **diegoparras@gmail.com** con el asunto `[searchgirl][security]`.

Por favor **no** abras un issue público hasta que exista un fix disponible.

Incluí: versión afectada, pasos de reproducción, impacto estimado y, si podés, una prueba de concepto. Respondemos dentro de lo razonable para un proyecto mantenido por una persona.

## Alcance

Searchgirl es la capa de valor sobre [SearXNG](https://github.com/searxng/searxng), que corre como contenedor separado. Las vulnerabilidades **del propio SearXNG** deben reportarse a su proyecto. Este canal cubre el código de Searchgirl: la API, el servidor MCP, la UI, la capa de auth y el proxy de fetch/thumbnails.

## Modelo de despliegue seguro

- El `docker-compose.yml` publica el puerto **solo en `127.0.0.1`** por defecto.
- El binario **se niega a arrancar** en una interfaz pública sin autenticación (`checkExposure`).
- Para exponerlo: activá auth (login local, tokens Bearer o federación Lockatus), poné `COOKIE_SECURE=1` detrás de HTTPS, y considerá `SEARCHGIRL_TRUSTED_PROXIES` si hay un reverse proxy.

Ver [README.md](README.md) y [DEPLOY.md](DEPLOY.md) para el detalle.

## Versiones soportadas

Se da soporte de seguridad a la última versión publicada (`latest` / el tag semver más reciente). No hay backports a versiones anteriores.
