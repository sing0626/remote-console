package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestOfflineStatusAndEnrollmentStayAvailable(t *testing.T) {
	dead := httptest.NewServer(http.NotFoundHandler())
	u, _ := url.Parse(dead.URL)
	dead.Close()
	h := newHandler([]byte(`{"version":1,"data":"encrypted-only"}`), u)
	for _, path := range []string{"/", "/control", "/screen/primary"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || !strings.Contains(w.Body.String(), "等候電話連線") {
			t.Fatalf("offline page: %d %s", w.Code, path)
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/_remoteconsole/status", nil))
	var state map[string]any
	if json.Unmarshal(w.Body.Bytes(), &state) != nil || state["phone_online"] != false || state["gateway_ready"] != true {
		t.Fatal("incorrect offline status")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/_remoteconsole/enroll", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "encrypted-only") || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("encrypted enrollment unavailable")
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/device/list", nil))
	if w.Code != 503 {
		t.Fatal("offline API should be unavailable")
	}
}
func TestProxyPreservesPublicOriginAndHTTPSAndUpgrades(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Host != "cloudphone.example.com" || r.Header.Get("Origin") != "https://cloudphone.example.com" || r.Header.Get("X-Forwarded-Proto") != "https" || r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("proxy lost public authentication/origin headers")
		}
		if r.Header.Get("Upgrade") == "websocket" {
			conn, buf, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.Close()
			buf.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
			buf.Flush()
			return
		}
		io.WriteString(w, "phone connected")
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	proxy := httptest.NewServer(newHandler([]byte("{}"), u))
	defer proxy.Close()
	for _, upgrade := range []bool{false, true} {
		req, _ := http.NewRequest("GET", proxy.URL+"/control", nil)
		req.Host = "cloudphone.example.com"
		req.Header.Set("Origin", "https://cloudphone.example.com")
		req.Header.Set("X-Forwarded-Proto", "https")
		req.Header.Set("Authorization", "Bearer fixture")
		if upgrade {
			req.Header.Set("Connection", "Upgrade")
			req.Header.Set("Upgrade", "websocket")
		}
		r, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		if upgrade && r.StatusCode != 101 {
			t.Fatal("WebSocket upgrade was lost")
		}
		if !upgrade && r.StatusCode != 200 {
			t.Fatal("phone proxy failed")
		}
		r.Body.Close()
	}
}
