package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	nethtml "golang.org/x/net/html"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"remoteconsole.local/connect/releaseinfo"
	"remoteconsole.local/connect/trust"
	"strconv"
	"strings"
)

const deviceParameter = "rc_device"
const deviceHeader = "X-RemoteConsole-Device"

type deviceGateway struct {
	devices   []*gatewayDevice
	byID      map[string]*gatewayDevice
	defaultID string
	auth      *controllerAuthority
	relays    *relayMonitor
}

func newMultiHandler(devices []*gatewayDevice, defaultID string) http.Handler {
	g := &deviceGateway{devices: devices, byID: map[string]*gatewayDevice{}, defaultID: defaultID}
	for _, device := range devices {
		g.byID[device.ID] = device
		device.Proxy = deviceProxy(device, defaultID)
	}
	return g
}

func (g *deviceGateway) selection(r *http.Request) (id string, fromReferrer bool, err error) {
	header := r.Header.Get(deviceHeader)
	query := r.URL.Query()[deviceParameter]
	if len(query) > 1 {
		return "", false, fmt.Errorf("duplicate device selector")
	}
	if len(query) == 1 {
		id = query[0]
		if id == "" {
			return "", false, fmt.Errorf("empty device selector")
		}
	}
	if header != "" {
		if id != "" && id != header {
			return "", false, fmt.Errorf("conflicting device selectors")
		}
		id = header
	}
	if id == "" && r.Referer() != "" {
		ref, e := url.Parse(r.Referer())
		if e == nil && ref.Host == r.Host && (ref.Scheme == "http" || ref.Scheme == "https") {
			values := ref.Query()[deviceParameter]
			if len(values) == 1 && values[0] != "" {
				id = values[0]
				fromReferrer = true
			}
		}
	}
	if id != "" && !deviceIDPattern.MatchString(id) {
		return "", false, fmt.Errorf("invalid device selector")
	}
	return id, fromReferrer, nil
}

func (g *deviceGateway) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if serveBrandAsset(w, r) {
		return
	}
	w.Header().Set("Referrer-Policy", "same-origin")
	r.Header.Del(trust.DelegationHeader)
	if g.auth != nil && strings.HasPrefix(r.URL.Path, trustPrefix) {
		g.auth.ServeHTTP(w, r)
		return
	}
	if g.relays != nil && r.URL.Path == "/_remoteconsole/relays/report" {
		g.relays.receive(w, r)
		return
	}
	if g.relays != nil && r.URL.Path == "/_remoteconsole/relays/status" {
		if r.Method != "GET" {
			http.Error(w, "method", 405)
			return
		}
		if g.auth == nil {
			http.Error(w, "authorization required", 401)
			return
		}
		if _, ok, _ := g.auth.identity(r); !ok {
			http.Error(w, "authorization required", 401)
			return
		}
		g.relays.status(w, r)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/_remoteconsole/devices" {
		type entry struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Online bool   `json:"online"`
		}
		list := make([]entry, 0, len(g.devices))
		for _, d := range g.devices {
			list = append(list, entry{d.ID, d.Name, online(d.Upstream)})
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-RemoteConsole-Registry", "1")
		json.NewEncoder(w).Encode(map[string]any{"registry_version": 1, "default_device": g.defaultID, "devices": list, "trusted_controller": g.auth != nil, "suite_version": releaseinfo.Version})
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/_remoteconsole/enroll" {
		wanted := r.URL.Query().Get("key")
		d := g.byID[g.defaultID]
		if wanted != "" {
			d = nil
			for _, item := range g.devices {
				if item.Fingerprint == wanted {
					d = item
					break
				}
			}
		}
		if d == nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Write(d.Payload)
		return
	}
	id, ref, err := g.selection(r)
	if err != nil {
		http.Error(w, "invalid device selection", http.StatusBadRequest)
		return
	}
	if id == "" && r.Method == http.MethodGet && (r.URL.Path == "/control" || r.URL.Path == "/") && len(g.devices) > 1 && r.Header.Get("Upgrade") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		io.WriteString(w, portalPage)
		return
	}
	if id == "" {
		id = g.defaultID
	}
	d := g.byID[id]
	if d == nil {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodGet && r.URL.Path == "/_remoteconsole/status" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		json.NewEncoder(w).Encode(map[string]any{"gateway_ready": true, "phone_online": online(d.Upstream), "automatic_enrollment": true, "device_id": id, "device_name": d.Name, "suite_version": releaseinfo.Version})
		return
	}
	// Resolve inherited HTML/module/CSS URLs to a device-specific cache key.
	// A cookie never chooses the destination, so two tabs cannot switch each
	// other's phone by changing a shared browser session cookie.
	if ref && r.Method == http.MethodGet && r.Header.Get("Upgrade") == "" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Add("Vary", "Referer")
		http.Redirect(w, r, deviceURL(r.URL.String(), id), http.StatusTemporaryRedirect)
		return
	}
	if g.auth != nil {
		if r.Method == "GET" && r.URL.Path == "/unlock" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			io.WriteString(w, portalPage)
			return
		}
		controller, ok, present := g.auth.identity(r)
		if present && !ok {
			if r.Method == "GET" && strings.Contains(r.Header.Get("Accept"), "text/html") && r.Header.Get("Upgrade") == "" {
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				io.WriteString(w, portalPage)
				return
			}
			authJSON(w, 401, map[string]any{"error": "controller_revoked"})
			return
		}
		if ok {
			if err := authorizePhoneRequest(r, d, controller); err != nil {
				authJSON(w, 400, map[string]any{"error": "invalid_request"})
				return
			}
			if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
				w = &controllerWriter{ResponseWriter: w, authority: g.auth, controller: controller.ID}
			}
		}
	}
	d.Proxy.ServeHTTP(w, r)
}

