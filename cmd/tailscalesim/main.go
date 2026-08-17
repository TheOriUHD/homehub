// Command tailscalesim runs the Tailscale cloud API simulator as a
// standalone server for development and end-to-end tests. It prints the
// accepted API key.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"github.com/TheOriUHD/homehub/internal/tailscalesim"
)

func main() {
	addr := flag.String("addr", ":8012", "listen address")
	flag.Parse()

	fmt.Printf("tailscalesim listening on %s\n", *addr)
	fmt.Printf("api key: %s  tailnet: %s\n", tailscalesim.APIKey, tailscalesim.Tailnet)
	log.Fatal(http.ListenAndServe(*addr, tailscalesim.New()))
}
