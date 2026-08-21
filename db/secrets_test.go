package db

import (
	"encoding/json"
	"testing"
)

func TestSecretRoundTripAndLegacyCompatibility(t *testing.T) {
	d := &DB{}
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	if err := d.SetSecretKey(key); err != nil {
		t.Fatal(err)
	}
	enc, err := d.ProtectSecret("api-secret")
	if err != nil {
		t.Fatal(err)
	}
	if enc == "api-secret" || len(enc) < len(secretPrefix) || enc[:len(secretPrefix)] != secretPrefix {
		t.Fatalf("unexpected ciphertext %q", enc)
	}
	plain, err := d.RevealSecret(enc)
	if err != nil || plain != "api-secret" {
		t.Fatalf("round trip: %q, %v", plain, err)
	}
	legacy, err := d.RevealSecret("legacy-secret")
	if err != nil || legacy != "legacy-secret" {
		t.Fatalf("legacy value: %q, %v", legacy, err)
	}
}

func TestSecretTamperRejected(t *testing.T) {
	d := &DB{}
	if err := d.SetSecretKey(make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
	enc, err := d.ProtectSecret("value")
	if err != nil {
		t.Fatal(err)
	}
	bad := enc[:len(enc)-1] + "x"
	if _, err := d.RevealSecret(bad); err == nil {
		t.Fatal("tampered ciphertext accepted")
	}
}

func TestMCPEnvProtectRevealAndRedact(t *testing.T) {
	d := &DB{}
	if err := d.SetSecretKey([]byte("12345678901234567890123456789012")); err != nil {
		t.Fatal(err)
	}
	raw := json.RawMessage(`{"X-API-Key":"abc","NODE_ENV":"prod"}`)
	protected, err := d.ProtectMCPEnv(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(protected) == string(raw) {
		t.Fatal("MCP credential was not encrypted")
	}
	plain, err := d.RevealMCPEnv(protected)
	var revealed map[string]string
	if err != nil || json.Unmarshal(plain, &revealed) != nil || revealed["X-API-Key"] != "abc" || revealed["NODE_ENV"] != "prod" {
		t.Fatalf("MCP env round trip: %s, %v", plain, err)
	}
	redacted := RedactMCPEnv(plain)
	var got map[string]string
	if json.Unmarshal(redacted, &got) != nil || got["X-API-Key"] != "***" || got["NODE_ENV"] != "prod" {
		t.Fatalf("unexpected redaction: %s", redacted)
	}
}
