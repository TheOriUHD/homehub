// Command caddysim runs the caddy-reader helper simulator as a standalone
// server for development and end-to-end tests. It opens a pairing window at
// startup and prints the code.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"homehub/internal/caddysim"
)

func main() {
	addr := flag.String("addr", ":9776", "listen address")
	readonly := flag.Bool("readonly", false, "start without write mode, like a helper missing --writable")
	flag.Parse()

	sim := caddysim.New()
	sim.SetWritable(!*readonly)
	code := sim.OpenPairing()
	fmt.Printf("caddysim listening on %s (writable: %v)\n", *addr, !*readonly)
	fmt.Printf("pairing code: %s %s\n", code[:3], code[3:])
	log.Fatal(http.ListenAndServe(*addr, sim))
}
