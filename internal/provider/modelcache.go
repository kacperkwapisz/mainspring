package provider

import (
	"context"
	"sync"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

// ModelCache keeps each account's model list from its provider. A list older
// than the TTL is still served while a fresh one is fetched in the background,
// so a tool asking for /v1/models never waits on the provider twice.
type ModelCache struct {
	mu  sync.Mutex
	ttl time.Duration
	m   map[string]*modelEntry
}

type modelEntry struct {
	models     []domain.ModelInfo
	until      time.Time
	refreshing bool
}

func NewModelCache(ttl time.Duration) *ModelCache {
	return &ModelCache{ttl: ttl, m: map[string]*modelEntry{}}
}

// Get returns the cached list for key, fetching it when there is none yet.
// A failed fetch keeps the last good list.
func (c *ModelCache) Get(ctx context.Context, key string, fetch func(context.Context) ([]domain.ModelInfo, error)) []domain.ModelInfo {
	c.mu.Lock()
	e := c.m[key]
	if e != nil && len(e.models) > 0 {
		models := e.models
		if time.Now().After(e.until) && !e.refreshing {
			e.refreshing = true
			go c.refresh(key, fetch)
		}
		c.mu.Unlock()
		return models
	}
	c.mu.Unlock()
	models, err := fetch(ctx)
	if err != nil || len(models) == 0 {
		return nil
	}
	c.store(key, models)
	return models
}

func (c *ModelCache) refresh(key string, fetch func(context.Context) ([]domain.ModelInfo, error)) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	models, err := fetch(ctx)
	c.mu.Lock()
	defer c.mu.Unlock()
	e := c.m[key]
	if e == nil {
		return
	}
	e.refreshing = false
	if err == nil && len(models) > 0 {
		e.models, e.until = models, time.Now().Add(c.ttl)
	} else {
		e.until = time.Now().Add(c.ttl / 4) // try again sooner after a failure
	}
}

func (c *ModelCache) store(key string, models []domain.ModelInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.m[key] = &modelEntry{models: models, until: time.Now().Add(c.ttl)}
}

// WithCatalog fills in what a provider's own list leaves out (prices, a
// display name) from the models.dev catalog.
func WithCatalog(p domain.ProviderID, models []domain.ModelInfo) []domain.ModelInfo {
	rows, ok := catalogPack(providerCatalog[p])
	if !ok {
		return models
	}
	byID := map[string]catalogModel{}
	for _, r := range rows {
		byID[r.ID] = r
	}
	for i, m := range models {
		r, ok := byID[m.ID]
		if !ok {
			continue
		}
		if m.Cost == nil {
			models[i].Cost = r.Cost
		}
		if m.Name == "" {
			models[i].Name, models[i].DisplayName = r.Name, r.Name
		}
	}
	return models
}
