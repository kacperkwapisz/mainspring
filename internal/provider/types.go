package provider

import (
	"context"
	"net/http"

	"github.com/kacperkwapisz/mainspring/internal/domain"
)

type ExecuteOptions struct {
	Stream         bool
	CountTokens    bool
	Compact        bool
	InboundHeaders map[string]string
	CallerKey      string
	// CacheTTL "1h" asks Claude to keep this request's prompt cache for an hour
	// instead of five minutes. The proxy sets it for requests in a session.
	CacheTTL string
}

type ExecuteResult struct {
	OK        bool
	Status    int
	Body      any
	Stream    <-chan any
	Retryable bool
	Message   string
	// Header holds the upstream response headers (rate-limit state lives here).
	Header http.Header
	// Raw is the upstream error body, unsanitised, for limit parsing.
	Raw []byte
}

type Executor interface {
	ID() domain.ProviderID
	Format() domain.ExecutorFormat
	Models() []domain.ModelInfo
	Execute(ctx context.Context, credential domain.Credential, body any, opts ExecuteOptions) (ExecuteResult, error)
	Refresh(ctx context.Context, credential domain.Credential) (domain.Credential, error)
}

// ModelFormatter is optional. When present, Proxy picks the translator format per model
// (OpenCode serves chat, responses, and Anthropic messages from one Credential).
type ModelFormatter interface {
	FormatFor(model string) domain.ExecutorFormat
}

func FormatFor(ex Executor, model string) domain.ExecutorFormat {
	if mf, ok := ex.(ModelFormatter); ok {
		return mf.FormatFor(model)
	}
	return ex.Format()
}

func IsRetryableStatus(status int) bool {
	switch status {
	case 401, 408, 429, 500, 502, 503, 504:
		return true
	default:
		return false
	}
}

// RefreshError is returned when a provider's token endpoint refuses a refresh.
type RefreshError struct {
	Provider domain.ProviderID
	Status   int
}

func (e *RefreshError) Error() string {
	return string(e.Provider) + " refresh failed: " + itoa(e.Status)
}

// Permanent reports whether the refresh token itself was rejected, so the
// account needs a fresh sign-in (as opposed to a network or server hiccup).
func (e *RefreshError) Permanent() bool {
	return e.Status == 400 || e.Status == 401 || e.Status == 403
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
