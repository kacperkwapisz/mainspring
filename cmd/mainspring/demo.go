package main

import (
	"crypto/rand"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net"
	"strconv"

	"github.com/kacperkwapisz/mainspring/internal/app"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
)

// runDemo serves the panel on localhost with an in-memory pool of made-up
// accounts, sessions and usage. Nothing is written to disk and no request
// reaches a provider with a real account.
func runDemo(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("demo", flag.ContinueOnError)
	fs.SetOutput(stderr)
	port := fs.Int("port", 8318, "port to listen on")
	signIn := fs.Bool("sign-in", false, "require a security key, to try the sign-in screens")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	secret := make([]byte, 32)
	_, _ = rand.Read(secret)
	mode := "proxy"
	if *signIn {
		mode = "webauthn"
	}
	booted, err := app.Create(map[string]string{
		"JWT_SECRET": hex.EncodeToString(secret), "DATABASE_PATH": ":memory:",
		"MAINSPRING_AUTH": mode, "HOST": "127.0.0.1", "PORT": strconv.Itoa(*port),
	})
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if err := app.SeedDemo(booted); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	if *signIn {
		if code, err := booted.Auth.EnsureSetupCode(); err == nil && code != "" {
			printSetupCode(stdout, code)
		}
	}
	addr := net.JoinHostPort("127.0.0.1", strconv.Itoa(*port))
	fmt.Fprintf(stdout, "mainspring demo on http://%s (made-up data, in memory)\n", addr)
	if err := httpx.Server(addr, booted.Handler).ListenAndServe(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}
