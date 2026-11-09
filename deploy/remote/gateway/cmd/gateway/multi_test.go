package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func multiFixture(t *testing.T) (*httptest.Server, *http.Client) {
	t.Helper()
	devices := []*gatewayDevice{}
	for _, id := range []string{"primary", "secondary"} {
		id := id
		upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Has(deviceParameter) || r.Header.Get(deviceHeader) != "" {
				t.Error("gateway selector leaked to phone protocol")
			}
			if r.URL.Path == "/api/unlock" {
				http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: id + "-session", Path: "/", HttpOnly: true})
				io.WriteString(w, `{"result":"success"}`)
				return
			}
			if r.URL.Path == "/static/main.js" {
				w.Header().Set("Content-Type", "text/javascript")
				io.WriteString(w, id)
				return
			}
			if r.URL.Path == "/control" {
				w.Header().Set("Content-Type", "text/html")
				io.WriteString(w, `<!doctype html><html><head data-test="yes"><script src="/static/main.js"></script></head><body><script>location.href='/unlock?next=a&b=c';fetch('/api/device/list');</script></body></html>`)
				return
			}
			cookie, err := r.Cookie("auth_token")
			if err != nil || cookie.Value != id+"-session" {
				http.Error(w, "wrong phone session", 401)
				return
			}
			if r.Header.Get("Upgrade") == "websocket" {
				connection, buffer, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				defer connection.Close()
				buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n" + id)
				buffer.Flush()
				return
			}
			io.WriteString(w, id)
		}))
		t.Cleanup(upstream.Close)
		u, _ := url.Parse(upstream.URL)
		devices = append(devices, &gatewayDevice{ID: id, Name: map[string]string{"primary": "Android phone A", "secondary": "Android phone B"}[id], Upstream: u, Payload: []byte(`{"sealed":"` + id + `"}`), Fingerprint: id + "-key"})
	}
	gateway := httptest.NewServer(newMultiHandler(devices, "primary"))
	t.Cleanup(gateway.Close)
	jar, _ := cookiejar.New(nil)
	return gateway, &http.Client{Jar: jar}
}

func TestDirectoryAndSingleURLPortal(t *testing.T) {
	server, client := multiFixture(t)
	r, err := client.Get(server.URL + "/_remoteconsole/devices")
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	var directory struct {
		Version int `json:"registry_version"`
		Devices []struct {
			ID, Name string
			Online   bool
		}
	}
	if json.NewDecoder(r.Body).Decode(&directory) != nil || directory.Version != 1 || len(directory.Devices) != 2 || !directory.Devices[1].Online {
		t.Fatal("missing host directory")
	}
	r, err = client.Get(server.URL + "/control")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if !strings.Contains(string(body), "你的電話，都喺呢度") || strings.Contains(string(body), "sealed") {
		t.Fatal("entry must be a chooser, without enrollment credentials")
	}
}

func TestOneCookieJarCanControlTwoPhonesWithoutCrossingSessions(t *testing.T) {
	server, client := multiFixture(t)
	for _, id := range []string{"primary", "secondary"} {
		r, err := client.Post(server.URL+"/api/unlock?rc_device="+id, "application/json", strings.NewReader(`{"pin":"fixture"}`))
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
	}
	for round := 0; round < 2; round++ {
		for _, id := range []string{"secondary", "primary"} {
			r, err := client.Get(server.URL + "/api/device/list?rc_device=" + id)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := io.ReadAll(r.Body)
			r.Body.Close()
			if r.StatusCode != 200 || string(body) != id {
				t.Fatalf("phone session crossed: %s %d %s", id, r.StatusCode, body)
			}
		}
	}
	// A primary phone cookie must never be forwarded as the S22's cookie.
	request, _ := http.NewRequest("GET", server.URL+"/api/device/list?rc_device=secondary", nil)
	request.AddCookie(&http.Cookie{Name: "auth_token", Value: "primary-session"})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("legacy cookie crossed to another phone")
	}
}

