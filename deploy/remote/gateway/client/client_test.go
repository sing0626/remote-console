package client

import (
	"bytes"
	"context"
	"net/http"
	"os"
	"path/filepath"
	"remoteconsole.local/connect/envelope"
	"testing"
)

func TestIdentityPersistsAndProfileCacheDetectsPartialImport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.key")
	first, err := Identity(path)
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Identity(path)
	if err != nil || first.N.Cmp(second.N) != 0 {
		t.Fatal("identity was replaced")
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(before, after) {
		t.Fatal("identity bytes changed")
	}
	b := envelope.Bundle{FRPC: "serverAddr = \"93.184.216.34\"\n", ICE: []byte("fixture-ice"), CA: []byte("fixture-ca")}
	profile := filepath.Join(dir, "remote")
	origin := "https://control.example.com"
	if err = Save(profile, origin, b); err != nil {
		t.Fatal(err)
	}
	if !Cached(profile, origin) {
		t.Fatal("complete import not cached")
	}
	if Cached(profile, "https://another.example.com") {
		t.Fatal("profile reused for another server")
	}
	if err = os.WriteFile(filepath.Join(profile, "ice.private.json"), []byte("modified"), 0600); err != nil {
		t.Fatal(err)
	}
	if Cached(profile, origin) {
		t.Fatal("corrupt or partial profile accepted")
	}
}
func TestEnrollmentURLRejectsDowngradeAndCredentialLeaks(t *testing.T) {
	for _, raw := range []string{"http://control.example.com", "https://user:secret@control.example.com", "https://control.example.com/?token=secret", "https://control.example.com/path", "https://control.example.com:7000"} {
		if _, err := ServerURL(raw); err == nil {
			t.Errorf("unsafe origin accepted: %s", raw)
		}
	}
	if _, err := ServerURL("https://control.example.com"); err != nil {
		t.Fatal(err)
	}
}

func TestPublicIPRoutingRetainsTLSVerification(t *testing.T) {
	for _, address := range []string{"127.0.0.1", "10.0.0.1", "::1", "invalid"} {
		if _, err := TransportForServer("https://control.example.com", address); err == nil {
			t.Errorf("invalid bootstrap address accepted: %s", address)
		}
	}
	roundTripper, err := TransportForServer("https://control.example.com", "93.184.216.34")
	if err != nil {
		t.Fatal(err)
	}
	transport := roundTripper.(*http.Transport)
	if transport.TLSClientConfig != nil && transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("IP routing weakened TLS verification")
	}
	if transport.Proxy != nil {
		t.Fatal("explicit server routing must not use an ambient proxy")
	}
	if _, err = transport.DialContext(context.Background(), "tcp", "different.example.com:443"); err == nil {
		t.Fatal("server origin changed")
	}
}
