package sub

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// RateLimited means the provider's usage endpoint refused the read for now
// (429). It says nothing about the account's own limits.
type RateLimited struct {
	Provider   string
	RetryAfter time.Duration // 0 when the provider didn't say
}

func (e *RateLimited) Error() string {
	return fmt.Sprintf("%s is limiting how often limits can be read", e.Provider)
}

// HTTPError turns a failed usage response into an error, keeping 429's
// Retry-After.
func HTTPError(provider string, res *http.Response) error {
	if res.StatusCode == http.StatusTooManyRequests {
		return &RateLimited{Provider: provider, RetryAfter: retryAfter(res.Header.Get("Retry-After"))}
	}
	return fmt.Errorf("%s usage endpoint answered %d", strings.ToLower(provider), res.StatusCode)
}

func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}
