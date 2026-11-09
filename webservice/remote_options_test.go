package webservice

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMarbleFrameRateValidation(t *testing.T) {
	for _, fps := range []string{"30", "60", "120", ""} {
		config := map[string]string{"max_fps": fps}
		if err := validateStreamOptions(config); err != nil {
			t.Fatal(err)
		}
	}
	for _, fps := range []string{"0", "999", "-1", "60;sh"} {
		if validateStreamOptions(map[string]string{"max_fps": fps}) == nil {
			t.Fatal("invalid FPS accepted", fps)
		}
	}
	if validateStreamOptions(map[string]string{"max_size": "99999"}) == nil {
		t.Fatal("unbounded dimensions")
	}
}
func TestMarbleTemporaryTURNConfiguration(t *testing.T) {
	secret := strings.Repeat("private", 8)
	config := &ICEConfiguration{STUN: []string{"stun:test.example:3478"}, TURN: []string{"turn:test.example:3478?transport=udp"}, Secret: secret}
	servers := config.Servers(time.Unix(1800000000, 0))
	if len(servers) != 2 || servers[1].Username != "1800003600:remote" {
		t.Fatal("TURN credential deadline invalid")
	}
	mac := hmac.New(sha1.New, []byte(secret))
	mac.Write([]byte(servers[1].Username))
	if servers[1].Credential != base64.StdEncoding.EncodeToString(mac.Sum(nil)) {
		t.Fatal("coturn REST HMAC mismatch")
	}
	encoded, _ := json.Marshal(servers)
	if strings.Contains(string(encoded), secret) {
		t.Fatal("master TURN secret exposed")
	}
}
func TestMarbleForwardedSecureCookieOnlyFromLoopback(t *testing.T) {
	r := httptest.NewRequest("POST", "http://control.example/api/unlock", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.RemoteAddr = "192.168.1.20:1234"
	if secureCookieRequest(r) {
		t.Fatal("untrusted forwarded scheme accepted")
	}
	r.RemoteAddr = "127.0.0.1:1234"
	if !secureCookieRequest(r) {
		t.Fatal("HTTPS tunnel cookie missing Secure")
	}
}
