package webservice

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/pion/webrtc/v4"
	"os"
	"regexp"
	"strings"
	"time"
)

type ICEConfiguration struct {
	STUN    []string      `json:"stun_urls"`
	TURN    []string      `json:"turn_urls"`
	Secret  string        `json:"turn_shared_secret"`
	Regions []RelayRegion `json:"relay_regions,omitempty"`
}

type RelayRegion struct {
	ID     string   `json:"id"`
	Name   string   `json:"name"`
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
	c := &ICEConfiguration{STUN: []string{"stun:stun.l.google.com:19302"}}
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
	if len(c.Regions) > 4 {
		return nil, fmt.Errorf("too many relay regions")
	}
	seen := map[string]bool{}
	for _, region := range c.Regions {
		if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,23}$`).MatchString(region.ID) || seen[region.ID] || strings.TrimSpace(region.Name) == "" || len(region.Name) > 240 {
			return nil, fmt.Errorf("invalid relay region")
		}
		seen[region.ID] = true
		data, _ := json.Marshal(&ICEConfiguration{STUN: region.STUN, TURN: region.TURN, Secret: region.Secret})
		if _, err := ParseICEConfiguration(data); err != nil {
			return nil, err
		}
	}
	return c, nil
}
func (c *ICEConfiguration) Servers(now time.Time) []webrtc.ICEServer {
	return c.ServersForRegion(now, "")
}
func (c *ICEConfiguration) ServersForRegion(now time.Time, region string) []webrtc.ICEServer {
	if c == nil {
		return []webrtc.ICEServer{{URLs: []string{"stun:stun.l.google.com:19302"}}}
	}
	servers := []webrtc.ICEServer{}
	if len(c.Regions) > 0 {
		for _, item := range c.Regions {
			if region == "" || region == "auto" || item.ID == region {
				single := &ICEConfiguration{STUN: item.STUN, TURN: item.TURN, Secret: item.Secret}
				servers = append(servers, single.Servers(now)...)
			}
		}
		if len(servers) > 0 {
			return servers
		}
	}
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
	return manager.iceServersForRegion("")
}
func (manager *WebRTCManager) iceServersForRegion(region string) []webrtc.ICEServer {
	manager.iceConfigMu.RLock()
	config := manager.iceConfig
	manager.iceConfigMu.RUnlock()
	return config.ServersForRegion(time.Now(), region)
}

func (manager *WebRTCManager) relayRegions() []map[string]any {
	manager.iceConfigMu.RLock()
	defer manager.iceConfigMu.RUnlock()
	items := []map[string]any{}
	if manager.iceConfig == nil {
		return items
	}
	for _, region := range manager.iceConfig.Regions {
		items = append(items, map[string]any{"id": region.ID, "name": region.Name, "urls": region.TURN})
	}
	return items
}
func (manager *WebRTCManager) hasRelayRegion(id string) bool {
	if id == "" || id == "auto" {
		return true
	}
	for _, region := range manager.relayRegions() {
		if region["id"] == id {
			return true
		}
	}
	return false
}
