package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"remoteconsole.local/connect/trust"
	"strings"
	"sync"
	"time"
)

const trustPrefix = "/_remoteconsole/trust/"
const trustCookie = "rc_controller_v1"
const pairingCookie = "rc_controller_pairing_v1"

type controllerAuthority struct {
	store         *trust.Store
	keys          map[string][]byte
	ownerVerifier trust.Verifier
	mu            sync.Mutex
	connections   map[string]map[net.Conn]bool
}

func newControllerAuthority(file string, devices []*gatewayDevice) (*controllerAuthority, error) {
	store, e := trust.Open(file)
	if e != nil {
		return nil, e
	}
	a := &controllerAuthority{store: store, keys: map[string][]byte{}, connections: map[string]map[net.Conn]bool{}}
	for _, d := range devices {
		if len(d.AuthKey) != 32 {
			return nil, errors.New("phone delegation identity unavailable")
		}
		a.keys[d.Fingerprint] = d.AuthKey
	}
	return a, nil
}
func authJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func authCookie(w http.ResponseWriter, r *http.Request, name, value string, seconds int) {
	c := &http.Cookie{Name: name, Value: value, Path: "/", MaxAge: seconds, HttpOnly: true, SameSite: http.SameSiteStrictMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"}
	if seconds > 0 {
		c.Expires = time.Now().Add(time.Duration(seconds) * time.Second)
	}
	http.SetCookie(w, c)
}
func (a *controllerAuthority) credential(r *http.Request) (string, bool) {
	if values, ok := r.Header[http.CanonicalHeaderKey(trust.ControllerHeader)]; ok {
		if len(values) != 1 {
			return "", true
		}
		return values[0], true
	}
	if c, e := r.Cookie(trustCookie); e == nil {
		return c.Value, true
	}
	return "", false
}
func (a *controllerAuthority) identity(r *http.Request) (trust.Controller, bool, bool) {
	credential, present := a.credential(r)
	c, ok := a.store.Authenticate(credential)
	return c, ok, present
}
func authOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	return origin == "https://"+r.Host || origin == "http://"+r.Host
}
func (a *controllerAuthority) owner(r *http.Request, body []byte) bool {
	value := r.Header.Get(trust.OwnerHeader)
	parts := strings.SplitN(value, ":", 2)
	if len(parts) != 2 {
		return false
	}
	key := a.keys[parts[0]]
	_, e := a.ownerVerifier.Verify(key, "owner", parts[0], parts[1], r, body, time.Now())
	return e == nil
}
func decodeAuth(body []byte, target any) bool {
	return len(body) > 0 && json.Unmarshal(body, target) == nil
}
func (a *controllerAuthority) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !authOrigin(r) {
		authJSON(w, 403, map[string]any{"error": "origin_rejected"})
		return
	}
	body, e := io.ReadAll(io.LimitReader(r.Body, 8193))
	if e != nil || len(body) > 8192 {
		authJSON(w, 400, map[string]any{"error": "invalid_request"})
		return
	}
	route := strings.TrimPrefix(r.URL.Path, trustPrefix)
	if r.Method == "GET" && route == "status" {
		c, ok, _ := a.identity(r)
		result := map[string]any{"supported": true, "authenticated": ok, "approval": "owned_phone", "scope": "all_phones"}
		if ok {
			result["controller"] = c
		}
		authJSON(w, 200, result)
		return
	}
	if strings.HasPrefix(route, "owner/") {
		if !a.owner(r, body) {
			authJSON(w, 401, map[string]any{"error": "phone_authorization_required"})
			return
		}
		route = strings.TrimPrefix(route, "owner/")
		if r.Method == "GET" && route == "list" {
			a.list(w)
			return
		}
		if r.Method == "POST" && route == "approve" {
			var req struct {
				ID    string `json:"request_id"`
				Allow bool   `json:"allow"`
			}
			if !decodeAuth(body, &req) {
				authJSON(w, 400, map[string]any{"error": "invalid_request"})
				return
			}
			c, e := a.store.Approve(req.ID, req.Allow)
			if e != nil {
				authJSON(w, 409, map[string]any{"error": "request_expired"})
				return
			}
			authJSON(w, 200, map[string]any{"ok": true, "controller": c})
			return
		}
		if r.Method == "POST" && route == "revoke" {
			a.revoke(w, body)
			return
		}
		authJSON(w, 404, map[string]any{"error": "not_found"})
		return
	}
	if r.Method == "POST" && route == "requests" {
		var req struct{ Name, Platform, Kind string }
		if !decodeAuth(body, &req) {
			authJSON(w, 400, map[string]any{"error": "invalid_request"})
			return
		}
		ticket, e := a.store.Request(req.Name, req.Platform, req.Kind)
		if e != nil {
			authJSON(w, 429, map[string]any{"error": "too_many_requests"})
			return
		}
		if req.Kind == "browser" {
			authCookie(w, r, pairingCookie+"."+ticket.ID, ticket.Secret, 300)
			ticket.Secret = ""
		}
		authJSON(w, 201, ticket)
		return
	}
	if r.Method == "POST" && (route == "poll" || route == "cancel") {
		var req struct {
			ID     string `json:"request_id"`
			Secret string `json:"request_secret"`
		}
		if !decodeAuth(body, &req) {
			authJSON(w, 400, map[string]any{"error": "invalid_request"})
			return
		}
		if c, e := r.Cookie(pairingCookie + "." + req.ID); e == nil && req.Secret == "" {
			req.Secret = c.Value
		}
		if route == "cancel" {
			e := a.store.Cancel(req.ID, req.Secret)
			if e != nil {
				authJSON(w, 410, map[string]any{"error": "request_expired"})
				return
			}
			authCookie(w, r, pairingCookie+"."+req.ID, "", -1)
			authJSON(w, 200, map[string]any{"ok": true})
			return
		}
		result, e := a.store.Poll(req.ID, req.Secret)
		if e != nil {
			code := 410
			state := "request_expired"
			if errors.Is(e, trust.ErrPending) {
				code = 202
				state = "authorization_pending"
			}
			if errors.Is(e, trust.ErrSlow) {
				code = 429
				state = "slow_down"
				w.Header().Set("Retry-After", "2")
			}
			if errors.Is(e, trust.ErrDenied) {
				code = 403
				state = "access_denied"
			}
			authJSON(w, code, map[string]any{"state": state, "interval": 2})
			return
		}
		response := map[string]any{"state": "approved", "controller": result.Controller}
		if result.Kind == "browser" {
			authCookie(w, r, trustCookie, result.Credential, 365*24*3600)
			authCookie(w, r, pairingCookie+"."+req.ID, "", -1)
		} else {
			response["credential"] = result.Credential
		}
		authJSON(w, 200, response)
		return
	}
	if r.Method == "POST" && route == "session" {
		var req struct {
			Credential string `json:"credential"`
		}
		if len(body) > 0 && !decodeAuth(body, &req) {
			authJSON(w, 400, map[string]any{"error": "invalid_request"})
			return
		}
		credential, _ := a.credential(r)
		if req.Credential != "" {
			credential = req.Credential
		}
		c, ok := a.store.Authenticate(credential)
		if !ok {
			authCookie(w, r, trustCookie, "", -1)
			authJSON(w, 401, map[string]any{"error": "approval_required"})
			return
		}
		// Native clients retain their own protected credential; browser sessions
		// are represented by an HttpOnly cookie that is renewed on each visit.
		if _, e := r.Cookie(trustCookie); e == nil {
			authCookie(w, r, trustCookie, credential, 365*24*3600)
		}
		authJSON(w, 200, map[string]any{"authenticated": true, "controller": c})
		return
	}
	c, ok, _ := a.identity(r)
	if !ok {
		authJSON(w, 401, map[string]any{"error": "approval_required"})
		return
	}
	if r.Method == "GET" && route == "controllers" {
		a.list(w)
		return
	}
	if r.Method == "POST" && route == "revoke" {
		var req struct {
			ID string `json:"controller_id"`
		}
		_ = json.Unmarshal(body, &req)
		a.revoke(w, body)
		if req.ID == c.ID {
			authCookie(w, r, trustCookie, "", -1)
		}
		return
	}
	authJSON(w, 404, map[string]any{"error": "not_found"})
}
func (a *controllerAuthority) list(w http.ResponseWriter) {
	controllers, pending := a.store.List()
	authJSON(w, 200, map[string]any{"controllers": controllers, "requests": pending})
}
func (a *controllerAuthority) revoke(w http.ResponseWriter, body []byte) {
	var req struct {
		ID string `json:"controller_id"`
	}
	if !decodeAuth(body, &req) {
		authJSON(w, 400, map[string]any{"error": "invalid_request"})
		return
	}
	if e := a.store.Revoke(req.ID); e != nil {
		authJSON(w, 404, map[string]any{"error": "controller_not_found"})
		return
	}
	a.mu.Lock()
	connections := []net.Conn{}
	for conn := range a.connections[req.ID] {
		connections = append(connections, conn)
	}
	delete(a.connections, req.ID)
	a.mu.Unlock()
	for _, conn := range connections {
		conn.Close()
	}
	authJSON(w, 200, map[string]any{"ok": true})
}

