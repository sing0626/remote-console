package trust

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestApprovalPersistenceDenialExpiryAndRevocation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "controllers.json")
	s, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	s.now = func() time.Time { return now }
	ticket, e := s.Request("My iPhone", "ios", "native")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Poll(ticket.ID, ticket.Secret); e != ErrPending {
		t.Fatal("approval was not required")
	}
	if _, e = s.Poll(ticket.ID, ticket.Secret); e != ErrSlow {
		t.Fatal("polling limit was not applied")
	}
	if _, e = s.Poll(ticket.ID, "wrong"); e != ErrExpired {
		t.Fatal("another controller polled the request")
	}
	controller, e := s.Approve(ticket.ID, true)
	if e != nil {
		t.Fatal(e)
	}
	result, e := s.Poll(ticket.ID, ticket.Secret)
	if e != nil || result.Credential == "" {
		t.Fatal("approved credential missing")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(raw, []byte(result.Credential)) || bytes.Contains(raw, []byte(strings.Split(result.Credential, ".")[1])) || bytes.Contains(raw, []byte(ticket.Secret)) {
		t.Fatal("plaintext credential persisted")
	}
	restored, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if c, ok := restored.Authenticate(result.Credential); !ok || c.ID != controller.ID {
		t.Fatal("restart lost controller approval")
	}
	if _, ok := restored.Authenticate(result.Credential + "x"); ok {
		t.Fatal("modified credential accepted")
	}
	if e = restored.Revoke(controller.ID); e != nil {
		t.Fatal(e)
	}
	again, e := Open(path)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := again.Authenticate(result.Credential); ok {
		t.Fatal("revocation lost after restart")
	}
	denied, _ := s.Request("Other", "browser", "browser")
	s.Approve(denied.ID, false)
	if _, e = s.Poll(denied.ID, denied.Secret); e != ErrDenied {
		t.Fatal("denied request was approved")
	}
	expired, _ := s.Request("Expired", "android", "native")
	now = now.Add(301 * time.Second)
	if _, e = s.Poll(expired.ID, expired.Secret); e != ErrExpired {
		t.Fatal("expired request survived")
	}
	os.WriteFile(path, []byte(`{"version":99}`), 0600)
	if _, e = Open(path); e == nil {
		t.Fatal("unknown trust store silently reset")
	}
}

func TestSignaturesBindPurposePhoneMethodPathBodyExpiryAndNonce(t *testing.T) {
	fp := strings.Repeat("a", 64)
	key, e := DeriveKey([]byte("auth.token = \""+strings.Repeat("x", 40)+"\"\n"), fp)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	request := httptest.NewRequest("POST", "https://control.example.com/api/action?mode=1", nil)
	body := []byte(`{"value":1}`)
	token, e := Sign(key, "delegate", fp, "controller", request, body, now)
	if e != nil {
		t.Fatal(e)
	}
	var v Verifier
	if subject, e := v.Verify(key, "delegate", fp, token, request, body, now); e != nil || subject != "controller" {
		t.Fatal("valid delegation rejected")
	}
	if _, e = v.Verify(key, "delegate", fp, token, request, body, now); e == nil {
		t.Fatal("replay accepted")
	}
	for _, test := range []struct {
		purpose, fp        string
		requestURL, method string
		body               []byte
		at                 time.Time
	}{
		{"owner", fp, request.URL.String(), "POST", body, now}, {"delegate", strings.Repeat("b", 64), request.URL.String(), "POST", body, now},
		{"delegate", fp, "https://control.example.com/api/other?mode=1", "POST", body, now}, {"delegate", fp, request.URL.String(), "GET", body, now},
		{"delegate", fp, request.URL.String(), "POST", []byte(`{"value":2}`), now}, {"delegate", fp, request.URL.String(), "POST", body, now.Add(time.Minute)},
	} {
		var verifier Verifier
		r := httptest.NewRequest(test.method, test.requestURL, nil)
		if _, e = verifier.Verify(key, test.purpose, test.fp, token, r, test.body, test.at); e == nil {
			t.Fatal("unbound authorization accepted")
		}
	}
}