func cookiePrefix(id string) string {
	return "rcd." + base64.RawURLEncoding.EncodeToString([]byte(id)) + "."
}

func deviceProxy(device *gatewayDevice, defaultID string) *httputil.ReverseProxy {
	proxy := httputil.NewSingleHostReverseProxy(device.Upstream)
	original := proxy.Director
	proxy.Director = func(r *http.Request) {
		original(r)
		query := r.URL.Query()
		query.Del(deviceParameter)
		r.URL.RawQuery = query.Encode()
		r.Header.Del(deviceHeader)
		r.Header.Del("Accept-Encoding")
		r.Header.Del(trust.ControllerHeader)
		r.Header.Del(trust.OwnerHeader)
		cookies := r.Cookies()
		r.Header.Del("Cookie")
		prefix := cookiePrefix(device.ID)
		scoped := map[string]bool{}
		for _, cookie := range cookies {
			if cookie.Name == trustCookie || strings.HasPrefix(cookie.Name, pairingCookie+".") {
				continue
			}
			if strings.HasPrefix(cookie.Name, prefix) {
				scoped[strings.TrimPrefix(cookie.Name, prefix)] = true
			}
		}
		for _, cookie := range cookies {
			if cookie.Name == trustCookie || strings.HasPrefix(cookie.Name, pairingCookie+".") {
				continue
			}
			if strings.HasPrefix(cookie.Name, prefix) {
				cookie.Name = strings.TrimPrefix(cookie.Name, prefix)
				r.AddCookie(cookie)
			} else if device.ID == defaultID && cookie.Name == "auth_token" && !scoped[cookie.Name] {
				r.AddCookie(cookie)
			}
		}
	}
	proxy.ModifyResponse = func(response *http.Response) error {
		cookies := response.Cookies()
		response.Header.Del("Set-Cookie")
		for _, cookie := range cookies {
			cookie.Name = cookiePrefix(device.ID) + cookie.Name
			cookie.Domain = ""
			cookie.Path = "/"
			if response.Request.Header.Get("X-Forwarded-Proto") == "https" || response.Request.TLS != nil {
				cookie.Secure = true
			}
			response.Header.Add("Set-Cookie", cookie.String())
		}
		if location := response.Header.Get("Location"); location != "" {
			if u, e := url.Parse(location); e == nil {
				if u.Host == device.Upstream.Host {
					u.Host = response.Request.Host
					u.Scheme = "https"
					if response.Request.Header.Get("X-Forwarded-Proto") == "http" {
						u.Scheme = "http"
					}
				}
				if u.Host == "" || u.Host == response.Request.Host {
					response.Header.Set("Location", deviceURL(u.String(), device.ID))
				}
			}
		}
		response.Header.Set("Referrer-Policy", "same-origin")
		if response.StatusCode == http.StatusOK && strings.Contains(response.Header.Get("Content-Type"), "text/html") {
			if response.Header.Get("Content-Encoding") != "" {
				return fmt.Errorf("cannot scope compressed phone HTML")
			}
			body, err := io.ReadAll(io.LimitReader(response.Body, 2*1024*1024+1))
			response.Body.Close()
			if err != nil || len(body) > 2*1024*1024 {
				return fmt.Errorf("phone HTML exceeds gateway limit")
			}
			body = scopeHTML(body, device.ID)
			response.Body = io.NopCloser(bytes.NewReader(body))
			response.ContentLength = int64(len(body))
			response.Header.Set("Content-Length", strconv.Itoa(len(body)))
			response.Header.Set("Cache-Control", "no-store")
			response.Header.Del("ETag")
			response.Header.Del("Last-Modified")
			response.Header.Del("Content-MD5")
		}
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method == http.MethodGet && !strings.HasPrefix(r.URL.Path, "/api/") && r.Header.Get("Upgrade") == "" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			page := strings.ReplaceAll(offlinePage, "location.replace('/control')", "location.replace('/control'+location.search)")
			w.Write(scopeHTML([]byte(page), device.ID))
			return
		}
		http.Error(w, `{"error":"phone_offline"}`, http.StatusServiceUnavailable)
	}
	return proxy
}

