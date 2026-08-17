// Command ampsim runs the CubeCoders AMP API simulator as a standalone
// server for development and end-to-end tests. It prints the accepted
// credentials.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/TheOriUHD/homehub/internal/ampsim"
)

func main() {
	addr := flag.String("addr", ":8011", "listen address")
	flag.Parse()

	sim := ampsim.New()
	fmt.Printf("ampsim listening on %s\n", *addr)
	fmt.Printf("credentials: %s / %s\n", sim.Username, sim.Password)
	log.Fatal(http.ListenAndServe(*addr, sim))
}
