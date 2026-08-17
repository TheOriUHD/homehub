// Command truenassim runs the TrueNAS REST v2.0 API simulator as a standalone
// server for development and end-to-end tests. It prints the accepted API key.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"homehub/internal/truenassim"
)

func main() {
	addr := flag.String("addr", ":8010", "listen address")
	key := flag.String("key", "1-homehub-truenas-sim-key", "accepted API key")
	flag.Parse()

	fmt.Printf("truenassim listening on %s\n", *addr)
	fmt.Printf("api key: %s\n", *key)
	log.Fatal(http.ListenAndServe(*addr, truenassim.Handler(*key)))
}
