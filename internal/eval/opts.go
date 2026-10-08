package eval

import (
	"github.com/gcc2ge/knowledge-compounder/internal/config"
	"github.com/gcc2ge/knowledge-compounder/internal/embed"
	"github.com/gcc2ge/knowledge-compounder/internal/provider"
	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
)

// RetrievalOpts 按配置构造检索选项:配 KCP_EMBED_MODEL 用在线语义向量,否则本地字符哈希嵌入;
// 向量带磁盘缓存(.kcp-embed-cache.json),避免每次重复 embed 全库。
func RetrievalOpts(root string, cfg config.Config) retrieval.Options {
	opts := retrieval.Options{
		Top:   cfg.RetrieveK,
		Cache: embed.NewCache(retrieval.CachePath(root)),
	}
	if cfg.EmbedModel != "" {
		opts.Embedder = embed.NewOpenAI(cfg.EmbedBaseURL, cfg.EmbedAPIKey, cfg.EmbedModel)
	} else {
		opts.Embedder = embed.NewLocal(0)
	}
	return opts
}

func newProvider(cfg config.Config) provider.Provider {
	if cfg.Provider == "anthropic" {
		p := provider.NewAnthropic(cfg.Model, cfg.APIKey, cfg.BaseURL)
		p.MaxTokens = cfg.MaxOutputTokens
		return p
	}
	p := provider.NewOpenAICompatible(cfg.Model, cfg.APIKey, cfg.BaseURL)
	p.MaxTokens = cfg.MaxOutputTokens
	return p
}
