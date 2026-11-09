package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"remoteconsole.local/connect/trust"
	"strings"
	"testing"
	"time"
)

func TestOneControllerApprovalCoversBothPhonesAndRevokesLiveSocket(t *testing.T) {
	key := bytes.Repeat([]byte{7}, 32)
	fp := strings.Repeat("a", 64)
	storePath := filepath.Join(t.TempDir(), "controllers.json")
	phoneServer := func(key []byte, fp, label string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.Header.Get("Cookie"), trustCookie) || r.Header.Get(trust.ControllerHeader) != "" {
				t.Error("gateway credential leaked to phone")
			}
			var verifier trust.Verifier
			if _, e := verifier.Verify(key, "delegate", fp, r.Header.Get(trust.DelegationHeader), r, nil, time.Now()); e != nil {
				t.Error(e)
				w.WriteHeader(401)
				return
			}
			if r.Header.Get("Upgrade") == "websocket" {
				c, rw, e := w.(http.Hijacker).Hijack()
				if e != nil {
					t.Error(e)
					return
				}
				defer c.Close()
				rw.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
				rw.Flush()
				io.Copy(io.Discard, c)
				return
			}
			io.WriteString(w, label)
		}))
	}
	phone := phoneServer(key, fp, "primary")
	defer phone.Close()
	secondKey := bytes.Repeat([]byte{8}, 32)
	secondFP := strings.Repeat("b", 64)
	secondPhone := phoneServer(secondKey, secondFP, "secondary")
	defer secondPhone.Close()
	firstURL, _ := url.Parse(phone.URL)
	secondURL, _ := url.Parse(secondPhone.URL)
	devices := []*gatewayDevice{{ID: "primary", Name: "Phone A", Fingerprint: fp, AuthKey: key, Upstream: firstURL}, {ID: "secondary", Name: "Phone B", Fingerprint: secondFP, AuthKey: secondKey, Upstream: secondURL}}
	gateway := newMultiHandler(devices, "primary").(*deviceGateway)
	var e error
	gateway.auth, e = newControllerAuthority(storePath, devices)
	if e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer(gateway)
	defer server.Close()
	post := func(path string, data any, owner bool) *http.Response {
		raw, _ := json.Marshal(data)
		request, _ := http.NewRequest("POST", server.URL+trustPrefix+path, bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		if owner {
			signature, e := trust.Sign(key, "owner", fp, "phone-owner", request, raw, time.Now())
			if e != nil {
				t.Fatal(e)
			}
			request.Header.Set(trust.OwnerHeader, fp+":"+signature)
		}
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		return response
	}
	response := post("requests", map[string]any{"name": "My iPhone", "platform": "ios", "kind": "native"}, false)
	var ticket trust.Ticket
	json.NewDecoder(response.Body).Decode(&ticket)
	response.Body.Close()
	if response.StatusCode != 201 || ticket.Secret == "" {
		t.Fatal("approval request missing")
	}
	response = post("owner/approve", map[string]any{"request_id": ticket.ID, "allow": true}, false)
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("unauthenticated approval accepted")
	}
	response = post("owner/approve", map[string]any{"request_id": ticket.ID, "allow": true}, true)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("phone approval rejected")
	}
	response = post("poll", map[string]any{"request_id": ticket.ID, "request_secret": ticket.Secret}, false)
	var approval struct {
		Credential string           `json:"credential"`
		Controller trust.Controller `json:"controller"`
	}
	json.NewDecoder(response.Body).Decode(&approval)
	response.Body.Close()
	if approval.Credential == "" {
		t.Fatal("credential missing")
	}
	for _, id := range []string{"primary", "secondary"} {
		request, _ := http.NewRequest("GET", server.URL+"/api/device/list?rc_device="+id, nil)
		request.Header.Set(trust.ControllerHeader, approval.Credential)
		response, e = http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		body, _ := io.ReadAll(response.Body)
		response.Body.Close()
		if response.StatusCode != 200 || string(body) != id {
			t.Fatal("same approval did not cover the selected independent phone")
		}

	}
	request, _ := http.NewRequest("GET", server.URL+"/screen/ws?rc_device=secondary", nil)
	request.Header.Set(trust.ControllerHeader, approval.Credential)
	request.Header.Set("Connection", "Upgrade")
	request.Header.Set("Upgrade", "websocket")
	socketResponse, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer socketResponse.Body.Close()
	if socketResponse.StatusCode != 101 {
		t.Fatal("stream did not upgrade")
	}
	// Reopening the persistent authority must retain approval without a PIN.
	reopened, e := trust.Open(storePath)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := reopened.Authenticate(approval.Credential); !ok {
		t.Fatal("controller did not survive restart")
	}
	response = post("owner/revoke", map[string]any{"controller_id": approval.Controller.ID}, true)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("revocation failed")
	}
	closed := make(chan error, 1)
	go func() { b := make([]byte, 1); _, e := socketResponse.Body.Read(b); closed <- e }()
	select {
	case e := <-closed:
		if e == nil {
			t.Fatal("stream stayed usable after revocation")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("revocation did not close live stream")
	}
	request, _ = http.NewRequest("GET", server.URL+"/api/device/list?rc_device=primary", nil)
	request.Header.Set(trust.ControllerHeader, approval.Credential)
	response, e = http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	response.Body.Close()
	if response.StatusCode != 401 {
		t.Fatal("revoked controller made another request")
	}
}
