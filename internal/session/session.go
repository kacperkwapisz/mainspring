package session

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	fcrypto "github.com/kacperkwapisz/mainspring/internal/crypto"
)

const (
	CookieName = "ms_session"
	ttl        = 7 * 24 * time.Hour
)

func Issue(key []byte, secure bool) string {
	exp := time.Now().Add(ttl).UnixMilli()
	payload := strconv.FormatInt(exp, 10)
	sig := fcrypto.HMACSign(key, payload)
	value := payload + "." + sig
	parts := []string{
		CookieName + "=" + value,
		"Path=/",
		"HttpOnly",
		"SameSite=Lax",
		"Max-Age=" + strconv.Itoa(int(ttl.Seconds())),
	}
	if secure {
		parts = append(parts, "Secure")
	}
	return strings.Join(parts, "; ")
}

func Clear(secure bool) string {
	parts := []string{CookieName + "=", "Path=/", "HttpOnly", "SameSite=Lax", "Max-Age=0"}
	if secure {
		parts = append(parts, "Secure")
	}
	return strings.Join(parts, "; ")
}

func Read(r *http.Request, key []byte) bool {
	cookie := r.Header.Get("Cookie")
	var value string
	for _, part := range strings.Split(cookie, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, CookieName+"=") {
			value = part[len(CookieName)+1:]
			break
		}
	}
	if value == "" {
		return false
	}
	dot := strings.IndexByte(value, '.')
	if dot < 0 {
		return false
	}
	payload := value[:dot]
	sig := value[dot+1:]
	exp, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || exp < time.Now().UnixMilli() {
		return false
	}
	return fcrypto.HMACVerify(key, payload, sig)
}

func BearerPassword(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return ""
	}
	token := strings.TrimSpace(h[7:])
	if strings.HasPrefix(token, "sk-ms-") {
		return ""
	}
	return token
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
