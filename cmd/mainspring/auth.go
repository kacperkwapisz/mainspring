package main

import (
	"fmt"
	"io"

	"github.com/kacperkwapisz/mainspring/internal/auth"
	"github.com/kacperkwapisz/mainspring/internal/db"
	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

const authUsage = "usage: mainspring auth reset|status"

// runAuth runs `mainspring auth <cmd>` against the database the hub would
// open with the same environment. source overrides os env (tests).
func runAuth(args []string, source map[string]string, stdout, stderr io.Writer) int {
	if len(args) != 1 || (args[0] != "reset" && args[0] != "status") {
		fmt.Fprintln(stderr, authUsage)
		return 2
	}
	e, err := env.Load(source)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	d, err := db.Open(e.DatabasePath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	defer d.Close()
	svc := auth.New(e, store.NewWebAuthnStore(d), store.NewSettingsStore(d))
	switch args[0] {
	case "reset":
		code, removed, err := svc.Reset()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "removed %d key(s) and signed out every panel session\n", removed)
		printSetupCode(stdout, code)
	case "status":
		creds, err := svc.Creds.List()
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 1
		}
		fmt.Fprintf(stdout, "mode: %s\n", e.AuthMode)
		if e.AuthMode == env.AuthProxy {
			fmt.Fprintf(stdout, "require key: %t\n", svc.RequireKey())
		}
		fmt.Fprintf(stdout, "keys: %d\n", len(creds))
		for _, c := range creds {
			fmt.Fprintf(stdout, "  %s (%s, %s)\n", c.Name, c.Kind, c.RPID)
		}
	}
	return 0
}

func printSetupCode(w io.Writer, code string) {
	fmt.Fprintf(w, "mainspring setup code: %s — open the panel to add your first security key\n", code)
}
