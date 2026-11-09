package envelope

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"testing"
)

func TestSealedProfileAuthenticatesCiphertextAndRecipient(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	bundle := Bundle{Server: "control.example.com", FRPC: "private-token-should-not-be-public", ICE: []byte("private-turn-secret"), CA: []byte("public-ca")}
	sealed, err := Seal(&key.PublicKey, bundle)
	if err != nil {
		t.Fatal(err)
	}
	public, _ := json.Marshal(sealed)
	if bytes.Contains(public, []byte(bundle.FRPC)) || bytes.Contains(public, bundle.ICE) {
		t.Fatal("plaintext secret appeared in public enrollment response")
	}
	got, err := Open(key, sealed)
	if err != nil || got.FRPC != bundle.FRPC {
		t.Fatalf("roundtrip: %v", err)
	}
	bad := sealed
	bad.Data = append([]byte(nil), sealed.Data...)
	bad.Data[len(bad.Data)-1] ^= 1
	if _, err = Open(key, bad); err == nil {
		t.Fatal("corrupt ciphertext accepted")
	}
	other, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = Open(other, sealed); err == nil {
		t.Fatal("another phone decrypted this enrollment")
	}
	bad = sealed
	bad.Nonce = nil
	if _, err = Open(key, bad); err == nil {
		t.Fatal("invalid nonce accepted")
	}
	bad = sealed
	bad.Version = 2
	if _, err = Open(key, bad); err == nil {
		t.Fatal("unknown protocol accepted")
	}
}
