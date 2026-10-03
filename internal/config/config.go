// Package config 环境配置:provider/model/key/base_url + 停止/恢复参数。
package config

import (
	"os"
	"strconv"
)

type Config struct {
	Provider        string
	Model           string
	APIKey          string
	BaseURL         string
	MaxSteps        int
	MaxSameAction   int
	MaxTokens       int    // 0 = 不限(整体对话 token 预算,真实 usage 优先,缺省估算)
	MaxHeal         int    // LLM 调用连续失败重试次数
	Deadline        int    // 秒;0 = 默认 600
	StateFile       string // checkpoint 持久化路径(可断点续跑)
	Stream          bool   // SSE 流式输出(默认开,KCP_STREAM=0 关闭)
	CompactTokens   int    // M09 上下文治理:估算 token 超此值压缩早期历史;0 = 关闭治理
	KeepRounds      int    // M09:压缩时保留最近几轮(0 = 默认 8)
	CheckpointEvery int    // checkpoint 写盘步频(0 = 默认每 3 步)
	EmbedModel      string // 语义检索:留空 = 离线字符哈希嵌入;设置后用 OpenAI 兼容 /embeddings
	EmbedBaseURL    string
	EmbedAPIKey     string
	RetrieveK       int // search_wiki / kcp search 返回条数
}

func Load() Config {
	return Config{
		Provider:        getenv("KCP_PROVIDER", "openai-compatible"),
		Model:           getenv("KCP_MODEL", "deepseek-chat"),
		APIKey:          os.Getenv("KCP_API_KEY"),
		BaseURL:         getenv("KCP_BASE_URL", "https://api.openai.com/v1"),
		MaxSteps:        getenvInt("KCP_MAX_STEPS", 10),
		MaxSameAction:   getenvInt("KCP_MAX_SAME_ACTION", 3),
		MaxTokens:       getenvInt("KCP_MAX_TOKENS", 0),
		MaxHeal:         getenvInt("KCP_MAX_HEAL", 3),
		Deadline:        getenvInt("KCP_DEADLINE", 600),
		StateFile:       os.Getenv("KCP_STATE_FILE"),
		Stream:          getenvInt("KCP_STREAM", 1) == 1,
		CompactTokens:   getenvInt("KCP_COMPACT_TOKENS", 0),
		KeepRounds:      getenvInt("KCP_COMPACT_KEEP_ROUNDS", 0),
		CheckpointEvery: getenvInt("KCP_CHECKPOINT_EVERY", 0),
		EmbedModel:      os.Getenv("KCP_EMBED_MODEL"),
		EmbedBaseURL:    getenv("KCP_EMBED_BASE_URL", os.Getenv("KCP_BASE_URL")),
		EmbedAPIKey:     getenv("KCP_EMBED_API_KEY", os.Getenv("KCP_API_KEY")),
		RetrieveK:       getenvInt("KCP_RETRIEVE_K", 5),
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
