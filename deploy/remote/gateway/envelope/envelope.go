package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
)

type Sealed struct {
	Version   int    `json:"version"`
	Recipient string `json:"recipient"`
	Key       []byte `json:"key"`
	Nonce     []byte `json:"nonce"`
	Data      []byte `json:"data"`
}
type Bundle struct {
	Server     string `json:"server"`
	ServerIP   string `json:"server_ip"`
	FRPC       string `json:"frpc"`
	ICE        []byte `json:"ice"`
	CA         []byte `json:"ca"`
	TunnelPort int    `json:"tunnel_port,omitempty"`
	ProxyPort  int    `json:"proxy_port,omitempty"`
}

func Fingerprint(key *rsa.PublicKey) string {
	der, _ := x509.MarshalPKIXPublicKey(key)
	sum := sha256.Sum256(der)
	return hex.EncodeToString(sum[:])
}
func PublicPEM(key *rsa.PublicKey) []byte {
	der, _ := x509.MarshalPKIXPublicKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}
func ParsePublic(data []byte) (*rsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("invalid enrollment public key")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := key.(*rsa.PublicKey)
	if !ok || rsaKey.N.BitLen() < 3072 {
		return nil, errors.New("enrollment requires RSA 3072 or stronger")
	}
	return rsaKey, nil
}
func Seal(key *rsa.PublicKey, bundle Bundle) (Sealed, error) {
	var out Sealed
	data, err := json.Marshal(bundle)
	if err != nil {
		return out, err
	}
	secret := make([]byte, 32)
	if _, err = rand.Read(secret); err != nil {
		return out, err
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return out, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return out, err
	}
	out.Version = 1
	out.Recipient = Fingerprint(key)
	out.Nonce = make([]byte, gcm.NonceSize())
	if _, err = rand.Read(out.Nonce); err != nil {
		return out, err
	}
	aad := []byte("remote-console-enroll-v1:" + out.Recipient)
	out.Key, err = rsa.EncryptOAEP(sha256.New(), rand.Reader, key, secret, aad)
	if err != nil {
		return out, err
	}
	out.Data = gcm.Seal(nil, out.Nonce, data, aad)
	return out, nil
}
func Open(key *rsa.PrivateKey, in Sealed) (Bundle, error) {
	var bundle Bundle
	if in.Version != 1 || in.Recipient != Fingerprint(&key.PublicKey) {
		return bundle, errors.New("enrollment recipient mismatch")
	}
	aad := []byte("remote-console-enroll-v1:" + in.Recipient)
	secret, err := rsa.DecryptOAEP(sha256.New(), rand.Reader, key, in.Key, aad)
	if err != nil {
		return bundle, errors.New("invalid encrypted enrollment key")
	}
	block, err := aes.NewCipher(secret)
	if err != nil {
		return bundle, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return bundle, err
	}
	if len(in.Nonce) != gcm.NonceSize() {
		return bundle, errors.New("invalid enrollment nonce")
	}
	data, err := gcm.Open(nil, in.Nonce, in.Data, aad)
	if err != nil {
		return bundle, errors.New("enrollment integrity check failed")
	}
	err = json.Unmarshal(data, &bundle)
	return bundle, err
}
