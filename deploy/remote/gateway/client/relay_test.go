package client

import (
	"bytes"
	"os"
	"path/filepath"
	"remoteconsole.local/connect/envelope"
	"strings"
	"testing"
)

func TestRelayRefreshPreservesTunnelIdentityAndDurableCache(t *testing.T) {
	dir, origin := t.TempDir(), "https://control.example.com"
	ice := func(host string) []byte {
		return []byte(`{"turn_urls":["turn:` + host + `:3478?transport=udp"],"turn_shared_secret":"` + strings.Repeat("x", 48) + `"}`)
	}
	b := envelope.Bundle{FRPC: "serverAddr = \"93.184.216.34\"\n", CA: []byte("same-certificate"), ICE: ice("first.example.com")}
	if err := Save(dir, origin, b); err != nil {
		t.Fatal(err)
	}
	b.ICE = ice("second.example.com")
	changed, err := UpdateICE(dir, origin, b)
	if err != nil || !changed || !Cached(dir, origin) {
		t.Fatalf("refresh not durable: %v %v", changed, err)
	}
	for name, expected := range map[string][]byte{"phone-frpc.toml": []byte(b.FRPC), "ca.crt": b.CA, "ice.private.json": b.ICE} {
		actual, _ := os.ReadFile(filepath.Join(dir, name))
		if !bytes.Equal(actual, expected) {
			t.Fatal("unexpected profile bytes", name)
		}
	}
	if changed, err = UpdateICE(dir, origin, b); err != nil || changed {
		t.Fatal("unchanged refresh rewrote cache")
	}
	b.CA = []byte("changed-identity")
	b.ICE = ice("third.example.com")
	if _, err = UpdateICE(dir, origin, b); err == nil {
		t.Fatal("changed tunnel certificate accepted")
	}
	if !Cached(dir, origin) {
		t.Fatal("rejected profile damaged existing cache")
	}
	b.ICE = []byte(`{"turn_shared_secret":"short"}`)
	if _, err = UpdateICE(dir, origin, b); err == nil {
		t.Fatal("invalid relay accepted")
	}
}
