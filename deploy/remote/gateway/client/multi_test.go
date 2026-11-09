package client

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"remoteconsole.local/connect/envelope"
	"strings"
	"testing"
)

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestPerPhoneEnrollmentUsesFingerprintAndSealedPorts(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(1)}, &x509.Certificate{SerialNumber: big.NewInt(1)}, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	bundle := envelope.Bundle{Server: "control.example.com", ServerIP: "93.184.216.34", TunnelPort: 7001, ProxyPort: 18082, CA: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca}), ICE: []byte(`{"turn_shared_secret":"fixture-012345678901234567890123456789","turn_urls":["turn:93.184.216.34:3478"]}`)}
	bundle.FRPC = fmt.Sprintf("serverAddr = %q\nserverPort = 7001\nlocalIP = \"127.0.0.1\"\nlocalPort = 8079\nremotePort = 18082\ntransport.tls.enable = true\ntransport.tls.serverName = %q\ntransport.tls.trustedCaFile = %q\n[[proxies]]\n", bundle.ServerIP, bundle.Server, CAPath)
	if err = Validate(bundle, bundle.Server); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*envelope.Bundle){func(b *envelope.Bundle) { b.TunnelPort = 7000 }, func(b *envelope.Bundle) { b.ProxyPort = 18081 }, func(b *envelope.Bundle) { b.ProxyPort = 80 }, func(b *envelope.Bundle) { b.FRPC += "serverPort = 7002\n" }} {
		b := bundle
		mutate(&b)
		if Validate(b, b.Server) == nil {
			t.Fatal("unbound or duplicate tunnel settings accepted")
		}
	}
	sealed, err := envelope.Seal(&key.PublicKey, bundle)
	if err != nil {
		t.Fatal(err)
	}
	payload, _ := json.Marshal(sealed)
	transport := transportFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Scheme != "https" || r.URL.Host != "control.example.com" || r.URL.Path != "/_remoteconsole/enroll" || r.URL.Query().Get("key") != envelope.Fingerprint(&key.PublicKey) {
			t.Fatal("phone fetched another identity's profile")
		}
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: http.NoBody}, nil
	})
	// Substitute only the sealed response body; the production request path and
	// recipient verification still execute unchanged.
	wrapped := transportFunc(func(r *http.Request) (*http.Response, error) {
		response, e := transport.RoundTrip(r)
		response.Body = io.NopCloser(strings.NewReader(string(payload)))
		return response, e
	})
	got, err := FetchWithTransport(context.Background(), "https://control.example.com", key, wrapped)
	if err != nil || got.TunnelPort != 7001 || got.ProxyPort != 18082 {
		t.Fatalf("per-phone profile: %v", err)
	}
	legacy := bundle
	legacy.TunnelPort = 0
	legacy.ProxyPort = 0
	legacy.FRPC = strings.ReplaceAll(strings.ReplaceAll(legacy.FRPC, "7001", "7000"), "18082", "18081")
	if err = Validate(legacy, legacy.Server); err != nil {
		t.Fatalf("primary phone enrollment changed: %v", err)
	}
}
