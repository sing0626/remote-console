package webservice

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"remoteconsole.local/connect/trust"
	"strings"
	"testing"
	"time"
)

func TestGatewayDelegationCannotBecomeAnOwnerApproval(t *testing.T) {
	wm := testMaster(t)
	key := bytes.Repeat([]byte{9}, 32)
	fp := strings.Repeat("b", 64)
	if e := wm.ConfigureGatewayTrust("https://control.example.com", fp, key, nil); e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("GET", "https://control.example.com/api/ice-config", nil)
	token, e := trust.Sign(key, "delegate", fp, "controller", request, nil, time.Now())
	if e != nil {
		t.Fatal(e)
	}
	request.Header.Set(trust.DelegationHeader, token)
	response := httptest.NewRecorder()
	wm.router.ServeHTTP(response, request)
	if response.Code != 200 {
		t.Fatal("valid delegated controller rejected")
	}
	response = httptest.NewRecorder()
	wm.router.ServeHTTP(response, request)
	if response.Code != 401 {
		t.Fatal("replayed delegation accepted")
	}
	request = httptest.NewRequest("GET", "https://control.example.com/api/gateway-trust/list", nil)
	token, _ = trust.Sign(key, "delegate", fp, "controller", request, nil, time.Now())
	request.Header.Set(trust.DelegationHeader, token)
	response = httptest.NewRecorder()
	wm.router.ServeHTTP(response, request)
	if response.Code != 403 {
		t.Fatal("delegated controller elevated itself to a phone-owner approval")
	}
}

func TestReceiverOwnerBridgeSignsRequestsWithoutExposingIdentity(t *testing.T) {
	key := bytes.Repeat([]byte{4}, 32)
	fp := strings.Repeat("c", 64)
	gateway := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		header := strings.SplitN(r.Header.Get(trust.OwnerHeader), ":", 2)
		if len(header) != 2 || header[0] != fp {
			t.Error("owner identity missing")
			w.WriteHeader(401)
			return
		}
		var verifier trust.Verifier
		if subject, e := verifier.Verify(key, "owner", fp, header[1], r, raw, time.Now()); e != nil || subject != "phone-owner" {
			t.Error("owner request not correctly signed")
			w.WriteHeader(401)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"controllers": []any{}, "requests": []any{}})
	}))
	defer gateway.Close()
	wm := testMaster(t)
	if e := wm.ConfigureGatewayTrust(gateway.URL, fp, key, gateway.Client().Transport); e != nil {
		t.Fatal(e)
	}
	token, e := wm.GenerateToken()
	if e != nil {
		t.Fatal(e)
	}
	request := httptest.NewRequest("GET", "http://127.0.0.1:8079/api/gateway-trust/list", nil)
	request.Header.Set("Authorization", "Bearer "+token)
	response := httptest.NewRecorder()
	wm.router.ServeHTTP(response, request)
	if response.Code != 200 || bytes.Contains(response.Body.Bytes(), key) || strings.Contains(response.Body.String(), fp) {
		t.Fatal("owner bridge failed or exposed phone identity")
	}
}
