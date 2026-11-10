package prices

import (
	"bytes"
	"encoding/json"
	"sort"
	"sync/atomic"
)

// The model catalog starts as the api.json built into the binary and is
// replaced by the copy Mainspring downloads from models.dev every day, so new
// models show up without a Mainspring release.

// catalogProviders are the models.dev providers whose models Mainspring serves.
var catalogProviders = []string{"anthropic", "openai", "xai"}

var live atomic.Pointer[[]byte]

// API returns the current catalog: the latest download, else the built-in one.
func API() []byte {
	if p := live.Load(); p != nil {
		return *p
	}
	return APIJSON
}

// SetLive replaces the catalog with a trimmed download (see Trim).
func SetLive(trimmed []byte) {
	if len(trimmed) == 0 {
		return
	}
	live.Store(&trimmed)
}

type trimmedModel struct {
	ID      string          `json:"id"`
	Name    string          `json:"name,omitempty"`
	Cost    json.RawMessage `json:"cost,omitempty"`
	Limit   json.RawMessage `json:"limit,omitempty"`
	Release string          `json:"-"`
}

// Trim keeps the providers and fields Mainspring reads from a models.dev
// api.json, with each provider's models newest first.
func Trim(raw []byte) ([]byte, error) {
	var all map[string]struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Models map[string]struct {
			ID      string          `json:"id"`
			Name    string          `json:"name"`
			Cost    json.RawMessage `json:"cost"`
			Limit   json.RawMessage `json:"limit"`
			Release string          `json:"release_date"`
		} `json:"models"`
	}
	if err := json.Unmarshal(raw, &all); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	buf.WriteString("{")
	first := true
	for _, pid := range catalogProviders {
		p, ok := all[pid]
		if !ok || len(p.Models) == 0 {
			continue
		}
		models := make([]trimmedModel, 0, len(p.Models))
		for key, m := range p.Models {
			id := m.ID
			if id == "" {
				id = key
			}
			models = append(models, trimmedModel{ID: id, Name: m.Name, Cost: m.Cost, Limit: m.Limit, Release: m.Release})
		}
		sort.SliceStable(models, func(i, j int) bool {
			if models[i].Release != models[j].Release {
				return models[i].Release > models[j].Release
			}
			return models[i].ID < models[j].ID
		})
		if !first {
			buf.WriteString(",")
		}
		first = false
		head, _ := json.Marshal(map[string]string{"id": pid, "name": p.Name})
		buf.WriteString(`"` + pid + `":` + string(head[:len(head)-1]) + `,"models":{`)
		for i, m := range models {
			if i > 0 {
				buf.WriteString(",")
			}
			k, _ := json.Marshal(m.ID)
			v, _ := json.Marshal(m)
			buf.Write(k)
			buf.WriteString(":")
			buf.Write(v)
		}
		buf.WriteString("}}")
	}
	buf.WriteString("}")
	var check map[string]any
	if err := json.Unmarshal(buf.Bytes(), &check); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
