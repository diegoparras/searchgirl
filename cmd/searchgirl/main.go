// Command searchgirl is the Suite Escriba search app, by SearXNG: one static
// binary serving the web UI, the JSON API and the MCP server over the same
// port, with an official SearXNG container as the metasearch backend.
package main

import (
	"fmt"
	"os"

	"github.com/diegoparras/searchgirl"
)

// version es la unica fuente de verdad del numero: la usan el cartel de
// arranque, /api/config, el MCP y el CI (searchgirl notas -version). Cada
// version tiene su seccion en CHANGELOG.md (lo exige un test).
const version = "0.6.6"

func main() {
	// No arguments = serve. "Fácil de usar" starts here.
	if len(os.Args) < 2 {
		if err := cmdServe(nil); err != nil {
			fmt.Fprintln(os.Stderr, "searchgirl:", err)
			os.Exit(1)
		}
		return
	}
	args := os.Args[2:]
	var err error
	switch os.Args[1] {
	case "serve":
		err = cmdServe(args)
	case "version", "-v", "--version":
		fmt.Println("searchgirl " + version)
	case "notas":
		os.Exit(runNotas(searchgirl.Changelog, version, args, os.Stdout, os.Stderr))
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "searchgirl: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "searchgirl:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `searchgirl — el buscador de la Suite Escriba · By SearXNG

usage: searchgirl [command] [flags]

commands:
  serve             run the server (default command)
    -http :8080     serve UI + API + MCP over HTTP on this address;
                    empty = MCP over stdio (for local LLM clients)
  version           print the version
  notas             print this version's release notes (from CHANGELOG.md)
    -version        print only the version number

environment:
  SEARXNG_URL                  SearXNG base URL (default http://searxng:8080)
  SEARXNG_TIMEOUT              per-search timeout (default 10s)
  SEARCHGIRL_DEFAULT_LANGUAGE  default language (default es)
  SEARCHGIRL_SAFESEARCH        default safesearch 0|1|2 (default 0)
`)
}
