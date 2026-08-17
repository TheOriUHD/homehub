// Command pvesim runs the Proxmox VE API simulator as a standalone server for
// development and end-to-end tests. It prints the accepted API token on start.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	crand "crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"flag"
	"fmt"
	"log"
	"math/big"
	"net"
	"net/http"
	"time"

	"homehub/internal/pvesim"
)

func main() {
	addr := flag.String("addr", ":8006", "listen address")
	useTLS := flag.Bool("tls", false, "serve HTTPS with an in-memory self-signed certificate")
	flag.Parse()

	sim := pvesim.New(pvesim.Options{})
	scheme := "http"
	if *useTLS {
		scheme = "https"
	}
	fmt.Printf("pvesim listening on %s://%s/api2/json\n", scheme, *addr)
	fmt.Printf("API token: %s\n", sim.Token())

	srv := &http.Server{
		Addr:              *addr,
		Handler:           sim.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if *useTLS {
		cert, err := selfSignedCert()
		if err != nil {
			log.Fatalf("generate certificate: %v", err)
		}
		srv.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}}
		log.Fatal(srv.ListenAndServeTLS("", ""))
	}
	log.Fatal(srv.ListenAndServe())
}

func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), crand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: "pvesim"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"localhost", "pvesim"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
	}
	der, err := x509.CreateCertificate(crand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}
