package webservice

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
	"webscreen/devicetools"

	"github.com/gin-gonic/gin"
)

func (wm *WebMaster) deviceTools(c *gin.Context) {
	if !wm.config.EnableDeviceTools || wm.tools == nil {
		c.AbortWithStatusJSON(404, gin.H{"error": "tools_unavailable"})
		return
	}
	if c.Request.Method != "GET" && c.Request.Method != "HEAD" && !sameOrigin(c.Request) {
		c.AbortWithStatusJSON(403, gin.H{"error": "origin_not_allowed"})
		return
	}
	owner := c.GetString("gateway_controller")
	if owner == "" {
		owner = "local-owner"
	}
	wm.tools.ServeHTTP(c.Writer, devicetools.WithOwner(c.Request, owner))
}
func (wm *WebMaster) createTools() {
	if !wm.config.EnableDeviceTools {
		return
	}
	shared := wm.config.SharedDirectory
	if shared == "" {
		shared = "/sdcard"
	}
	manager, err := devicetools.New(devicetools.Config{Shared: shared, State: wm.config.ToolStateDirectory, Version: wm.config.Version, Telemetry: deviceTelemetry, Quality: wm.WebRTCManager.SetAdaptiveRate})
	if err != nil {
		return
	}
	wm.tools = manager
}

var telemetryCache struct {
	sync.Mutex
	at    time.Time
	value map[string]any
}

func deviceTelemetry(ctx context.Context) map[string]any {
	telemetryCache.Lock()
	defer telemetryCache.Unlock()
	if time.Since(telemetryCache.at) < 15*time.Second && telemetryCache.value != nil {
		return telemetryCache.value
	}
	value := map[string]any{}
	command := func(name string, args ...string) string {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, name, args...)
		output, err := cmd.Output()
		if err != nil || len(output) > 262144 {
			return ""
		}
		return string(output)
	}
	for _, line := range strings.Split(command("dumpsys", "battery"), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := parts[0]
		raw := strings.TrimSpace(parts[1])
		number, _ := strconv.Atoi(raw)
		switch key {
		case "level":
			value["battery_percent"] = number
		case "temperature":
			value["temperature_c"] = float64(number) / 10
		case "status":
			value["charging"] = number == 2 || number == 5
		case "voltage":
			value["voltage_mv"] = number
		}
	}
	value["model"] = strings.TrimSpace(command("getprop", "ro.product.model"))
	value["android"] = strings.TrimSpace(command("getprop", "ro.build.version.release"))
	wifi := command("cmd", "wifi", "status")
	network := "流動數據／其他網絡"
	if strings.Contains(wifi, "Wifi is enabled") && (strings.Contains(wifi, "Wifi is connected") || strings.Contains(wifi, "Supplicant state: COMPLETED")) {
		network = "Wi-Fi"
	}
	value["network"] = network
	value["updated_at"] = time.Now().Unix()
	telemetryCache.value = value
	telemetryCache.at = time.Now()
	return value
}
