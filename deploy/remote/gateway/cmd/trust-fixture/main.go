// trust-fixture verifies private CI deployments; it is not built into either App.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"remoteconsole.local/connect/envelope"
	"remoteconsole.local/connect/trust"
	"strings"
	"time"
)

func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
func run() error {
	origin := flag.String("origin", "https://control.example.com", "fixture gateway")
	profile := flag.String("profile", "../private/phone-frpc.toml", "fixture profile")
	public := flag.String("public-key", "../owner-enrollment.pub", "fixture phone public key")
	state := flag.String("state", "../private/ci-controller.private.json", "private fixture state")
	address := flag.String("connect-address", "127.0.0.1:443", "loopback HTTPS test proxy")
	mode := flag.String("mode", "approve", "approve, check, or revoke")
	flag.Parse()
	u, e := url.Parse(*origin)
	if e != nil || u.Scheme != "https" || u.User != nil || u.RawQuery != "" {
		return fmt.Errorf("invalid fixture HTTPS origin")
	}
	host, _, e := net.SplitHostPort(*address)
	if e != nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("fixture routing must be loopback")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, target string) (net.Conn, error) {
		if target != u.Hostname()+":443" {
			return nil, fmt.Errorf("fixture origin changed")
		}
		return (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, network, *address)
	}
	client := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	data, e := os.ReadFile(*public)
	if e != nil {
		return e
	}
	pub, e := envelope.ParsePublic(data)
	if e != nil {
		return e
	}
	fp := envelope.Fingerprint(pub)
	data, e = os.ReadFile(*profile)
	if e != nil {
		return e
	}
	key, e := trust.DeriveKey(data, fp)
	if e != nil {
		return e
	}
	call := func(path string, payload any, credential string, owner bool) (int, map[string]any, error) {
		var raw []byte
		method := "GET"
		if payload != nil {
			raw, e = json.Marshal(payload)
			if e != nil {
				return 0, nil, e
			}
			method = "POST"
		}
		request, e := http.NewRequest(method, *origin+"/_remoteconsole/trust/"+path, bytes.NewReader(raw))
		if e != nil {
			return 0, nil, e
		}
		request.Header.Set("Content-Type", "application/json")
		if credential != "" {
			request.Header.Set(trust.ControllerHeader, credential)
		}
		if owner {
			signature, e := trust.Sign(key, "owner", fp, "phone-owner", request, raw, time.Now())
			if e != nil {
				return 0, nil, e
			}
			request.Header.Set(trust.OwnerHeader, fp+":"+signature)
		}
		response, e := client.Do(request)
		if e != nil {
			return 0, nil, e
		}
		defer response.Body.Close()
		var result map[string]any
		e = json.NewDecoder(io.LimitReader(response.Body, 131072)).Decode(&result)
		return response.StatusCode, result, e
	}
	var saved struct {
		Credential string `json:"credential"`
		ID         string `json:"id"`
	}
	if *mode == "approve" {
		code, ticket, e := call("requests", map[string]any{"name": "CI controller", "platform": "fixture", "kind": "native"}, "", false)
		if e != nil {
			return e
		}
		if code != 201 {
			return fmt.Errorf("fixture request: HTTP %d", code)
		}
		code, _, e = call("owner/approve", map[string]any{"request_id": ticket["request_id"], "allow": true}, "", true)
		if e != nil {
			return e
		}
		if code != 200 {
			return fmt.Errorf("fixture approval: HTTP %d", code)
		}
		code, result, e := call("poll", map[string]any{"request_id": ticket["request_id"], "request_secret": ticket["request_secret"]}, "", false)
		if e != nil {
			return e
		}
		if code != 200 {
			return fmt.Errorf("fixture credential: HTTP %d", code)
		}
		saved.Credential, _ = result["credential"].(string)
		controller, _ := result["controller"].(map[string]any)
		saved.ID, _ = controller["id"].(string)
		if saved.Credential == "" || saved.ID == "" {
			return fmt.Errorf("missing credential")
		}
		raw, _ := json.Marshal(saved)
		if e = os.WriteFile(*state, raw, 0600); e != nil {
			return e
		}
		fmt.Println("Phone-owner approval succeeded; credential saved privately.")
		return nil
	}
	raw, e := os.ReadFile(*state)
	if e != nil {
		return e
	}
	if e = json.Unmarshal(raw, &saved); e != nil {
		return e
	}
	if *mode == "revoke" {
		code, _, e := call("owner/revoke", map[string]any{"controller_id": saved.ID}, "", true)
		if e != nil {
			return e
		}
		if code != 200 {
			return fmt.Errorf("fixture revoke: HTTP %d", code)
		}
		code, _, e = call("session", map[string]any{"credential": saved.Credential}, "", false)
		if e != nil {
			return e
		}
		if code != 401 {
			return fmt.Errorf("revoked credential remained valid")
		}
		fmt.Println("Revocation denied further sessions.")
		return nil
	}
	if *mode != "check" {
		return fmt.Errorf("unknown fixture mode")
	}
	code, result, e := call("session", map[string]any{"credential": saved.Credential}, "", false)
	if e != nil {
		return e
	}
	if code != 200 || result["authenticated"] != true {
		return fmt.Errorf("approval was not retained")
	}
	for id, want := range map[string]string{"primary": "fixture-phone-online", "secondary": "fixture-s22-online"} {
		request, _ := http.NewRequest("GET", *origin+"/api/device/list?rc_device="+id, nil)
		request.Header.Set(trust.ControllerHeader, saved.Credential)
		response, e := client.Do(request)
		if e != nil {
			return e
		}
		body, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil {
			return e
		}
		if response.StatusCode != 200 || !strings.Contains(string(body), want) || response.Header.Get("X-Fixture-Delegation-Verified") != "true" {
			return fmt.Errorf("selected phone delegation failed: %s", id)
		}
	}
	fmt.Println("One retained credential reached both independently verified phone identities.")
	return nil
}
