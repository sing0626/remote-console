package webservice

import (
	"github.com/golang-jwt/jwt/v5"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

func testMaster(t *testing.T) *WebMaster {
	t.Helper()
	var files fs.FS = fstest.MapFS{"static/ok.txt": {Data: []byte("ok")}, "marble-unlock.html": {Data: []byte("pair")}, "control.html": {Data: []byte("control")}}
	wm := New(WebMasterConfig{}, files)
	wm.SetPIN("246810")
	wm.setRouter()
	return wm
}
func TestMarbleProtectedRoutesAndOrigin(t *testing.T) {
	wm := testMaster(t)
	for _, path := range []string{"/control", "/api/device/list", "/screen/ws", "/api/screen/sessions", "/api/ice-config"} {
		r := httptest.NewRequest("GET", path, nil)
		w := httptest.NewRecorder()
		wm.router.ServeHTTP(w, r)
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s returned %d", path, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/unlock", strings.NewReader(`{"pin":"246810"}`))
	r.Header.Set("Origin", "https://attacker.invalid")
	w := httptest.NewRecorder()
	wm.router.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Fatal("foreign origin accepted")
	}
	r = httptest.NewRequest("GET", "/control", nil)
	r.Header.Set("Accept", "text/html")
	w = httptest.NewRecorder()
	wm.router.ServeHTTP(w, r)
	if w.Code != 302 || strings.Contains(w.Body.String(), "control") {
		t.Fatal("redirect continued into protected handler")
	}
}
func TestMarblePairingAndLockout(t *testing.T) {
	wm := testMaster(t)
	for i := 0; i < 5; i++ {
		r := httptest.NewRequest("POST", "/api/unlock", strings.NewReader(`{"pin":"000000"}`))
		w := httptest.NewRecorder()
		wm.router.ServeHTTP(w, r)
	}
	r := httptest.NewRequest("POST", "/api/unlock", strings.NewReader(`{"pin":"246810"}`))
	w := httptest.NewRecorder()
	wm.router.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "Too many attempts") {
		t.Fatal("PIN lockout bypassed")
	}
	wm = testMaster(t)
	r = httptest.NewRequest("POST", "/api/unlock", strings.NewReader(`{"pin":"246810"}`))
	w = httptest.NewRecorder()
	wm.router.ServeHTTP(w, r)
	if len(w.Result().Cookies()) != 1 {
		t.Fatal("pairing did not create cookie")
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie flags missing")
	}
	r = httptest.NewRequest("GET", "/control", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	wm.router.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "control") {
		t.Fatal("valid paired session rejected")
	}
}
func TestMarbleTokensExpireAndRotate(t *testing.T) {
	a, b := testMaster(t), testMaster(t)
	token, err := a.GenerateToken()
	if err != nil {
		t.Fatal(err)
	}
	if !a.validateToken(token) || b.validateToken(token) {
		t.Fatal("token restart isolation failed")
	}
	claims := &CustomClaims{Role: "admin", RegisteredClaims: jwt.RegisteredClaims{Issuer: "webscreen", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}
	expired, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.jwtSecret)
	if a.validateToken(expired) {
		t.Fatal("expired token accepted")
	}
	claims.ExpiresAt = nil
	missing, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(a.jwtSecret)
	if a.validateToken(missing) {
		t.Fatal("token without expiry accepted")
	}
	if a.validateToken(token + "corrupt") {
		t.Fatal("forged token accepted")
	}
}
