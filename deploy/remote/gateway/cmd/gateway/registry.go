package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"remoteconsole.local/connect/envelope"
	"remoteconsole.local/connect/trust"
	"strconv"
	"strings"
)

var deviceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)

type deviceConfig struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Upstream string `json:"upstream"`
	Key      string `json:"key"`
	Private  string `json:"private"`
}
type registryConfig struct {
	Version int            `json:"version"`
	Default string         `json:"default_device"`
	Devices []deviceConfig `json:"devices"`
}
type gatewayDevice struct {
	ID, Name, Fingerprint string
	Upstream              *url.URL
	Payload               []byte
	Proxy                 *httputil.ReverseProxy
	AuthKey               []byte
}

// The registry is server-owned configuration. Requests can only select an ID;
// they never supply an upstream URL, filename, port, or enrollment key.
func handlerFromRegistry(path string, trustFile ...string) (http.Handler, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg registryConfig
	if err = json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("invalid device registry")
	}
	if cfg.Version != 1 || len(cfg.Devices) == 0 || len(cfg.Devices) > 32 {
		return nil, fmt.Errorf("unsupported device registry")
	}
	devices := make([]*gatewayDevice, 0, len(cfg.Devices))
	ids, keys := map[string]bool{}, map[string]bool{}
	for _, item := range cfg.Devices {
		if !deviceIDPattern.MatchString(item.ID) || ids[item.ID] || strings.TrimSpace(item.Name) == "" || len([]rune(item.Name)) > 80 {
			return nil, fmt.Errorf("invalid or duplicate device identity")
		}
		upstream, err := url.Parse(item.Upstream)
		if err != nil || upstream.Scheme != "http" || upstream.Hostname() != "127.0.0.1" || upstream.User != nil || upstream.RawQuery != "" || upstream.Fragment != "" || upstream.Path != "" {
			return nil, fmt.Errorf("device upstream must be a loopback HTTP listener")
		}
		port, err := strconv.Atoi(upstream.Port())
		if err != nil || port < 1024 || port > 65535 {
			return nil, fmt.Errorf("invalid device upstream port")
		}
		payload, fingerprint, proxyPort, err := sealDeviceProfile(item.Private, item.Key)
		if err != nil {
			return nil, fmt.Errorf("cannot prepare enrollment for %s: %w", item.ID, err)
		}
		if port != proxyPort || keys[fingerprint] {
			return nil, fmt.Errorf("device profile/upstream mismatch or duplicate key")
		}
		ids[item.ID], keys[fingerprint] = true, true
		device := &gatewayDevice{ID: item.ID, Name: item.Name, Upstream: upstream, Payload: payload, Fingerprint: fingerprint}
		if len(trustFile) > 0 && trustFile[0] != "" {
			profile, e := os.ReadFile(filepath.Join(item.Private, "phone-frpc.toml"))
			if e != nil {
				return nil, e
			}
			device.AuthKey, e = trust.DeriveKey(profile, fingerprint)
			if e != nil {
				return nil, e
			}
		}
		devices = append(devices, device)
	}
	if !ids[cfg.Default] {
		return nil, fmt.Errorf("default device is missing")
	}
	handler := newMultiHandler(devices, cfg.Default).(*deviceGateway)
	if len(trustFile) > 0 && trustFile[0] != "" {
		handler.auth, err = newControllerAuthority(trustFile[0], devices)
		if err != nil {
			return nil, err
		}
	}
	stateDir := filepath.Dir(path)
	if len(trustFile) > 0 && trustFile[0] != "" {
		stateDir = filepath.Dir(trustFile[0])
	}
	handler.relays, err = loadRelayMonitor(filepath.Dir(path), stateDir)
	if err != nil {
		return nil, err
	}
	return handler, nil
}

func sealDeviceProfile(dir, keyPath string) ([]byte, string, int, error) {
	keyData, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, "", 0, err
	}
	key, err := envelope.ParsePublic(keyData)
	if err != nil {
		return nil, "", 0, err
	}
	frpc, err := os.ReadFile(filepath.Join(dir, "phone-frpc.toml"))
	if err != nil {
		return nil, "", 0, err
	}
	setting := func(field string) (string, error) {
		hits := regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(field)+`\s*=\s*([^\r\n]+)\s*$`).FindAllSubmatch(frpc, -1)
		if len(hits) != 1 {
			return "", fmt.Errorf("missing or duplicate profile field %s", field)
		}
		return strings.Trim(strings.TrimSpace(string(hits[0][1])), `"`), nil
	}
	host, err := setting("transport.tls.serverName")
	if err != nil {
		return nil, "", 0, err
	}
	ip, err := setting("serverAddr")
	if err != nil {
		return nil, "", 0, err
	}
	tunnelRaw, err := setting("serverPort")
	if err != nil {
		return nil, "", 0, err
	}
	proxyRaw, err := setting("remotePort")
	if err != nil {
		return nil, "", 0, err
	}
	tunnelPort, e1 := strconv.Atoi(tunnelRaw)
	proxyPort, e2 := strconv.Atoi(proxyRaw)
	if e1 != nil || e2 != nil || tunnelPort < 1024 || tunnelPort > 65535 || proxyPort < 1024 || proxyPort > 65535 || tunnelPort == proxyPort {
		return nil, "", 0, fmt.Errorf("invalid profile ports")
	}
	ice, err := os.ReadFile(filepath.Join(dir, "ice.private.json"))
	if err != nil {
		return nil, "", 0, err
	}
	ca, err := os.ReadFile(filepath.Join(dir, "pki/ca.crt"))
	if err != nil {
		return nil, "", 0, err
	}
	sealed, err := envelope.Seal(key, envelope.Bundle{Server: host, ServerIP: ip, FRPC: string(frpc), ICE: ice, CA: ca, TunnelPort: tunnelPort, ProxyPort: proxyPort})
	if err != nil {
		return nil, "", 0, err
	}
	payload, err := json.Marshal(sealed)
	return payload, envelope.Fingerprint(key), proxyPort, err
}
