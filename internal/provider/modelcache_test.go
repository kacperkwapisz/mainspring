package provider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

func TestModelCacheRemembersAFailedFetchBriefly(t *testing.T) {
	c := NewModelCache(time.Hour)
	calls := 0
	fail := true
	fetch := func(context.Context) ([]domain.ModelInfo, error) {
		calls++
		if fail {
			return nil, errors.New("down")
		}
		return []domain.ModelInfo{{ID: "m"}}, nil
	}
	for range 3 {
		if got := c.Get(context.Background(), "a", fetch); got != nil {
			t.Fatalf("want nil, got %v", got)
		}
	}
	if calls != 1 {
		t.Fatalf("a failing account was fetched %d times, want 1", calls)
	}

	// Once the failure has aged out, the next Get fetches again.
	c.mu.Lock()
	c.m["a"].until = time.Now().Add(-time.Second)
	c.mu.Unlock()
	fail = false
	if got := c.Get(context.Background(), "a", fetch); len(got) != 1 || calls != 2 {
		t.Fatalf("after the failure window: %v, %d calls", got, calls)
	}
	// A good list is served from the cache.
	if got := c.Get(context.Background(), "a", fetch); len(got) != 1 || calls != 2 {
		t.Fatalf("cached list: %v, %d calls", got, calls)
	}
}
