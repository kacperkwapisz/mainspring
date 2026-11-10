// Package authtest provides a software WebAuthn authenticator for tests. It
// makes ECDSA P-256 credentials with "none" attestation, the way a browser
// relays a real key's answer.
package authtest

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/go-webauthn/webauthn/protocol/webauthncbor"
	"github.com/go-webauthn/webauthn/protocol/webauthncose"
)

const (
	flagUP = 0x01
	flagUV = 0x04
	flagAT = 0x40
)

var b64 = base64.RawURLEncoding

// SoftKey is a software authenticator holding one credential.
type SoftKey struct {
	ID      []byte
	Priv    *ecdsa.PrivateKey
	Origin  string
	Counter uint32
}

// New returns a SoftKey that answers as if the page were served from origin.
func New(origin string) *SoftKey {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		panic(err)
	}
	id := make([]byte, 32)
	if _, err := rand.Read(id); err != nil {
		panic(err)
	}
	return &SoftKey{ID: id, Priv: priv, Origin: origin}
}

type creationOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RP        struct {
			ID string `json:"id"`
		} `json:"rp"`
	} `json:"publicKey"`
}

type requestOptions struct {
	PublicKey struct {
		Challenge string `json:"challenge"`
		RPID      string `json:"rpId"`
	} `json:"publicKey"`
}

// Create answers a {"publicKey": PublicKeyCredentialCreationOptions} body
// with the JSON the browser would post back after navigator.credentials.create.
func (k *SoftKey) Create(options []byte) ([]byte, error) {
	var opts creationOptions
	if err := json.Unmarshal(options, &opts); err != nil {
		return nil, err
	}
	if opts.PublicKey.Challenge == "" || opts.PublicKey.RP.ID == "" {
		return nil, fmt.Errorf("authtest: options missing challenge or rp.id: %s", options)
	}
	clientData, _ := json.Marshal(map[string]any{
		"type":        "webauthn.create",
		"challenge":   opts.PublicKey.Challenge,
		"origin":      k.Origin,
		"crossOrigin": false,
	})
	pub, err := k.Priv.PublicKey.ECDH()
	if err != nil {
		return nil, err
	}
	raw := pub.Bytes() // 0x04 || X || Y
	cose, err := webauthncbor.Marshal(webauthncose.EC2PublicKeyData{
		PublicKeyData: webauthncose.PublicKeyData{KeyType: int64(webauthncose.EllipticKey), Algorithm: int64(webauthncose.AlgES256)},
		Curve:         1,
		XCoord:        raw[1:33],
		YCoord:        raw[33:65],
	})
	if err != nil {
		return nil, err
	}
	authData := k.authHeader(opts.PublicKey.RP.ID, flagUP|flagUV|flagAT)
	authData = append(authData, make([]byte, 16)...) // AAGUID
	authData = binary.BigEndian.AppendUint16(authData, uint16(len(k.ID)))
	authData = append(authData, k.ID...)
	authData = append(authData, cose...)
	attObj, err := webauthncbor.Marshal(map[string]any{
		"fmt":      "none",
		"attStmt":  map[string]any{},
		"authData": authData,
	})
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id":                      b64.EncodeToString(k.ID),
		"rawId":                   b64.EncodeToString(k.ID),
		"type":                    "public-key",
		"authenticatorAttachment": "cross-platform",
		"clientExtensionResults":  map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"attestationObject": b64.EncodeToString(attObj),
			"transports":        []string{"usb"},
		},
	})
}

// Get answers a {"publicKey": PublicKeyCredentialRequestOptions} body with the
// JSON the browser would post back after navigator.credentials.get. Each call
// bumps the signature counter.
func (k *SoftKey) Get(options []byte) ([]byte, error) {
	var opts requestOptions
	if err := json.Unmarshal(options, &opts); err != nil {
		return nil, err
	}
	if opts.PublicKey.Challenge == "" || opts.PublicKey.RPID == "" {
		return nil, fmt.Errorf("authtest: options missing challenge or rpId: %s", options)
	}
	clientData, _ := json.Marshal(map[string]any{
		"type":        "webauthn.get",
		"challenge":   opts.PublicKey.Challenge,
		"origin":      k.Origin,
		"crossOrigin": false,
	})
	k.Counter++
	authData := k.authHeader(opts.PublicKey.RPID, flagUP|flagUV)
	clientHash := sha256.Sum256(clientData)
	digest := sha256.Sum256(append(append([]byte{}, authData...), clientHash[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.Priv, digest[:])
	if err != nil {
		return nil, err
	}
	return json.Marshal(map[string]any{
		"id":                      b64.EncodeToString(k.ID),
		"rawId":                   b64.EncodeToString(k.ID),
		"type":                    "public-key",
		"authenticatorAttachment": "cross-platform",
		"clientExtensionResults":  map[string]any{},
		"response": map[string]any{
			"clientDataJSON":    b64.EncodeToString(clientData),
			"authenticatorData": b64.EncodeToString(authData),
			"signature":         b64.EncodeToString(sig),
		},
	})
}

func (k *SoftKey) authHeader(rpID string, flags byte) []byte {
	rpHash := sha256.Sum256([]byte(rpID))
	out := append([]byte{}, rpHash[:]...)
	out = append(out, flags)
	return binary.BigEndian.AppendUint32(out, k.Counter)
}
