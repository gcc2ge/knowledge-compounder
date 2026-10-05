// Package embed 向量化:本地字符 n-gram 哈希(离线、确定性)或 OpenAI 兼容 /embeddings(在线语义)。
// 能力位对齐 M02:不配置 KCP_EMBED_MODEL 时用本地嵌入(任意 LLM 可跑、零外部依赖)。
package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Embedder 向量化接口。实现:LocalEmbedder(离线哈希)、OpenAIEmbedder(/embeddings)。
type Embedder interface {
	Embed(texts []string) ([][]float32, error)
	Dim() int
}

// ---- 离线嵌入:字符 n-gram 哈希 bag-of-words(2/3/4-gram),L2 归一化 ----

// LocalEmbedder 哈希字符 n-gram 到固定维度槽位,形态/拼写容错优于精确 token 匹配。
type LocalEmbedder struct {
	dim   int
	grams []int
}

// NewLocal 构造本地嵌入器。dim<=0 时取 512。
func NewLocal(dim int) *LocalEmbedder {
	if dim <= 0 {
		dim = 512
	}
	return &LocalEmbedder{dim: dim, grams: []int{2, 3, 4}}
}

func (l *LocalEmbedder) Dim() int { return l.dim }

func (l *LocalEmbedder) Embed(texts []string) ([][]float32, error) {
	vecs := make([][]float32, len(texts))
	for i, t := range texts {
		vecs[i] = l.embedOne(t)
	}
	return vecs, nil
}

func (l *LocalEmbedder) embedOne(text string) []float32 {
	v := make([]float32, l.dim)
	text = strings.ToLower(text)
	for _, n := range l.grams {
		for i := 0; i+n <= len(text); i++ {
			h := fnv.New32a()
			h.Write([]byte(text[i : i+n]))
			v[h.Sum32()%uint32(l.dim)]++
		}
	}
	norm(v)
	return v
}

// ---- 在线嵌入:OpenAI 兼容 /embeddings ----

// OpenAIEmbedder 走 {baseURL}/embeddings,model 如 text-embedding-3-small。
type OpenAIEmbedder struct {
	baseURL, apiKey, model string
	dim                    int
	client                 *http.Client // 带超时,防端点挂起(对齐 provider 的网络纪律)
}

// NewOpenAI 构造 OpenAI 兼容嵌入器。
func NewOpenAI(baseURL, apiKey, model string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		baseURL: strings.TrimSuffix(baseURL, "/"), apiKey: apiKey, model: model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (o *OpenAIEmbedder) Dim() int { return o.dim }

type openAIEmbedReq struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}
type openAIEmbedResp struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

func (o *OpenAIEmbedder) Embed(texts []string) ([][]float32, error) {
	body, err := json.Marshal(openAIEmbedReq{Model: o.model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, o.baseURL+"/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+o.apiKey)
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("embeddings HTTP %d", resp.StatusCode)
	}
	var out openAIEmbedResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	vecs := make([][]float32, 0, len(out.Data))
	for _, d := range out.Data {
		norm(d.Embedding)
		vecs = append(vecs, d.Embedding)
		if o.dim == 0 {
			o.dim = len(d.Embedding)
		}
	}
	if len(vecs) != len(texts) {
		return nil, fmt.Errorf("embeddings 返回 %d 条,期望 %d", len(vecs), len(texts))
	}
	return vecs, nil
}

// ---- 公共 ----

// Cosine 计算两个 L2 归一化向量的余弦相似度。
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	var dot float64
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		dot += float64(a[i]) * float64(b[i])
	}
	return dot
}

func norm(v []float32) {
	var sum float64
	for _, x := range v {
		sum += float64(x) * float64(x)
	}
	if sum > 0 {
		s := float32(1 / math.Sqrt(sum))
		for i := range v {
			v[i] *= s
		}
	}
}

// Cache 磁盘向量缓存:key = 文档路径|mtime|size,避免每轮查询重复 embed 全库。
// dim 记录缓存向量维度:切换嵌入器(本地 512 ↔ OpenAI 1536)时维度不符的键视为
// 未命中并重新 embed,Set 时维度冲突则整库重建——防 Cosine 取 min 兜底产生错乱检索。
type Cache struct {
	path string
	mu   sync.Mutex
	m    map[string][]float32
	dim  int
}

// NewCache 加载(或初始化)缓存文件。path 为空则只做内存缓存。
// 兼容旧版无 dim 的纯 map 缓存文件(读入后 dim=0,首次 Set 时定维)。
func NewCache(path string) *Cache {
	c := &Cache{path: path, m: map[string][]float32{}}
	if path == "" {
		return c
	}
	if b, err := os.ReadFile(path); err == nil {
		var disk struct {
			Dim  int                  `json:"dim"`
			Vecs map[string][]float32 `json:"vecs"`
		}
		if json.Unmarshal(b, &disk) == nil && disk.Vecs != nil {
			c.m, c.dim = disk.Vecs, disk.Dim
			return c
		}
		_ = json.Unmarshal(b, &c.m) // 旧版结构
	}
	return c
}

// Key 生成文档缓存键。
func Key(path string, fi os.FileInfo) string {
	return KeyParts(path, fi.ModTime().UnixNano(), fi.Size())
}

// KeyParts 免 stat 的缓存键(索引里已存 mtime/size 时用)。
func KeyParts(path string, mtimeNano, size int64) string {
	return path + "|" + strconv.FormatInt(mtimeNano, 10) + "|" + strconv.FormatInt(size, 10)
}

// Get 读取缓存向量。wantDim>0 且缓存维度不符 → 未命中(调用方重新 embed 覆盖)。
func (c *Cache) Get(key string, wantDim int) ([]float32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[key]
	if ok && wantDim > 0 && len(v) != wantDim {
		return nil, false
	}
	return v, ok
}

// Set 写入向量;与既有缓存维度冲突时整库重建(嵌入器切换)。
func (c *Cache) Set(key string, v []float32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dim > 0 && len(v) != c.dim {
		c.m = map[string][]float32{}
	}
	c.dim = len(v)
	c.m[key] = v
}

// Save 持久化到磁盘(幂等,失败静默)。带 dim 字段供下次加载校验。
func (c *Cache) Save() {
	if c.path == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.m) == 0 {
		return
	}
	if b, err := json.Marshal(map[string]any{"dim": c.dim, "vecs": c.m}); err == nil {
		_ = os.WriteFile(c.path, b, 0o644)
	}
}
