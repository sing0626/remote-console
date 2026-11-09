package webservice

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v4"
	"os"
	"strings"
	"time"
)

type ICEConfiguration struct {
	STUN   []string `json:"stun_urls"`
	TURN   []string `json:"turn_urls"`
	Secret string   `json:"turn_shared_secret"`
}

func ReadICEConfiguration(path string) (*ICEConfiguration, error) {
	c := &ICEConfiguration{STUN: []string{"stun:stun.l.google.com:19302"}}
	if path == "" {
		return c, nil
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	return ParseICEConfiguration(data)
}

func ParseICEConfiguration(data []byte) (*ICEConfiguration, error) {
	c := &ICEConfiguration{}
	if err := json.Unmarshal(data, c); err != nil {
		return nil, err
	}
	if len(c.TURN) > 0 && len(c.Secret) < 32 {
		return nil, fmt.Errorf("TURN secret must contain at least 32 characters")
	}
	for _, url := range c.STUN {
		if !strings.HasPrefix(url, "stun:") {
			return nil, fmt.Errorf("invalid STUN URL")
		}
	}
	for _, url := range c.TURN {
		if !strings.HasPrefix(url, "turn:") && !strings.HasPrefix(url, "turns:") {
			return nil, fmt.Errorf("invalid TURN URL")
		}
	}
	return c, nil
}
func (c *ICEConfiguration) Servers(now time.Time) []webrtc.ICEServer {
	if c == nil {
		return []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}
	servers := []webrtc.ICEServer{}
	if len(c.STUN) > 0 {
		servers = append(servers, webrtc.ICEServer{URLs: c.STUN})
	}
	if len(c.TURN) > 0 {
		username := fmt.Sprintf("%d:remote", now.Add(time.Hour).Unix())
		mac := hmac.New(sha1.New, []byte(c.Secret))
		_, _ = mac.Write([]byte(username))
		servers = append(servers, webrtc.ICEServer{URLs: c.TURN, Username: username, Credential: base64.StdEncoding.EncodeToString(mac.Sum(nil))})
	}
	return servers
}
func (wm *WebMaster) SetICEConfiguration(config *ICEConfiguration) {
	wm.WebRTCManager.iceConfigMu.Lock()
	defer wm.WebRTCManager.iceConfigMu.Unlock()
	wm.WebRTCManager.iceConfig = config
}

func (manager *WebRTCManager) iceServers() []webrtc.ICEServer {
	manager.iceConfigMu.RLock()
	config := manager.iceConfig
	manager.iceConfigMu.RUnlock()
	return config.Servers(time.Now())
}
