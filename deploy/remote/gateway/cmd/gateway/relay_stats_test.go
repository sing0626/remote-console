package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestRelayReportsAuthenticationPersistenceAndWarnings(t *testing.T) {
	dir := t.TempDir()
	key := sha256.Sum256([]byte("synthetic-report-key"))
	cfg, _ := json.Marshal([]relayNode{{ID: "hk", Name: "Hong Kong", Key: hex.EncodeToString(key[:])}})
	os.WriteFile(filepath.Join(dir, "relay-nodes.json"), cfg, 0600)
	m, e := loadRelayMonitor(dir)
	if e != nil {
		t.Fatal(e)
	}
	value := relayReport{ID: "hk", At: time.Now().Unix(), QuotaGB: 1, Count: "tx", Month: relayUsage{TX: 960000000}, Active: true}
	body, _ := json.Marshal(value)
	send := func(sig string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/_remoteconsole/relays/report", strings.NewReader(string(body)))
		r.Header.Set("X-HalfCloud-Time", strconv.FormatInt(value.At, 10))
		r.Header.Set("X-HalfCloud-Signature", sig)
		w := httptest.NewRecorder()
		m.receive(w, r)
		return w
	}
	if w := send("00"); w.Code != 401 {
		t.Fatal(w.Code)
	}
	digest := sha256.Sum256(body)
	mac := hmac.New(sha256.New, key[:])
	fmt.Fprintf(mac, "%d\n%x", value.At, digest)
	sig := hex.EncodeToString(mac.Sum(nil))
	if w := send(sig); w.Code != 204 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := send(sig); w.Code != 409 {
		t.Fatal("replay", w.Code)
	}
	m, e = loadRelayMonitor(dir)
	if e != nil {
		t.Fatal(e)
	}
	w := httptest.NewRecorder()
	m.status(w, httptest.NewRequest("GET", "/", nil))
	text := w.Body.String()
	if !strings.Contains(text, "95%") || strings.Contains(text, hex.EncodeToString(key[:])) || strings.Contains(text, `"key"`) {
		t.Fatal(text)
	}
	gateway := newMultiHandler(nil, "").(*deviceGateway)
	gateway.relays = m
	w = httptest.NewRecorder()
	gateway.ServeHTTP(w, httptest.NewRequest("GET", "/_remoteconsole/relays/status", nil))
	if w.Code != 401 {
		t.Fatal("unauthorized stats", w.Code)
	}
}
