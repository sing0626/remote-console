package webservice

import (
	"fmt"
	"strconv"
)

func validateStreamOptions(config map[string]string) error {
	fps := config["max_fps"]
	if fps == "" {
		config["max_fps"] = "30"
	} else if fps != "30" && fps != "60" && fps != "120" {
		return fmt.Errorf("choose 30, 60 or 120 FPS")
	}
	for key, bounds := range map[string][2]int{"max_size": {256, 3840}, "video_bit_rate": {500000, 30000000}} {
		if value := config[key]; value != "" {
			n, err := strconv.Atoi(value)
			if err != nil || n < bounds[0] || n > bounds[1] {
				return fmt.Errorf("invalid %s", key)
			}
		}
	}
	return nil
}

func (manager *WebRTCManager) activeStreamOptions() map[string]string {
	manager.RLock()
	defer manager.RUnlock()
	for _, b := range manager.broadcasters {
		if b.Agent != nil && b.Agent.Alive() {
			result := make(map[string]string)
			for k, v := range b.StreamOptions {
				result[k] = v
			}
			return result
		}
	}
	return nil
}
