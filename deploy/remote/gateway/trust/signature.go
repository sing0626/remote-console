// Package trust implements owned-controller approval and short-lived,
// request-bound delegation between the gateway and an enrolled phone.
package trust

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"
)

const DelegationHeader = "X-RemoteConsole-Delegation"
const OwnerHeader = "X-RemoteConsole-Phone-Authorization"
const ControllerHeader = "X-RemoteConsole-Controller"

func Random(n int) (string, error) {
	b := make([]byte, n)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// DeriveKey keeps the management/delegation key separate from the existing
// per-phone tunnel credential. Neither credential is sent to controllers.
func DeriveKey(profile []byte, fingerprint string) ([]byte, error) {
	hits := regexp.MustCompile(`(?m)^auth\.token\s*=\s*"([^"\r\n]+)"\s*$`).FindAllSubmatch(profile, -1)
	if len(hits) != 1 || len(hits[0][1]) < 32 || !regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(fingerprint) {
		return nil, errors.New("invalid enrolled phone identity")
	}
	mac := hmac.New(sha256.New, hits[0][1])
	mac.Write([]byte("RemoteConsole/trusted-controller/v1/" + fingerprint))
	return mac.Sum(nil), nil
}

type claims struct {
	Version  int    `json:"v"`
	Purpose  string `json:"purpose"`
	Audience string `json:"aud"`
	Subject  string `json:"sub"`
	Issued   int64  `json:"iat"`
	Expires  int64  `json:"exp"`
	Method   string `json:"method"`
	URI      string `json:"uri"`
	Body     string `json:"body"`
	Nonce    string `json:"nonce"`
}

func digest(body []byte) string { h := sha256.Sum256(body); return hex.EncodeToString(h[:]) }
func Sign(key []byte, purpose, audience, subject string, r *http.Request, body []byte, now time.Time) (string, error) {
	if len(key) != 32 || (purpose != "owner" && purpose != "delegate") {
		return "", errors.New("invalid signing identity")
	}
	nonce, e := Random(18)
	if e != nil {
		return "", e
	}
	data, e := json.Marshal(claims{1, purpose, audience, subject, now.Unix(), now.Add(30 * time.Second).Unix(), r.Method, r.URL.RequestURI(), digest(body), nonce})
	if e != nil {
		return "", e
	}
	encoded := base64.RawURLEncoding.EncodeToString(data)
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

type Verifier struct {
	mu   sync.Mutex
	seen map[string]int64
}

func (v *Verifier) Verify(key []byte, purpose, audience, token string, r *http.Request, body []byte, now time.Time) (string, error) {
	bad := errors.New("invalid or expired phone authorization")
	if len(key) != 32 || len(token) > 4096 {
		return "", bad
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return "", bad
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return "", bad
	}
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(parts[0]))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return "", bad
	}
	data, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return "", bad
	}
	var c claims
	if json.Unmarshal(data, &c) != nil || c.Version != 1 || c.Purpose != purpose || c.Audience != audience || c.Subject == "" || c.Method != r.Method || c.URI != r.URL.RequestURI() || c.Body != digest(body) || c.Issued > now.Unix()+30 || c.Expires <= now.Unix() || c.Expires-c.Issued != 30 || len(c.Nonce) != 24 {
		return "", bad
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seen == nil {
		v.seen = map[string]int64{}
	}
	for nonce, until := range v.seen {
		if until <= now.Unix() {
			delete(v.seen, nonce)
		}
	}
	replayKey := audience + ":" + purpose + ":" + c.Nonce
	if _, ok := v.seen[replayKey]; ok || len(v.seen) >= 8192 {
		return "", bad
	}
	v.seen[replayKey] = c.Expires
	return c.Subject, nil
}
