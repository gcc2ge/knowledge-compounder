// Package config 环境配置:provider/model/key/base_url + 停止/恢复参数。
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
	MaxTokens     int // 0 = 不限(整体对话 token 预算,估算)
	MaxHeal       int // LLM 调用连续失败重试次数
	Deadline      int // 秒;0 = 默认 600
	StateFile     string // checkpoint 持久化路径(可断点续跑)
	Stream        bool // SSE 流式输出(默认开,KCP_STREAM=0 关闭)
}

func Load() Config {
	return Config{
		Provider:      getenv("KCP_PROVIDER", "openai-compatible"),
		Model:         getenv("KCP_MODEL", "deepseek-chat"),
		APIKey:        os.Getenv("KCP_API_KEY"),
		BaseURL:       getenv("KCP_BASE_URL", "https://api.openai.com/v1"),
		MaxSteps:      getenvInt("KCP_MAX_STEPS", 10),
		MaxSameAction: getenvInt("KCP_MAX_SAME_ACTION", 3),
		MaxTokens:     getenvInt("KCP_MAX_TOKENS", 0),
		MaxHeal:       getenvInt("KCP_MAX_HEAL", 3),
		Deadline:      getenvInt("KCP_DEADLINE", 600),
		StateFile:     os.Getenv("KCP_STATE_FILE"),
		Stream:        getenvInt("KCP_STREAM", 1) == 1,
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
