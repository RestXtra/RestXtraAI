package db

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

const secretPrefix = "enc:v1:"

// DB-local secret key. It is injected by the server from dataDir/secrets.key;
// keeping it out of PostgreSQL means a database dump alone is insufficient to
// recover stored provider credentials.
var secretKeys sync.Map // *DB -> []byte

// SetSecretKey installs the process-local AES-256 key used by credential fields.
func (d *DB) SetSecretKey(key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("secret key must be 32 bytes")
	}
	k := append([]byte(nil), key...)
	secretKeys.Store(d, k)
	return nil
}

func (d *DB) secretKey() []byte {
	if v, ok := secretKeys.Load(d); ok {
		return v.([]byte)
	}
	return nil
}

// ProtectSecret encrypts new values. With no configured key (standalone DB
// unit tests or raw migration tooling), it preserves the historical plaintext
// behavior rather than silently making data unreadable.
func (d *DB) ProtectSecret(value string) (string, error) {
	if value == "" || strings.HasPrefix(value, secretPrefix) {
		return value, nil
	}
	key := d.secretKey()
	if len(key) == 0 {
		return value, nil
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(b)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(value), nil)
	return secretPrefix + base64.RawStdEncoding.EncodeToString(out), nil
}

// RevealSecret decrypts enc:v1 values and transparently accepts legacy
// plaintext values so existing installations can be upgraded without a data
// migration window.
func (d *DB) RevealSecret(value string) (string, error) {
	if value == "" || !strings.HasPrefix(value, secretPrefix) {
		return value, nil
	}
	key := d.secretKey()
	if len(key) == 0 {
		return "", fmt.Errorf("encrypted secret key is not configured")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(value, secretPrefix))
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	b, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(b)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted secret is truncated")
	}
	plain, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plain), nil
}

func (d *DB) GetSecretSetting(key string) (string, bool, error) {
	v, ok, err := d.GetSetting(key)
	if err != nil || !ok {
		return v, ok, err
	}
	plain, err := d.RevealSecret(v)
	return plain, true, err
}

func (d *DB) SetSecretSetting(key, value string) error {
	enc, err := d.ProtectSecret(value)
	if err != nil {
		return err
	}
	return d.SetSetting(key, enc)
}

// ProtectMCPEnv encrypts values whose keys conventionally carry credentials,
// leaving non-sensitive process settings (for example NODE_EXTRA_CA_CERTS)
// readable. The JSON shape is preserved for existing MCP integrations.
func (d *DB) ProtectMCPEnv(raw json.RawMessage) (json.RawMessage, error) {
	return d.transformMCPEnv(raw, true)
}

// RevealMCPEnv decrypts protected MCP credentials and accepts legacy plaintext.
func (d *DB) RevealMCPEnv(raw json.RawMessage) (json.RawMessage, error) {
	return d.transformMCPEnv(raw, false)
}

func RedactMCPEnv(raw json.RawMessage) json.RawMessage {
	var env map[string]string
	if json.Unmarshal(raw, &env) != nil {
		return raw
	}
	for k, v := range env {
		if mcpSecretKey(k) && v != "" {
			env[k] = "***"
		}
	}
	out, err := json.Marshal(env)
	if err != nil {
		return raw
	}
	return out
}

func mcpSecretKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	for _, marker := range []string{"api-key", "apikey", "authorization", "token", "password", "passwd", "secret", "credential"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func (d *DB) transformMCPEnv(raw json.RawMessage, protect bool) (json.RawMessage, error) {
	if len(raw) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var env map[string]string
	if err := json.Unmarshal(raw, &env); err != nil {
		return raw, err
	}
	for k, v := range env {
		if !mcpSecretKey(k) || v == "" {
			continue
		}
		var err error
		if protect {
			env[k], err = d.ProtectSecret(v)
		} else {
			env[k], err = d.RevealSecret(v)
		}
		if err != nil {
			return nil, err
		}
	}
	return json.Marshal(env)
}
