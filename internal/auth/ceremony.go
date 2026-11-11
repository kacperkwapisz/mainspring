package auth

import (
	"errors"
	"net/http"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	fcrypto "github.com/kacperkwapisz/mainspring/internal/crypto"
	"github.com/kacperkwapisz/mainspring/internal/session"
	"github.com/kacperkwapisz/mainspring/internal/store"
)

const (
	ceremonyRegister = "register"
	ceremonyLogin    = "login"
)

// ceremony is the server-side half of a WebAuthn begin/finish pair.
type ceremony struct {
	purpose   string
	data      webauthn.SessionData
	rpID      string
	origin    string
	kind      string
	name      string
	fromSetup bool
	expires   int64
}

// operator is the single WebAuthn user.
type operator struct {
	id    []byte
	creds []webauthn.Credential
}

func (o operator) WebAuthnID() []byte                         { return o.id }
func (o operator) WebAuthnName() string                       { return "operator" }
func (o operator) WebAuthnDisplayName() string                { return "Mainspring operator" }
func (o operator) WebAuthnCredentials() []webauthn.Credential { return o.creds }

func (s *Service) relyingParty(rp RelyingParty) (*webauthn.WebAuthn, error) {
	return webauthn.New(&webauthn.Config{
		RPID:          rp.ID,
		RPDisplayName: "Mainspring",
		RPOrigins:     []string{rp.Origin},
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			UserVerification: protocol.VerificationPreferred,
		},
		Timeouts: webauthn.TimeoutsConfig{
			Login:        webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL},
			Registration: webauthn.TimeoutConfig{Enforce: true, Timeout: ceremonyTTL, TimeoutUVD: ceremonyTTL},
		},
	})
}

func (s *Service) operatorFor(rpID string) (operator, []store.AuthCredential, error) {
	id, err := s.UserHandle()
	if err != nil {
		return operator{}, nil, err
	}
	rows, err := s.Creds.ListForRP(rpID)
	if err != nil {
		return operator{}, nil, err
	}
	creds := make([]webauthn.Credential, 0, len(rows))
	for _, row := range rows {
		creds = append(creds, row.Credential)
	}
	return operator{id: id, creds: creds}, rows, nil
}

func descriptors(creds []webauthn.Credential) []protocol.CredentialDescriptor {
	out := make([]protocol.CredentialDescriptor, 0, len(creds))
	for i := range creds {
		out = append(out, creds[i].Descriptor())
	}
	return out
}

func (s *Service) usableRP(r *http.Request) (RelyingParty, error) {
	rp := s.RelyingParty(r)
	if !rp.SecureContext {
		return rp, fail(http.StatusBadRequest, "insecure_context", "Security keys need HTTPS or localhost")
	}
	return rp, nil
}

// CanRegister reports whether r may add a key and whether it does so through
// the setup code. With zero keys, a key is needed only through setup, except
// in proxy mode without auth.requireKey, where the proxy already vouched.
func (s *Service) CanRegister(r *http.Request) (allowed, fromSetup bool, err error) {
	n, err := s.Creds.Count()
	if err != nil {
		return false, false, err
	}
	if n == 0 {
		if s.SetupAuthed(r) {
			return true, true, nil
		}
		return !s.KeyRequiredFor(r), false, nil
	}
	return s.Authed(r), false, nil
}

