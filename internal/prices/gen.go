//go:build ignore

// Regenerates api.json from models.dev: go generate ./internal/prices
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"

	"github.com/kacperkwapisz/mainspring/internal/prices"
)

func main() {
	res, err := http.Get("https://models.dev/api.json")
	if err != nil {
		panic(err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		panic(err)
	}
	trimmed, err := prices.Trim(raw)
	if err != nil {
		panic(err)
	}
	var out bytes.Buffer
	if err := json.Indent(&out, trimmed, "", "  "); err != nil {
		panic(err)
	}
	out.WriteString("\n")
	if err := os.WriteFile("api.json", out.Bytes(), 0o644); err != nil {
		panic(err)
	}
}
