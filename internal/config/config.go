// Package config 环境配置:provider/model/key/base_url + 停止参数。
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Provider      string
	Model         string
	APIKey        string
	BaseURL       string
	MaxSteps      int
	MaxSameAction int
}

func Load() Config {
	return Config{
		Provider:      getenv("KCP_PROVIDER", "openai-compatible"),
		Model:         getenv("KCP_MODEL", "deepseek-chat"),
		APIKey:        os.Getenv("KCP_API_KEY"),
		BaseURL:       getenv("KCP_BASE_URL", "https://api.openai.com/v1"),
		MaxSteps:      getenvInt("KCP_MAX_STEPS", 10),
		MaxSameAction: getenvInt("KCP_MAX_SAME_ACTION", 3),
	}
}

func getenv(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func getenvInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