// BeginRegistration starts adding a key of kind named name. It returns the
// options for navigator.credentials.create and a Set-Cookie for ms_wa.
func (s *Service) BeginRegistration(r *http.Request, kind, name string, fromSetup bool) (*protocol.PublicKeyCredentialCreationOptions, string, error) {
	rp, err := s.usableRP(r)
	if err != nil {
		return nil, "", err
	}
	if kind != KindSecurityKey && kind != KindPasskey {
		return nil, "", fail(http.StatusBadRequest, "kind_invalid", "Kind must be security-key or passkey")
	}
	if name, err = ValidName(name); err != nil {
		return nil, "", err
	}
	if taken, err := s.Creds.NameTaken(name, ""); err != nil {
		return nil, "", err
	} else if taken {
		return nil, "", fail(http.StatusBadRequest, "name_taken", "Another key already has that name")
	}
	wa, err := s.relyingParty(rp)
	if err != nil {
		return nil, "", err
	}
	user, _, err := s.operatorFor(rp.ID)
	if err != nil {
		return nil, "", err
	}
	opts := []webauthn.RegistrationOption{webauthn.WithExclusions(descriptors(user.creds))}
	if kind == KindSecurityKey {
		// Non-discoverable, so it doesn't use one of the key's passkey slots.
		opts = append(opts,
			webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
				AuthenticatorAttachment: protocol.CrossPlatform,
				ResidentKey:             protocol.ResidentKeyRequirementDiscouraged,
				RequireResidentKey:      protocol.ResidentKeyNotRequired(),
				UserVerification:        protocol.VerificationPreferred,
			}),
			webauthn.WithPublicKeyCredentialHints([]protocol.PublicKeyCredentialHints{protocol.PublicKeyCredentialHintSecurityKey}),
		)
	} else {
		opts = append(opts, webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationPreferred,
		}))
	}
	creation, data, err := wa.BeginRegistration(user, opts...)
	if err != nil {
		return nil, "", err
	}
	cookie := s.putCeremony(rp, ceremony{purpose: ceremonyRegister, data: *data, kind: kind, name: name, fromSetup: fromSetup})
	return &creation.Response, cookie, nil
}

