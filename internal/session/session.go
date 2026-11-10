package session

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	fcrypto "github.com/kacperkwapisz/mainspring/internal/crypto"
)

const (
	// CookieName holds the panel session.
	CookieName = "ms_session"
	// SetupCookieName lets the bearer register the first key after entering the setup code.
	SetupCookieName = "ms_setup"
	ttl             = 7 * 24 * time.Hour
	setupTTL        = 15 * time.Minute
	setupPurpose    = "setup:"
)

// Issue returns a Set-Cookie value for a panel session bound to epoch.
// The signed payload is "exp.epoch", so rotating the epoch invalidates every
// cookie issued before.
func Issue(key []byte, epoch string, secure bool) string {
	return issue(CookieName, "", key, epoch, ttl, secure)
}

// Read reports whether r carries a valid, unexpired session cookie for epoch.
func Read(r *http.Request, key []byte, epoch string) bool {
	return read(r, CookieName, "", key, epoch)
}

// IssueSetup returns a Set-Cookie value for a short-lived setup cookie bound to
// bind (the current setup code's identity), so it dies with the code.
func IssueSetup(key []byte, bind string, secure bool) string {
	return issue(SetupCookieName, setupPurpose, key, bind, setupTTL, secure)
}

// ReadSetup reports whether r carries a valid setup cookie for bind.
func ReadSetup(r *http.Request, key []byte, bind string) bool {
	return read(r, SetupCookieName, setupPurpose, key, bind)
}

// Clear expires the session cookie.
func Clear(secure bool) string { return ClearNamed(CookieName, secure) }

// ClearNamed expires the named cookie.
func ClearNamed(name string, secure bool) string {
	parts := []string{name + "=", "Path=/", "HttpOnly", "SameSite=Lax", "Max-Age=0"}
	if secure {
		parts = append(parts, "Secure")
	}
	return strings.Join(parts, "; ")
}

// Cookie returns the raw value of the named cookie on r, or "".
func Cookie(r *http.Request, name string) string {
	c, err := r.Cookie(name)
	if err != nil {
		return ""
	}
	return c.Value
}

// Set returns a Set-Cookie value for an opaque value with the given TTL.
func Set(name, value string, maxAge time.Duration, secure bool) string {
	parts := []string{
		name + "=" + value,
		"Path=/",
		"HttpOnly",
		"SameSite=Lax",
		"Max-Age=" + strconv.Itoa(int(maxAge.Seconds())),
	}
	if secure {
		parts = append(parts, "Secure")
	}
	return strings.Join(parts, "; ")
}

func issue(name, purpose string, key []byte, epoch string, life time.Duration, secure bool) string {
	exp := time.Now().Add(life).UnixMilli()
	payload := strconv.FormatInt(exp, 10) + "." + epoch
	sig := fcrypto.HMACSign(key, purpose+payload)
	return Set(name, payload+"."+sig, life, secure)
}

func read(r *http.Request, name, purpose string, key []byte, epoch string) bool {
	if epoch == "" {
		return false
	}
	value := Cookie(r, name)
	if value == "" {
		return false
	}
	last := strings.LastIndexByte(value, '.')
	if last < 0 {
		return false
	}
	payload, sig := value[:last], value[last+1:]
	dot := strings.IndexByte(payload, '.')
	if dot < 0 {
		return false
	}
	exp, err := strconv.ParseInt(payload[:dot], 10, 64)
	if err != nil || exp < time.Now().UnixMilli() {
		return false
	}
	if payload[dot+1:] != epoch {
		return false
	}
	return fcrypto.HMACVerify(key, purpose+payload, sig)
}

// BearerLocalKey returns the LocalKey from Authorization: Bearer, or from the
// x-api-key header Anthropic SDKs send when ANTHROPIC_API_KEY is set.
func BearerLocalKey(r *http.Request) string {
	if k := strings.TrimSpace(r.Header.Get("x-api-key")); strings.HasPrefix(k, "sk-ms-") {
		return k
	}
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	token := strings.TrimSpace(h[7:])
	if strings.HasPrefix(token, "sk-ms-") {
		return token
	}
	return ""
}