func TestNativeRoutingAndWebSocketStayOnSelectedDevice(t *testing.T) {
	server, client := multiFixture(t)
	for _, id := range []string{"secondary", "primary"} {
		request, _ := http.NewRequest("POST", server.URL+"/api/unlock", strings.NewReader(`{}`))
		request.Header.Set(deviceHeader, id)
		r, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		request, _ = http.NewRequest("GET", server.URL+"/screen/ws", nil)
		request.Header.Set(deviceHeader, id)
		request.Header.Set("Connection", "Upgrade")
		request.Header.Set("Upgrade", "websocket")
		r, err = client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(r.Body)
		r.Body.Close()
		if r.StatusCode != 101 || string(body) != id {
			t.Fatalf("WebSocket selected wrong host: %d %s", r.StatusCode, body)
		}
	}
}

func TestScopedLoginTakesPrecedenceOverStaleLegacyCookie(t *testing.T) {
	server, _ := multiFixture(t)
	request, _ := http.NewRequest("GET", server.URL+"/api/device/list?rc_device=primary", nil)
	// A pre-upgrade browser cookie can survive a phone service restart. The
	// newly paired namespace cookie must win regardless of browser ordering.
	request.AddCookie(&http.Cookie{Name: "auth_token", Value: "stale-before-upgrade"})
	request.AddCookie(&http.Cookie{Name: cookiePrefix("primary") + "auth_token", Value: "primary-session"})
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("fresh pairing shadowed by legacy cookie: HTTP %d", response.StatusCode)
	}
}

func TestSelectorValidationAndFingerprintEnrollment(t *testing.T) {
	server, client := multiFixture(t)
	for _, path := range []string{"/api/device/list?rc_device=unknown", "/api/device/list?rc_device=../secondary", "/api/device/list?rc_device=secondary&rc_device=primary"} {
		r, err := client.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode != 400 && r.StatusCode != 404 {
			t.Fatalf("unsafe route accepted: %s", path)
		}
	}
	request, _ := http.NewRequest("GET", server.URL+"/api/device/list?rc_device=primary", nil)
	request.Header.Set(deviceHeader, "secondary")
	r, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 400 {
		t.Fatal("conflicting selector accepted")
	}
	r, err = client.Get(server.URL + "/_remoteconsole/enroll?key=secondary-key")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(body) != `{"sealed":"secondary"}` {
		t.Fatal("enrollment went to wrong phone")
	}
	r, err = client.Get(server.URL + "/_remoteconsole/enroll?key=unknown")
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 404 {
		t.Fatal("unknown enrollment key fell back to default phone")
	}
}

func TestHTMLAndNestedAssetsRetainDeviceWithoutEditingScriptContents(t *testing.T) {
	server, client := multiFixture(t)
	r, err := client.Get(server.URL + "/control?rc_device=secondary")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(r.Body)
	r.Body.Close()
	text := string(body)
	if !strings.HasPrefix(text, "<!doctype html>") || !strings.Contains(text, `src="/static/main.js?rc_device=secondary"`) || !strings.Contains(text, "class extends OriginalWebSocket") {
		t.Fatal("HTML lost explicit asset/signal routing")
	}
	if !strings.Contains(text, `location.href='/unlock?next=a&b=c'`) {
		t.Fatal("inline JS was edited as an HTML attribute")
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	request, _ := http.NewRequest("GET", server.URL+"/static/main.js", nil)
	request.Header.Set("Referer", server.URL+"/screen/primary?rc_device=secondary")
	r, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	r.Body.Close()
	if r.StatusCode != 307 || r.Header.Get("Location") != "/static/main.js?rc_device=secondary" || r.Header.Get("Cache-Control") != "no-store" {
		t.Fatal("nested assets lost their device or shared a cache key")
	}
	request, _ = http.NewRequest("GET", server.URL+"/static/main.js", nil)
	request.Header.Set("Referer", "https://other.example/control?rc_device=secondary")
	r, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, _ = io.ReadAll(r.Body)
	r.Body.Close()
	if string(body) != "primary" {
		t.Fatal("cross-origin referrer selected a phone")
	}
}
