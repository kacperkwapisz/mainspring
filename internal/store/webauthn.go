package store

import (
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"

	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/kacperkwapisz/mainspring/internal/db"
)

// ErrCredentialExists is returned when a WebAuthn credential ID is already stored.
var ErrCredentialExists = errors.New("credential already added")

// AuthCredential is one WebAuthn credential (security key or passkey) the
// operator registered for panel sign-in.
type AuthCredential struct {
	// ID is the base64url credential ID.
	ID         string
	Name       string
	Kind       string
	RPID       string
	Credential webauthn.Credential
	CreatedAt  int64
	LastUsedAt *int64
}

// WebAuthnStore keeps panel sign-in credentials and the one-time setup code.
type WebAuthnStore struct {
	db *db.DB
}

func NewWebAuthnStore(d *db.DB) *WebAuthnStore { return &WebAuthnStore{db: d} }

// CredentialID encodes a raw WebAuthn credential ID the way rows are keyed.
func CredentialID(raw []byte) string { return base64.RawURLEncoding.EncodeToString(raw) }

// Add stores c. Its ID is derived from c.Credential.ID.
func (s *WebAuthnStore) Add(c AuthCredential) (AuthCredential, error) {
	raw, err := json.Marshal(c.Credential)
	if err != nil {
		return c, err
	}
	c.ID = CredentialID(c.Credential.ID)
	if c.CreatedAt == 0 {
		c.CreatedAt = nowMs()
	}
	if _, ok, err := s.Get(c.ID); err != nil {
		return c, err
	} else if ok {
		return c, ErrCredentialExists
	}
	_, err = s.db.SQL.Exec(
		`INSERT INTO auth_credentials(id, name, kind, rp_id, credential_json, created_at, last_used_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Name, c.Kind, c.RPID, string(raw), c.CreatedAt, c.LastUsedAt,
	)
	return c, err
}

// List returns every credential, oldest first.
func (s *WebAuthnStore) List() ([]AuthCredential, error) {
	return s.query(`SELECT id, name, kind, rp_id, credential_json, created_at, last_used_at FROM auth_credentials ORDER BY created_at, id`)
}

// ListForRP returns the credentials registered for rpID, oldest first.
func (s *WebAuthnStore) ListForRP(rpID string) ([]AuthCredential, error) {
	return s.query(`SELECT id, name, kind, rp_id, credential_json, created_at, last_used_at FROM auth_credentials WHERE rp_id = ? ORDER BY created_at, id`, rpID)
}

// Count returns how many credentials exist.
func (s *WebAuthnStore) Count() (int, error) {
	var n int
	err := s.db.SQL.QueryRow(`SELECT COUNT(*) FROM auth_credentials`).Scan(&n)
	return n, err
}

// Get returns the credential with id.
func (s *WebAuthnStore) Get(id string) (AuthCredential, bool, error) {
	rows, err := s.query(`SELECT id, name, kind, rp_id, credential_json, created_at, last_used_at FROM auth_credentials WHERE id = ?`, id)
	if err != nil || len(rows) == 0 {
		return AuthCredential{}, false, err
	}
	return rows[0], true, nil
}

// NameTaken reports whether another credential (not exceptID) already uses
// name, compared case-insensitively.
func (s *WebAuthnStore) NameTaken(name, exceptID string) (bool, error) {
	var n int
	err := s.db.SQL.QueryRow(`SELECT COUNT(*) FROM auth_credentials WHERE lower(name) = lower(?) AND id != ?`, strings.TrimSpace(name), exceptID).Scan(&n)
	return n > 0, err
}

// Rename sets the display name of id. It reports false when id is unknown.
func (s *WebAuthnStore) Rename(id, name string) (bool, error) {
	res, err := s.db.SQL.Exec(`UPDATE auth_credentials SET name = ? WHERE id = ?`, name, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Touch stores the credential state after a successful sign-in (sign count,
// flags) and sets last_used_at to at.
func (s *WebAuthnStore) Touch(id string, cred webauthn.Credential, at int64) error {
	raw, err := json.Marshal(cred)
	if err != nil {
		return err
	}
	_, err = s.db.SQL.Exec(`UPDATE auth_credentials SET credential_json = ?, last_used_at = ? WHERE id = ?`, string(raw), at, id)
	return err
}

// Delete removes id. It reports false when id is unknown.
func (s *WebAuthnStore) Delete(id string) (bool, error) {
	res, err := s.db.SQL.Exec(`DELETE FROM auth_credentials WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DeleteAll removes every credential and returns how many were removed.
func (s *WebAuthnStore) DeleteAll() (int, error) {
	res, err := s.db.SQL.Exec(`DELETE FROM auth_credentials`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// PutSetupCode replaces the setup code. Only its SHA-256 hash is stored.
// It returns the row's created_at.
func (s *WebAuthnStore) PutSetupCode(code string) (int64, error) {
	at := nowMs()
	_, err := s.db.SQL.Exec(
		`INSERT INTO auth_setup(id, code_hash, created_at) VALUES (1, ?, ?) ON CONFLICT(id) DO UPDATE SET code_hash = excluded.code_hash, created_at = excluded.created_at`,
		hashSetupCode(code), at,
	)
	return at, err
}

// SetupCode reports whether a setup code is pending. hash is the stored
// SHA-256 of the code; it identifies the code without revealing it.
func (s *WebAuthnStore) SetupCode() (hash string, createdAt int64, ok bool, err error) {
	err = s.db.SQL.QueryRow(`SELECT code_hash, created_at FROM auth_setup WHERE id = 1`).Scan(&hash, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, false, nil
	}
	if err != nil {
		return "", 0, false, err
	}
	return hash, createdAt, true, nil
}

// MatchSetupCode reports whether code matches the pending setup code.
func (s *WebAuthnStore) MatchSetupCode(code string) (bool, error) {
	var stored string
	err := s.db.SQL.QueryRow(`SELECT code_hash FROM auth_setup WHERE id = 1`).Scan(&stored)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	got := hashSetupCode(code)
	return subtle.ConstantTimeCompare([]byte(got), []byte(stored)) == 1, nil
}

// ConsumeSetupCode deletes the pending setup code.
func (s *WebAuthnStore) ConsumeSetupCode() error {
	_, err := s.db.SQL.Exec(`DELETE FROM auth_setup WHERE id = 1`)
	return err
}

func hashSetupCode(code string) string {
	sum := sha256.Sum256([]byte(code))
	return hex.EncodeToString(sum[:])
}

func (s *WebAuthnStore) query(q string, args ...any) ([]AuthCredential, error) {
	rows, err := s.db.SQL.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuthCredential{}
	for rows.Next() {
		var c AuthCredential
		var raw string
		var last sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Name, &c.Kind, &c.RPID, &raw, &c.CreatedAt, &last); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(raw), &c.Credential); err != nil {
			return nil, err
		}
		if last.Valid {
			v := last.Int64
			c.LastUsedAt = &v
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
