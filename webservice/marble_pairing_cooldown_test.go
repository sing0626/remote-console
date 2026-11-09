package webservice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func pairingRequest(t *testing.T, wm *WebMaster, pin, address string) (int, map[string]any) {
	t.Helper()
	r := httptest.NewRequest("POST", "/api/unlock", strings.NewReader(`{"pin":"`+pin+`"}`))
	r.RemoteAddr = address
	w := httptest.NewRecorder()
	wm.router.ServeHTTP(w, r)
	var body map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	return w.Code, body
}

func TestMarblePairingCooldownBeginsOnFifthFailure(t *testing.T) {
	wm := testMaster(t)
	for attempt := 1; attempt <= 5; attempt++ {
		status, body := pairingRequest(t, wm, "000000", "192.0.2.4:1234")
		if body["leftTries"] != float64(5-attempt) {
			t.Fatalf("incorrect attempts remaining: %v", body)
		}
		if attempt < 5 && status != http.StatusOK {
			t.Fatal("locked before fifth failure")
		}
		if attempt == 5 && (status != http.StatusTooManyRequests || body["retryAfterSeconds"] != float64(600)) {
			t.Fatal("fifth failure did not start cooldown immediately")
		}
	}
	until := wm.UnlockAttemptRecords["192.0.2.4"].LockUntil
	status, _ := pairingRequest(t, wm, "246810", "192.0.2.4:1234")
	if status != 429 || wm.UnlockAttemptRecords["192.0.2.4"].LockUntil != until {
		t.Fatal("blocked attempt changed deadline or bypassed lock")
	}
	status, body := pairingRequest(t, wm, "246810", "192.0.2.5:1234")
	if status != 200 || body["result"] != "success" {
		t.Fatal("one client's lockout blocked another client")
	}
}

func TestMarbleExpiredPairingWindowsReset(t *testing.T) {
	for _, locked := range []bool{false, true} {
		wm := testMaster(t)
		wm.UnlockAttemptRecords["192.0.2.4"] = UnlockAttemptRecord{Attempts: 5, IsLocked: locked, LockUntil: time.Now().Add(-time.Minute)}
		status, body := pairingRequest(t, wm, "246810", "192.0.2.4:1234")
		if status != 200 || body["result"] != "success" {
			t.Fatalf("expired lock was renewed (IsLocked=%v)", locked)
		}
	}
	wm := testMaster(t)
	wm.UnlockAttemptRecords["192.0.2.4"] = UnlockAttemptRecord{Attempts: 4, LockUntil: time.Now().Add(-time.Minute)}
	_, body := pairingRequest(t, wm, "000000", "192.0.2.4:1234")
	if body["leftTries"] != float64(4) {
		t.Fatal("expired failure window kept old attempts")
	}
}
