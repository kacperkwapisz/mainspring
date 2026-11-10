package crypto

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func goldenPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "golden", name)
}

func TestCryptoGoldens(t *testing.T) {
	raw, err := os.ReadFile(goldenPath(t, "crypto.json"))
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		JWTSecret          string `json:"jwtSecret"`
		JWTKeyHex          string `json:"jwtKeyHex"`
		SHA256SkMs         string `json:"sha256SkMs"`
		HMACExp            string `json:"hmacExp"`
		NonHexSecretKeyHex string `json:"nonHexSecretKeyHex"`
		EncryptDecrypt     struct {
			Plain string `json:"plain"`
		} `json:"encryptDecrypt"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	key := SecretToKey(g.JWTSecret)
	if hex.EncodeToString(key) != g.JWTKeyHex {
		t.Fatalf("hex key %s want %s", hex.EncodeToString(key), g.JWTKeyHex)
	}
	if SHA256Hex("sk-ms-"+"abababababababababababababababababababababababab") != g.SHA256SkMs {
		t.Fatalf("sha %s", SHA256Hex("sk-ms-"+"abababababababababababababababababababababababab"))
	}
	if HMACSign(key, "1710000000000") != g.HMACExp {
		t.Fatalf("hmac %s want %s", HMACSign(key, "1710000000000"), g.HMACExp)
	}
	blob, err := Encrypt(key, g.EncryptDecrypt.Plain)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decrypt(key, blob)
	if err != nil || got != g.EncryptDecrypt.Plain {
		t.Fatalf("roundtrip %q %v", got, err)
	}
	nonHex := SecretToKey("xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx")
	if hex.EncodeToString(nonHex) != g.NonHexSecretKeyHex {
		t.Fatalf("nonhex %s", hex.EncodeToString(nonHex))
	}
}