func deviceURL(raw, id string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	q := u.Query()
	q.Set(deviceParameter, id)
	u.RawQuery = q.Encode()
	return u.String()
}

func scopeHTML(body []byte, id string) []byte {
	encoded, _ := json.Marshal(id)
	bootstrap := `<script>(function(){const id=` + string(encoded) + `;const route=function(value){const u=new URL(value,location.href);if(u.host===location.host&&['http:','https:','ws:','wss:'].includes(u.protocol))u.searchParams.set('rc_device',id);return u.href;};const originalFetch=window.fetch;window.fetch=function(input,init){if(input instanceof Request)return originalFetch.call(this,new Request(route(input.url),input),init);return originalFetch.call(this,route(input),init);};const OriginalWebSocket=window.WebSocket;window.WebSocket=class extends OriginalWebSocket{constructor(value,protocols){if(protocols===undefined)super(route(value));else super(route(value),protocols);}};window.remoteConsoleDevice=id;})();</script>`
	var output bytes.Buffer
	tokenizer := nethtml.NewTokenizer(bytes.NewReader(body))
	inserted := false
	for {
		kind := tokenizer.Next()
		if kind == nethtml.ErrorToken {
			break
		}
		raw := append([]byte(nil), tokenizer.Raw()...)
		if kind == nethtml.StartTagToken || kind == nethtml.SelfClosingTagToken {
			token := tokenizer.Token()
			changed := false
			for index, attr := range token.Attr {
				if (attr.Key == "src" || attr.Key == "href" || attr.Key == "action") && strings.HasPrefix(attr.Val, "/") && !strings.HasPrefix(attr.Val, "//") {
					token.Attr[index].Val = deviceURL(attr.Val, id)
					changed = true
				}
			}
			if changed {
				output.WriteString(token.String())
			} else {
				output.Write(raw)
			}
			if token.Data == "head" && !inserted {
				output.WriteString(bootstrap)
				inserted = true
			}
		} else {
			output.Write(raw)
		}
	}
	if inserted {
		return output.Bytes()
	}
	text := output.String()
	index := 0
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(text)), "<!doctype") {
		if end := strings.Index(text, ">"); end >= 0 {
			index = end + 1
		}
	}
	return []byte(text[:index] + bootstrap + text[index:])
}
