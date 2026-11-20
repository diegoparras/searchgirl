package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/diegoparras/searchgirl/internal/novedades"
)

// cartelArranque escribe la version y que cambia en esta version (del
// CHANGELOG embebido). Es lo PRIMERO que sale al levantar: lo que se lee en
// los registros del deploy de EasyPanel. Nunca falla ni corta el arranque.
func cartelArranque(w io.Writer, changelog, ver string) {
	for _, l := range novedades.Cartel("SEARCHGIRL", ver, novedades.Parsear(changelog), 80) {
		fmt.Fprintln(w, "searchgirl: "+l)
	}
}

// runNotas imprime las notas de la version en markdown (las usa el CI para el
// resumen y el Release). Sale con error si la version no tiene su seccion en
// el CHANGELOG: ningun deploy sin decir que cambia.
//
//	searchgirl notas            -> markdown de la version actual
//	searchgirl notas -version   -> solo el numero
func runNotas(changelog, ver string, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("notas", flag.ContinueOnError)
	fs.SetOutput(stderr)
	soloVersion := fs.Bool("version", false, "imprimir solo el numero de version")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *soloVersion {
		fmt.Fprint(stdout, ver)
		return 0
	}
	v, ok := novedades.Buscar(novedades.Parsear(changelog), ver)
	if !ok || len(v.Secciones) == 0 {
		fmt.Fprintf(stderr, "CHANGELOG.md no tiene la seccion \"## [%s]\" (o esta vacia): agregala antes de publicar.\n", ver)
		return 1
	}
	fmt.Fprint(stdout, novedades.NotasMarkdown(v))
	return 0
}
