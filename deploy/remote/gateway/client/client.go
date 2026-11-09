package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"remoteconsole.local/connect/envelope"
	"strings"
	"time"
)

const CAPath = "/data/local/tmp/remote-console/remote/ca.crt"

// TransportForServer avoids relying on Android root's unavailable libc DNS
// while HTTP's TLS verification continues to use the original HTTPS hostname.
func TransportForServer(origin, address string) (http.RoundTripper, error) {
	u, err := ServerURL(origin)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(address))
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return nil, errors.New("use a public server IPv4")
	}
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.Proxy = nil
	t.DialContext = func(ctx context.Context, network, target string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(target)
		if err != nil || host != u.Hostname() || port != "443" {
			return nil, errors.New("enrollment routing changed server origin")
		}
		return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
	}
	return t, nil
}

func Identity(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		key, e := rsa.GenerateKey(rand.Reader, 3072)
		if e != nil {
			return nil, e
		}
		if e = os.MkdirAll(filepath.Dir(path), 0700); e != nil {
			return nil, e
		}
		f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if e != nil {
			return nil, e
		}
		data = pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
		_, e = f.Write(data)
		closeErr := f.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return key, nil
	}
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid private enrollment identity")
	}
	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err == nil && key.N.BitLen() < 3072 {
		return nil, errors.New("enrollment identity too short")
	}
	return key, err
}
func ServerURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	if u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Port() != "" && u.Port() != "443" || u.Path != "" && u.Path != "/" {
		return nil, errors.New("use an HTTPS server origin without credentials or path")
	}
	u.Path = ""
	return u, nil
}
func Fetch(ctx context.Context, origin string, key *rsa.PrivateKey) (envelope.Bundle, error) {
	return FetchWithTransport(ctx, origin, key, nil)
}

// FetchWithTransport permits a caller to supply routing while retaining the same
// HTTPS origin validation, certificate checks and redirect restrictions.
func FetchWithTransport(ctx context.Context, origin string, key *rsa.PrivateKey, transport http.RoundTripper) (envelope.Bundle, error) {
	var bundle envelope.Bundle
	u, err := ServerURL(origin)
	if err != nil {
		return bundle, err
	}
	endpoint := *u
	endpoint.Path = "/_remoteconsole/enroll"
	query := endpoint.Query()
	query.Set("key", envelope.Fingerprint(&key.PublicKey))
	endpoint.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return bundle, err
	}
	h := &http.Client{Transport: transport, Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		if r.URL.Scheme != "https" || r.URL.Host != u.Host {
			return errors.New("enrollment redirect changed server origin")
		}
		return nil
	}}
	response, err := h.Do(req)
	if err != nil {
		return bundle, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return bundle, fmt.Errorf("enrollment server returned HTTP %d", response.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	if err != nil {
		return bundle, err
	}
	if len(data) > 128*1024 {
		return bundle, errors.New("enrollment response too large")
	}
	var sealed envelope.Sealed
	if err = json.Unmarshal(data, &sealed); err != nil {
		return bundle, errors.New("server has not enabled encrypted enrollment")
	}
	bundle, err = envelope.Open(key, sealed)
	if err != nil {
		return bundle, err
	}
	return bundle, Validate(bundle, u.Hostname())
}
func Validate(b envelope.Bundle, host string) error {
	if b.Server != host {
		return errors.New("enrollment server identity mismatch")
	}
	ip := net.ParseIP(b.ServerIP)
	if ip == nil || ip.To4() == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return errors.New("invalid enrollment server IPv4")
	}
	tunnelPort, proxyPort := b.TunnelPort, b.ProxyPort
	if tunnelPort == 0 {
		tunnelPort = 7000
	}
	if proxyPort == 0 {
		proxyPort = 18081
	}
	if tunnelPort < 1024 || tunnelPort > 65535 || proxyPort < 1024 || proxyPort > 65535 || tunnelPort == proxyPort {
		return errors.New("invalid per-device tunnel ports")
	}
	required := map[string]string{
		"serverAddr": `"` + b.ServerIP + `"`, "serverPort": fmt.Sprint(tunnelPort), "localIP": `"127.0.0.1"`, "localPort": "8079", "remotePort": fmt.Sprint(proxyPort),
		"transport.tls.enable": "true", "transport.tls.serverName": `"` + host + `"`, "transport.tls.trustedCaFile": `"` + CAPath + `"`,
	}
	for field, want := range required {
		found := regexp.MustCompile(`(?m)^`+regexp.QuoteMeta(field)+`\s*=\s*([^\r\n]*?)\s*$`).FindAllStringSubmatch(b.FRPC, -1)
		if len(found) != 1 || found[0][1] != want {
			return fmt.Errorf("unexpected enrollment setting: %s", field)
		}
	}
	if strings.Contains(b.FRPC, "include") || strings.Contains(b.FRPC, "plugin") || strings.Count(b.FRPC, "[[proxies]]") != 1 {
		return errors.New("unsupported enrollment proxy configuration")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(b.CA) {
		return errors.New("invalid tunnel CA certificate")
	}
	var ice struct {
		Secret string   `json:"turn_shared_secret"`
		TURN   []string `json:"turn_urls"`
	}
	if json.Unmarshal(b.ICE, &ice) != nil || len(ice.Secret) < 32 || len(ice.TURN) == 0 {
		return errors.New("invalid private ICE configuration")
	}
	return nil
}
func Save(dir, origin string, b envelope.Bundle) error {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	files := map[string][]byte{"phone-frpc.toml": []byte(b.FRPC), "ice.private.json": b.ICE, "ca.crt": b.CA}
	manifest := struct {
		Origin string            `json:"origin"`
		Hashes map[string]string `json:"hashes"`
	}{origin, map[string]string{}}
	for name, data := range files {
		f, err := os.CreateTemp(dir, ".import-")
		if err != nil {
			return err
		}
		tmp := f.Name()
		_, err = f.Write(data)
		closeErr := f.Close()
		if err != nil {
			os.Remove(tmp)
			return err
		}
		if closeErr != nil {
			os.Remove(tmp)
			return closeErr
		}
		if err = os.Rename(tmp, filepath.Join(dir, name)); err != nil {
			os.Remove(tmp)
			return err
		}
		sum := sha256.Sum256(data)
		manifest.Hashes[name] = hex.EncodeToString(sum[:])
	}
	data, _ := json.Marshal(manifest)
	return os.WriteFile(filepath.Join(dir, "enrolled.json"), data, 0600)
}
func Cached(dir, origin string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "enrolled.json"))
	if err != nil {
		return false
	}
	var manifest struct {
		Origin string            `json:"origin"`
		Hashes map[string]string `json:"hashes"`
	}
	if json.Unmarshal(data, &manifest) != nil || manifest.Origin != origin || len(manifest.Hashes) != 3 {
		return false
	}
	for _, name := range []string{"phone-frpc.toml", "ice.private.json", "ca.crt"} {
		data, err = os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return false
		}
		sum := sha256.Sum256(data)
		if manifest.Hashes[name] != hex.EncodeToString(sum[:]) {
			return false
		}
		if name == "phone-frpc.toml" {
			address := regexp.MustCompile(`(?m)^serverAddr\s*=\s*"([0-9.]+)"\s*$`).FindSubmatch(data)
			if len(address) != 2 {
				return false
			}
			ip := net.ParseIP(string(address[1]))
			if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
				return false
			}
		}
	}
	return true
}