type trackedConn struct {
	net.Conn
	authority  *controllerAuthority
	controller string
	once       sync.Once
}

func (c *trackedConn) Close() error {
	var e error
	c.once.Do(func() {
		e = c.Conn.Close()
		c.authority.mu.Lock()
		delete(c.authority.connections[c.controller], c)
		c.authority.mu.Unlock()
	})
	return e
}

type controllerWriter struct {
	http.ResponseWriter
	authority  *controllerAuthority
	controller string
}

func (w *controllerWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *controllerWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, e := http.NewResponseController(w.ResponseWriter).Hijack()
	if e != nil {
		return nil, nil, e
	}
	tracked := &trackedConn{Conn: conn, authority: w.authority, controller: w.controller}
	w.authority.mu.Lock()
	active := w.authority.store.Active(w.controller)
	if active {
		if w.authority.connections[w.controller] == nil {
			w.authority.connections[w.controller] = map[net.Conn]bool{}
		}
		w.authority.connections[w.controller][tracked] = true
	}
	w.authority.mu.Unlock()
	if !active {
		conn.Close()
		return nil, nil, errors.New("controller revoked")
	}
	return tracked, rw, nil
}
func authorizePhoneRequest(r *http.Request, d *gatewayDevice, c trust.Controller) error {
	body, e := io.ReadAll(io.LimitReader(r.Body, 4*1024*1024+1))
	if e != nil || len(body) > 4*1024*1024 {
		return errors.New("request too large")
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	clone := r.Clone(r.Context())
	target := *r.URL
	q := target.Query()
	q.Del(deviceParameter)
	target.RawQuery = q.Encode()
	clone.URL = &target
	token, e := trust.Sign(d.AuthKey, "delegate", d.Fingerprint, c.ID, clone, body, time.Now())
	if e != nil {
		return e
	}
	r.Header.Set(trust.DelegationHeader, token)
	return nil
}
