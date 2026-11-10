// Package auth signs the operator into the panel with WebAuthn (security keys
// first, passkeys second), or trusts a reverse proxy that already did.
package auth

import (
	"crypto/rand"
	"encoding/base64"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	fcrypto "github.com/kacperkwapisz/mainspring/internal/crypto"
	"github.com/kacperkwapisz/mainspring/internal/env"
	"github.com/kacperkwapisz/mainspring/internal/session"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

// Settings keys.
const (
	SettingUserHandle   = "auth.userHandle"
	SettingSessionEpoch = "auth.sessionEpoch"
	SettingRequireKey   = "auth.requireKey"
)

// Credential kinds.
const (
	KindSecurityKey = "security-key"
	KindPasskey     = "passkey"
)

const (
	// CeremonyCookie holds the id of the in-flight WebAuthn ceremony.
	CeremonyCookie = "ms_wa"
	ceremonyTTL    = 5 * time.Minute

	setupWindow   = 10 * time.Minute
	setupMaxWrong = 10

	maxNameLen = 64
)

// Error is a user-facing failure with an HTTP status and a stable code.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func fail(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// Service holds panel sign-in state. It implements the panel Gate.
type Service struct {
	Mode       string
	TrustProxy bool
	PublicURL  string
	Creds      *store.WebAuthnStore
	Settings   *store.SettingsStore

	key []byte

	mu         sync.Mutex
	ceremonies map[string]ceremony
	wrong      []time.Time
	now        func() time.Time
}

// New builds a Service from the environment and stores.
func New(e *env.Env, creds *store.WebAuthnStore, settings *store.SettingsStore) *Service {
	return &Service{
		Mode:       e.AuthMode,
		TrustProxy: e.TrustProxy,
		PublicURL:  e.PublicURL,
		Creds:      creds,
		Settings:   settings,
		key:        e.JWTKey,
		ceremonies: map[string]ceremony{},
		now:        time.Now,
	}
}

// Authed reports whether r may use the panel. In proxy mode the reverse proxy
// already authenticated the operator unless auth.requireKey is on.
func (s *Service) Authed(r *http.Request) bool {
	if !s.KeyRequired() {
		return true
	}
	return s.SessionAuthed(r)
}

// SessionAuthed reports whether r carries a valid panel session cookie.
func (s *Service) SessionAuthed(r *http.Request) bool {
	epoch, err := s.Epoch()
	if err != nil {
		return false
	}
	return session.Read(r, s.key, epoch)
}

// KeyRequired reports whether the panel needs a WebAuthn session.
func (s *Service) KeyRequired() bool {
	return s.Mode != env.AuthProxy || s.RequireKey()
}

// RequireKey reports whether auth.requireKey is on.
func (s *Service) RequireKey() bool {
	v, _ := s.Settings.Get(SettingRequireKey)
	return v == "1"
}

// SetRequireKey turns auth.requireKey on or off. Turning it on needs proxy
// mode, a secure context and at least one key for this host.
func (s *Service) SetRequireKey(r *http.Request, on bool) error {
	if s.Mode != env.AuthProxy {
		return fail(http.StatusConflict, "not_proxy_mode", "Requiring a key only applies when Mainspring runs behind a sign-in proxy")
	}
	if !on {
		return s.Settings.Set(SettingRequireKey, "0")
	}
	rp := s.RelyingParty(r)
	if !rp.SecureContext {
		return fail(http.StatusBadRequest, "insecure_context", "Security keys need HTTPS or localhost")
	}
	creds, err := s.Creds.ListForRP(rp.ID)
	if err != nil {
		return err
	}
	if len(creds) == 0 {
		return fail(http.StatusConflict, "no_keys_for_host", "Add a key for this address first")
	}
	return s.Settings.Set(SettingRequireKey, "1")
}

// Epoch returns the session epoch, creating it on first use.
func (s *Service) Epoch() (string, error) {
	if v, ok := s.Settings.Get(SettingSessionEpoch); ok && v != "" {
		return v, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.Settings.Get(SettingSessionEpoch); ok && v != "" {
		return v, nil
	}
	v := fcrypto.RandomID(12)
	return v, s.Settings.Set(SettingSessionEpoch, v)
}

// RotateEpoch invalidates every session cookie issued so far.
func (s *Service) RotateEpoch() error {
	return s.Settings.Set(SettingSessionEpoch, fcrypto.RandomID(12))
}

// IssueSession returns a Set-Cookie value for a new panel session.
func (s *Service) IssueSession(r *http.Request) (string, error) {
	epoch, err := s.Epoch()
	if err != nil {
		return "", err
	}
	return session.Issue(s.key, epoch, s.RelyingParty(r).Secure), nil
}

// ClearSession returns a Set-Cookie value that ends the panel session.
func (s *Service) ClearSession(r *http.Request) string {
	return session.Clear(s.RelyingParty(r).Secure)
}

// UserHandle returns the operator's fixed WebAuthn user handle, creating it on
// first use.
func (s *Service) UserHandle() ([]byte, error) {
	if v, ok := s.Settings.Get(SettingUserHandle); ok {
		if b, err := base64.RawURLEncoding.DecodeString(v); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if v, ok := s.Settings.Get(SettingUserHandle); ok {
		if b, err := base64.RawURLEncoding.DecodeString(v); err == nil && len(b) == 32 {
			return b, nil
		}
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return b, s.Settings.Set(SettingUserHandle, base64.RawURLEncoding.EncodeToString(b))
}

// RelyingParty describes the WebAuthn relying party for one request.
type RelyingParty struct {
	// ID is the request host without port.
	ID string
	// Origin is scheme://host[:port].
	Origin string
	// Secure is true when the origin is https.
	Secure bool
	// SecureContext is true when browsers offer WebAuthn here.
	SecureContext bool
}

// RelyingParty derives the RP ID and origin from r. X-Forwarded-Host and
// X-Forwarded-Proto count only with MAINSPRING_TRUST_PROXY=1 or in proxy mode.
func (s *Service) RelyingParty(r *http.Request) RelyingParty {
	trust := s.TrustProxy || s.Mode == env.AuthProxy
	host := r.Host
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if trust {
		if v := firstHeader(r, "X-Forwarded-Host"); v != "" {
			host = v
		}
		if v := strings.ToLower(firstHeader(r, "X-Forwarded-Proto")); v == "http" || v == "https" {
			scheme = v
		}
	}
	host = strings.ToLower(strings.TrimSpace(host))
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	} else {
		hostname = strings.TrimSuffix(strings.TrimPrefix(hostname, "["), "]")
	}
	secure := scheme == "https"
	local := hostname == "localhost" || hostname == "127.0.0.1" || hostname == "::1"
	return RelyingParty{
		ID:            hostname,
		Origin:        scheme + "://" + host,
		Secure:        secure,
		SecureContext: hostname != "" && (secure || local),
	}
}

// HTTPSURL returns PUBLIC_URL when it is https, else "".
func (s *Service) HTTPSURL() string {
	if strings.HasPrefix(s.PublicURL, "https://") {
		return s.PublicURL
	}
	return ""
}

func firstHeader(r *http.Request, name string) string {
	v := r.Header.Get(name)
	if i := strings.IndexByte(v, ','); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// SetupNeeded reports whether the first key must be added with a setup code.
func (s *Service) SetupNeeded() (bool, error) {
	if !s.KeyRequired() {
		return false, nil
	}
	n, err := s.Creds.Count()
	return n == 0, err
}

// EnsureSetupCode creates a fresh setup code when no credentials exist and
// returns it; otherwise it returns "".
func (s *Service) EnsureSetupCode() (string, error) {
	n, err := s.Creds.Count()
	if err != nil || n > 0 {
		return "", err
	}
	return s.NewSetupCode()
}

// NewSetupCode replaces the setup code with a fresh one and returns it.
func (s *Service) NewSetupCode() (string, error) {
	code := GenerateSetupCode()
	_, err := s.Creds.PutSetupCode(code)
	return code, err
}

// Reset removes every credential, signs every session out and returns a fresh
// setup code.
func (s *Service) Reset() (code string, removed int, err error) {
	if removed, err = s.Creds.DeleteAll(); err != nil {
		return "", 0, err
	}
	if err = s.RotateEpoch(); err != nil {
		return "", removed, err
	}
	code, err = s.NewSetupCode()
	return code, removed, err
}

// VerifySetupCode checks code and returns a Set-Cookie value for ms_setup.
func (s *Service) VerifySetupCode(r *http.Request, code string) (string, error) {
	n, err := s.Creds.Count()
	if err != nil {
		return "", err
	}
	if n > 0 {
		return "", fail(http.StatusConflict, "setup_not_needed", "A key is already added. Sign in with it")
	}
	if !s.allowSetupAttempt() {
		return "", fail(http.StatusTooManyRequests, "setup_code_rate_limited", "Too many wrong codes. Wait a few minutes and try again")
	}
	ok, err := s.Creds.MatchSetupCode(NormalizeSetupCode(code))
	if err != nil {
		return "", err
	}
	hash, _, pending, err := s.Creds.SetupCode()
	if err != nil {
		return "", err
	}
	if !ok || !pending {
		s.recordWrongSetup()
		return "", fail(http.StatusBadRequest, "setup_code_wrong", "That code doesn't match the one in hub's terminal")
	}
	return session.IssueSetup(s.key, s.setupBinding(hash), s.RelyingParty(r).Secure), nil
}

// SetupAuthed reports whether r carries a valid ms_setup cookie for the
// pending setup code while no credentials exist.
func (s *Service) SetupAuthed(r *http.Request) bool {
	hash, _, pending, err := s.Creds.SetupCode()
	if err != nil || !pending {
		return false
	}
	if n, err := s.Creds.Count(); err != nil || n > 0 {
		return false
	}
	return session.ReadSetup(r, s.key, s.setupBinding(hash))
}

// setupBinding ties an ms_setup cookie to one setup code without putting the
// code's hash in the cookie.
func (s *Service) setupBinding(hash string) string {
	return fcrypto.HMACSign(s.key, "setup-bind:"+hash)[:22]
}

func (s *Service) allowSetupAttempt() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	cutoff := s.now().Add(-setupWindow)
	kept := s.wrong[:0]
	for _, t := range s.wrong {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	s.wrong = kept
	return len(s.wrong) < setupMaxWrong
}

func (s *Service) recordWrongSetup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wrong = append(s.wrong, s.now())
}

// setupAlphabet leaves out 0, O, 1, I and L.
const setupAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// GenerateSetupCode returns a random XXXX-XXXX code.
func GenerateSetupCode() string {
	out := make([]byte, 0, 9)
	for i := 0; i < 8; i++ {
		if i == 4 {
			out = append(out, '-')
		}
		out = append(out, setupAlphabet[randIndex(len(setupAlphabet))])
	}
	return string(out)
}

func randIndex(n int) int {
	// Rejection sampling keeps the distribution uniform.
	limit := 256 - 256%n
	var b [1]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			panic(err)
		}
		if int(b[0]) < limit {
			return int(b[0]) % n
		}
	}
}

// NormalizeSetupCode uppercases code, drops spaces and dashes and puts the
// dash back, so "k7qm 2xpf" matches "K7QM-2XPF".
func NormalizeSetupCode(code string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(code) {
		if r == '-' || r == ' ' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	if len(s) == 8 {
		return s[:4] + "-" + s[4:]
	}
	return s
}

// ValidName trims name and checks it is 1–64 characters.
func ValidName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > maxNameLen {
		return "", fail(http.StatusBadRequest, "name_invalid", "Name the key in 1 to 64 characters")
	}
	return name, nil
}
