// Command aperturesim runs the Aperture NVR simulator as a standalone
// server for development and end-to-end tests. It prints the accepted API
// token.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/TheOriUHD/homehub/internal/aperturesim"
)

func main() {
	addr := flag.String("addr", ":8013", "listen address")
	flag.Parse()

	fmt.Printf("aperturesim listening on %s\n", *addr)
	fmt.Printf("api token: %s\n", aperturesim.APIKey)
	log.Fatal(http.ListenAndServe(*addr, aperturesim.New()))
}
