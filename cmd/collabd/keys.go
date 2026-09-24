package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/veliborsimonovic/collab/server"
)

func runKeygen() {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return
	}
	fmt.Println("COLLAB_PUBLIC_KEY=" + base64.StdEncoding.EncodeToString(pub))

	derBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		log.Fatalf("Failed to marshal private key to PKCS#8: %v", err)
	}

	pemBlock := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: derBytes,
	}

	err = pem.Encode(os.Stdout, pemBlock)
	if err != nil {
		log.Fatalf("Failed to write PEM to stdout: %v", err)
	}
}

func runToken(args []string) {
	fs := flag.NewFlagSet("token", flag.ExitOnError)
	keyPath := fs.String("key", "private.pem", "PEM file with the private key")
	doc := fs.String("doc", "", "document ID (required)")
	sub := fs.String("sub", "dev", "user ID")
	role := fs.String("role", "editor", "editor or viewer")
	name := fs.String("name", "Dev", "display name")
	color := fs.String("color", "#f60", "cursor colour")
	ttl := fs.Duration("ttl", time.Hour, "how long the token is valid, e.g. 30m, 2h")
	fs.Parse(args)

	if *doc == "" {
		log.Fatal("-doc is required")
	}

	raw, err := os.ReadFile(*keyPath)
	if err != nil {
		log.Fatal(err)
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		log.Fatalf("no PEM block found in %s", *keyPath)
	}
	k, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		log.Fatal(err)
	}
	priv, ok := k.(ed25519.PrivateKey)
	if !ok {
		log.Fatal("the key is not an Ed25519 private key")
	}

	fmt.Println(server.Sign(priv, server.Claims{
		Sub:   *sub,
		Doc:   *doc,
		Role:  *role,
		Name:  *name,
		Color: *color,
		Exp:   time.Now().Add(*ttl).Unix(),
	}))
}