// FinishRegistration verifies the attestation in body and stores the key.
// fromSetup reports whether this consumed the setup code.
func (s *Service) FinishRegistration(r *http.Request, body []byte) (store.AuthCredential, bool, error) {
	c, rp, err := s.takeCeremony(r, ceremonyRegister)
	if err != nil {
		return store.AuthCredential{}, false, err
	}
	if c.fromSetup {
		if !s.SetupAuthed(r) {
			return store.AuthCredential{}, false, fail(http.StatusUnauthorized, "setup_expired", "Enter the setup code again")
		}
	} else if allowed, _, err := s.CanRegister(r); err != nil {
		return store.AuthCredential{}, false, err
	} else if !allowed {
		return store.AuthCredential{}, false, fail(http.StatusUnauthorized, "panel_signed_out", "Sign in to the panel first")
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(body)
	if err != nil {
		return store.AuthCredential{}, false, fail(http.StatusBadRequest, "key_response_invalid", "The browser sent a key response Mainspring can't read")
	}
	wa, err := s.relyingParty(rp)
	if err != nil {
		return store.AuthCredential{}, false, err
	}
	user, _, err := s.operatorFor(rp.ID)
	if err != nil {
		return store.AuthCredential{}, false, err
	}
	cred, err := wa.CreateCredential(user, c.data, parsed)
	if err != nil {
		return store.AuthCredential{}, false, fail(http.StatusBadRequest, "key_check_failed", "The key's answer didn't check out. Try again")
	}
	if taken, err := s.Creds.NameTaken(c.name, ""); err != nil {
		return store.AuthCredential{}, false, err
	} else if taken {
		return store.AuthCredential{}, false, fail(http.StatusBadRequest, "name_taken", "Another key already has that name")
	}
	saved, err := s.Creds.Add(store.AuthCredential{Name: c.name, Kind: c.kind, RPID: rp.ID, Credential: *cred})
	if errors.Is(err, store.ErrCredentialExists) {
		return store.AuthCredential{}, false, fail(http.StatusConflict, "key_already_added", "That key is already added")
	}
	if err != nil {
		return store.AuthCredential{}, false, err
	}
	if c.fromSetup {
		if err := s.Creds.ConsumeSetupCode(); err != nil {
			return saved, true, err
		}
	}
	return saved, c.fromSetup, nil
}

// BeginLogin starts a sign-in with any key registered for this host. It
// returns the options for navigator.credentials.get and a Set-Cookie for ms_wa.
func (s *Service) BeginLogin(r *http.Request) (*protocol.PublicKeyCredentialRequestOptions, string, error) {
	rp, err := s.usableRP(r)
	if err != nil {
		return nil, "", err
	}
	user, _, err := s.operatorFor(rp.ID)
	if err != nil {
		return nil, "", err
	}
	if len(user.creds) == 0 {
		return nil, "", fail(http.StatusConflict, "no_keys_for_host", "No keys are added for this address")
	}
	wa, err := s.relyingParty(rp)
	if err != nil {
		return nil, "", err
	}
	// Listing the keys lets non-discoverable security keys answer without a username.
	assertion, data, err := wa.BeginLogin(user,
		webauthn.WithAllowedCredentials(descriptors(user.creds)),
		webauthn.WithUserVerification(protocol.VerificationPreferred),
	)
	if err != nil {
		return nil, "", err
	}
	cookie := s.putCeremony(rp, ceremony{purpose: ceremonyLogin, data: *data})
	return &assertion.Response, cookie, nil
}

// FinishLogin verifies the assertion in body and records the key's use.
func (s *Service) FinishLogin(r *http.Request, body []byte) (store.AuthCredential, error) {
	c, rp, err := s.takeCeremony(r, ceremonyLogin)
	if err != nil {
		return store.AuthCredential{}, err
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(body)
	if err != nil {
		return store.AuthCredential{}, fail(http.StatusBadRequest, "key_response_invalid", "The browser sent a key response Mainspring can't read")
	}
	user, rows, err := s.operatorFor(rp.ID)
	if err != nil {
		return store.AuthCredential{}, err
	}
	id := store.CredentialID(parsed.RawID)
	var row *store.AuthCredential
	for i := range rows {
		if rows[i].ID == id {
			row = &rows[i]
			break
		}
	}
	if row == nil {
		return store.AuthCredential{}, fail(http.StatusUnauthorized, "key_not_added", "That key isn't added to this Mainspring")
	}
	wa, err := s.relyingParty(rp)
	if err != nil {
		return store.AuthCredential{}, err
	}
	cred, err := wa.ValidateLogin(user, c.data, parsed)
	if err != nil {
		var unknown *protocol.ErrorUnknownCredential
		if errors.As(err, &unknown) {
			return store.AuthCredential{}, fail(http.StatusUnauthorized, "key_not_added", "That key isn't added to this Mainspring")
		}
		return store.AuthCredential{}, fail(http.StatusUnauthorized, "key_check_failed", "The key's answer didn't check out. Try again")
	}
	if cred.Authenticator.CloneWarning {
		return store.AuthCredential{}, fail(http.StatusUnauthorized, "key_clone_warning", "This key's counter went backwards, so it may have been copied. Sign in with another key")
	}
	at := s.now().UnixMilli()
	if err := s.Creds.Touch(row.ID, *cred, at); err != nil {
		return store.AuthCredential{}, err
	}
	row.Credential = *cred
	row.LastUsedAt = &at
	return *row, nil
}

// ClearCeremony returns a Set-Cookie value that drops ms_wa.
func (s *Service) ClearCeremony(r *http.Request) string {
	return session.ClearNamed(CeremonyCookie, s.RelyingParty(r).Secure)
}

func (s *Service) putCeremony(rp RelyingParty, c ceremony) string {
	id := fcrypto.RandomID(24)
	now := s.now()
	c.rpID = rp.ID
	c.origin = rp.Origin
	c.expires = now.Add(ceremonyTTL).UnixMilli()
	s.mu.Lock()
	for k, v := range s.ceremonies {
		if v.expires < now.UnixMilli() {
			delete(s.ceremonies, k)
		}
	}
	s.ceremonies[id] = c
	s.mu.Unlock()
	return session.Set(CeremonyCookie, id, ceremonyTTL, rp.Secure)
}

func (s *Service) takeCeremony(r *http.Request, purpose string) (ceremony, RelyingParty, error) {
	rp, err := s.usableRP(r)
	if err != nil {
		return ceremony{}, rp, err
	}
	id := session.Cookie(r, CeremonyCookie)
	s.mu.Lock()
	c, ok := s.ceremonies[id]
	if ok {
		delete(s.ceremonies, id)
	}
	s.mu.Unlock()
	if !ok || c.purpose != purpose || c.expires < s.now().UnixMilli() || c.rpID != rp.ID || c.origin != rp.Origin {
		return ceremony{}, rp, fail(http.StatusBadRequest, "ceremony_expired", "That request timed out. Try again")
	}
	return c, rp, nil
}
