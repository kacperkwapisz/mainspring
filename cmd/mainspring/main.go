package main

import (
	// Timezones for the panel's "today": distroless images have no zoneinfo.
	_ "time/tzdata"

	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/app"
	"github.com/kacperkwapisz/mainspring/internal/httpx"
	"github.com/kacperkwapisz/mainspring/internal/proxy"
)

var version = "dev"

func main() {
	app.Version = version
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "auth":
			os.Exit(runAuth(os.Args[2:], nil, os.Stdout, os.Stderr))
		case "demo":
			os.Exit(runDemo(os.Args[2:], os.Stdout, os.Stderr))
		case "version", "--version", "-v":
			fmt.Println("mainspring " + version)
			os.Exit(0)
		}
	}
	healthcheck := flag.Bool("healthcheck", false, "exit 0 if GET /health succeeds")
	flag.Parse()
	if *healthcheck {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8317"
		}
		res, err := http.Get("http://127.0.0.1:" + port + "/health")
		if err != nil || res.StatusCode != 200 {
			os.Exit(1)
		}
		os.Exit(0)
	}

	booted, err := app.Create(nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	code, err := booted.Auth.EnsureSetupCode()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if code != "" {
		printSetupCode(os.Stdout, code)
	}
	go func() {
		_ = booted.Hub.Prices.Refresh(false)
		t := time.NewTicker(24 * time.Hour)
		for range t.C {
			_ = booted.Hub.Prices.Refresh(false)
		}
	}()
	go func() {
		t := time.NewTicker(6 * time.Hour)
		for range t.C {
			_, _ = booted.Hub.Usage.Purge()
		}
	}()
	go func() {
		// Refresh tokens first, then read limits for accounts that went quiet.
		tick := func() {
			_, _ = proxy.KeepaliveCredentials(context.Background(), booted.Hub)
			proxy.PollUsage(context.Background(), booted.Hub, proxy.UsageMaxAge)
		}
		tick()
		t := time.NewTicker(proxy.UsageMaxAge)
		for range t.C {
			tick()
		}
	}()
	go proxy.WatchResets(context.Background(), booted.Hub)
	go app.WatchStatus(context.Background(), booted.Hub)
	go func() {
		time.Sleep(15 * time.Second)
		app.CheckForUpdates(context.Background(), booted.Hub, false)
		t := time.NewTicker(app.UpdateEvery)
		for range t.C {
			app.CheckForUpdates(context.Background(), booted.Hub, false)
		}
	}()
	addr := net.JoinHostPort(booted.Env.Host, strconv.Itoa(booted.Env.Port))
	srv := httpx.Server(addr, booted.Handler)
	httpx.Logf("%s listening on http://%s", version, addr)
	log.Fatal(srv.ListenAndServe())
}
