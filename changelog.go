// Package searchgirl solo embebe el CHANGELOG.md en el binario: es la fuente
// de las novedades de cada version (cartel del log de arranque, aviso en la
// UI y notas del Release). Vive en la raiz porque //go:embed no puede subir
// de directorio; el resto del codigo esta en cmd/ e internal/.
package searchgirl

import _ "embed"

// Changelog es el texto de CHANGELOG.md tal como se compilo.
//
//go:embed CHANGELOG.md
var Changelog string
