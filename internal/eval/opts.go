package eval

import (
	"github.com/knowledge-compounder/kcp/internal/config"
	"github.com/knowledge-compounder/kcp/internal/embed"
	"github.com/knowledge-compounder/kcp/internal/provider"
	"github.com/knowledge-compounder/kcp/internal/retrieval"
)

// RetrievalOpts 按配置构造检索选项:配 KCP_EMBED_MODEL 用在线语义向量,否则本地字符哈希嵌入;
// 向量带磁盘缓存(.kcp-embed-cache.json),避免每次重复 embed 全库。
func RetrievalOpts(root string, cfg config.Config) retrieval.Options {
	opts := retrieval.Options{Top: cfg.RetrieveK}
	if cfg.EmbedModel != "" {
		opts.Embedder = embed.NewOpenAI(cfg.EmbedBaseURL, cfg.EmbedAPIKey, cfg.EmbedModel)
		opts.Cache = embed.NewCache(retrieval.CachePath(root))
	} else {
		opts.Embedder = embed.NewLocal(0)
		opts.Cache = embed.NewCache(retrieval.CachePath(root))
	}
	return opts
}

func newProvider(cfg config.Config) provider.Provider {
	if cfg.Provider == "anthropic" {
		return provider.NewAnthropic(cfg.Model, cfg.APIKey, cfg.BaseURL)
	}
	return provider.NewOpenAICompatible(cfg.Model, cfg.APIKey, cfg.BaseURL)
}
