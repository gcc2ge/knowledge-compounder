// Package index 知识库内存索引:倒排(tf/df 预计算)+ wikilink 图。
// 目标:检索/计数/图查询零全库扫——BM25 与 wiki_mentions 走预计算结构,
// 增量同步(mtime|size 对比)只重解析变化页;快照 .kcp/index.gob(encoding/gob)跨进程复用。
// 快照只持久化倒排+元数据+出链(重算贵的);Text/Tokens 按需读盘水合——
// 实测 716 页快照含全文+全 token 表时 48MB,gob 解码比现读文件还慢,得不偿失。
// 规模边界:万页级以上考虑 sqlite(见 docs)。非并发安全:调用方(CLI agent 循环 / MCP stdio)为串行调用。
package index

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gcc2ge/knowledge-compounder/internal/embed"
	"github.com/gcc2ge/knowledge-compounder/internal/retrieval"
	"github.com/gcc2ge/knowledge-compounder/internal/wiki"
)

const snapshotVersion = 3

// PageMeta 单页索引项。Text/Tokens 仅进程内(全量 Build 时填充);快照载入后为空,hydrate 按需读盘。
type PageMeta struct {
	Path     string // 相对 root,如 wiki/concepts/xxx.md
	MTime    int64  // UnixNano(增量判据)
	Size     int64
	Dir      string // sources/concepts/entities/synthesis/notes
	Origin   string // frontmatter origin(私有度徽标用)
	Text     string // 全文缓存(懒加载)
	Tokens   map[string]int
	OutLinks []string // [[wikilink]] 目标

	hydrated bool // 已水合标记(区分「空页」与「未加载」)
}

// Index 内存索引。
type Index struct {
	Version int
	Pages   map[string]*PageMeta           // rel path → meta
	Invert  map[string]map[string]struct{} // term → rel path 集合(df = len)
	Back    map[string][]string            // slug → 链向它的页面 slug(反向链接)
	SavedAt time.Time

	root      string            // 项目根(gob 不序列化)
	slugIndex map[string]string // slug → rel path
}

// Report 一次增量同步的变更计数。
type Report struct{ Added, Updated, Removed, Unchanged int }

// MentionResult 单个 term 的源页提及计数。
type MentionResult struct {
	Term string
	Hits []string // 提及该 term 的源页 slug(source 页限定)
}

// ---- 构建与同步 ----

// Build 全量构建索引。
func Build(root string) *Index {
	idx := &Index{
		Version: snapshotVersion,
		Pages:   map[string]*PageMeta{},
		Invert:  map[string]map[string]struct{}{},
		Back:    map[string][]string{},
		root:    root,
	}
	for _, p := range wiki.Pages(root) {
		idx.addPage(p)
	}
	idx.rebuildGraph()
	return idx
}

// Load 读快照 → 增量 Sync → 返回;快照缺失/损坏/版本不符则全量 Build。
// KCP_INDEX=off 返回 nil(调用方降级走全扫旧路径)。
func Load(root string) *Index {
	if os.Getenv("KCP_INDEX") == "off" {
		return nil
	}
	if b, err := os.ReadFile(snapshotPath(root)); err == nil {
		idx := &Index{}
		if err := idx.GobDecode(b); err == nil {
			idx.root = root
			idx.Sync(root)
			return idx
		}
	}
	idx := Build(root)
	_ = idx.Save(root)
	return idx
}

// Sync 增量同步:stat 对比 mtime|size,未变跳过、变了重解析、消失则摘除。
func (idx *Index) Sync(root string) Report {
	var rp Report
	onDisk := map[string]string{} // rel → abs
	for _, p := range wiki.Pages(root) {
		onDisk[idx.rel(p)] = p
	}
	// 摘除消失页 / 重解析变化页
	for rel, meta := range idx.Pages {
		abs, ok := onDisk[rel]
		if !ok {
			idx.removePage(rel)
			rp.Removed++
			continue
		}
		if fi, err := os.Stat(abs); err == nil &&
			fi.ModTime().UnixNano() == meta.MTime && fi.Size() == meta.Size {
			rp.Unchanged++
			continue
		}
		idx.addPage(abs)
		rp.Updated++
	}
	// 新增页
	for rel, abs := range onDisk {
		if _, ok := idx.Pages[rel]; !ok {
			idx.addPage(abs)
			rp.Added++
		}
	}
	idx.rebuildGraph()
	return rp
}

// Save 原子写快照(tmp + rename)。
func (idx *Index) Save(root string) error {
	dir := filepath.Join(root, ".kcp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(dir, "index.gob.tmp")
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	idx.SavedAt = time.Now()
	b, err := idx.GobEncode()
	if err == nil {
		_, err = f.Write(b)
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "index.gob"))
}

// ---- 快照序列化(V3:倒排+元数据+出链;Text/Tokens 不持久化,懒水合) ----

// pageSnapshot 持久化的单页元数据(切片下标即页 id)。
type pageSnapshot struct {
	Path, Dir, Origin string
	MTime, Size       int64
	OutLinks          []string
}

// persistedV3 字典编码:路径只在 Pages 存一份,倒排 posting 用页 id(int32)。
// 朴素存法 57 万条 posting 各带一条长中文路径,快照 27MB、解码慢于全扫——字典化后体积 ≈ 1/5。
type persistedV3 struct {
	Version  int
	SavedAt  time.Time
	Pages    []pageSnapshot // 页 id = 下标
	Terms    []string       // 词表,下标与 Postings 对齐
	Postings [][]int32      // 词 id → 页 id 列表
}

// GobEncode 只序列化重算成本高的结构(倒排+元数据+出链图原料);
// Back/slugIndex 由 rebuildGraph 重建,Text/Tokens 按需读盘。
func (idx *Index) GobEncode() ([]byte, error) {
	ids := make(map[string]int32, len(idx.Pages))
	pages := make([]pageSnapshot, 0, len(idx.Pages))
	for rel, m := range idx.Pages {
		ids[rel] = int32(len(pages))
		pages = append(pages, pageSnapshot{Path: m.Path, Dir: m.Dir, Origin: m.Origin,
			MTime: m.MTime, Size: m.Size, OutLinks: m.OutLinks})
	}
	terms := make([]string, 0, len(idx.Invert))
	postings := make([][]int32, 0, len(idx.Invert))
	for t, set := range idx.Invert {
		ps := make([]int32, 0, len(set))
		for rel := range set {
			if id, ok := ids[rel]; ok { // 防御:跳过悬空 posting
				ps = append(ps, id)
			}
		}
		terms = append(terms, t)
		postings = append(postings, ps)
	}
	snap := persistedV3{Version: snapshotVersion, SavedAt: time.Now(),
		Pages: pages, Terms: terms, Postings: postings}
	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(snap); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode 载入快照并重建内存结构(slugIndex/Back;Text/Tokens 留空待水合)。
func (idx *Index) GobDecode(b []byte) error {
	var snap persistedV3
	if err := gob.NewDecoder(bytes.NewReader(b)).Decode(&snap); err != nil {
		return err
	}
	if snap.Version != snapshotVersion {
		return fmt.Errorf("快照版本 %d ≠ %d,弃用重建", snap.Version, snapshotVersion)
	}
	relByID := make([]string, len(snap.Pages))
	idx.Version = snap.Version
	idx.SavedAt = snap.SavedAt
	idx.Pages = make(map[string]*PageMeta, len(snap.Pages))
	for id, ps := range snap.Pages {
		relByID[id] = ps.Path
		idx.Pages[ps.Path] = &PageMeta{Path: ps.Path, Dir: ps.Dir, Origin: ps.Origin,
			MTime: ps.MTime, Size: ps.Size, OutLinks: ps.OutLinks}
	}
	idx.Invert = make(map[string]map[string]struct{}, len(snap.Terms))
	for i, t := range snap.Terms {
		ids := snap.Postings[i]
		set := make(map[string]struct{}, len(ids))
		for _, id := range ids {
			if int(id) < len(relByID) {
				set[relByID[id]] = struct{}{}
			}
		}
		if len(set) > 0 {
			idx.Invert[t] = set
		}
	}
	idx.rebuildGraph()
	return nil
}

// hydrate 按需读盘水合 Text/Tokens(读失败视作空页,下次 Sync 修正)。
func (idx *Index) hydrate(m *PageMeta) {
	if m.hydrated {
		return
	}
	m.hydrated = true
	b, err := os.ReadFile(filepath.Join(idx.root, m.Path))
	if err != nil {
		return
	}
	m.Text = string(b)
	m.Tokens = map[string]int{}
	for _, t := range retrieval.Tokenize(m.Text) {
		m.Tokens[t]++
	}
}

// UpdateFile 单页重解析(write_file 写入后的 hook;仅 wiki/ 内页面入索引)。
// 与 Build/Sync 的全量路径不同:这里是写密集热路径(compiler 一次编译多次写页),
// 只增量同步受影响的 Back/slugIndex 条目,不做全量 rebuildGraph。
func (idx *Index) UpdateFile(absPath string) {
	rel := idx.rel(absPath)
	if !strings.HasPrefix(rel, "wiki/") {
		return
	}
	old := idx.Pages[rel] // 增量 Back 对比的旧出链(新增页为 nil)
	idx.addPage(absPath)
	idx.syncBack(rel, old)
}

// syncBack 把单页出链变化增量反映到 Back/slugIndex,替代全量 rebuildGraph。
// 与 rebuildGraph 同语义:跳过 raw/ 前缀链接,target 去 .md 后缀。
func (idx *Index) syncBack(rel string, old *PageMeta) {
	slug := slugOf(rel)
	idx.slugIndex[slug] = rel
	cur := idx.Pages[rel]
	if cur == nil {
		return
	}
	var oldSet, newSet map[string]struct{}
	if old != nil {
		oldSet = linkTargetSet(old.OutLinks)
	}
	newSet = linkTargetSet(cur.OutLinks)
	for t := range oldSet {
		if _, keep := newSet[t]; !keep {
			removeBackEntry(idx.Back, t, slug)
		}
	}
	for t := range newSet {
		if _, had := oldSet[t]; !had {
			addBackEntry(idx.Back, t, slug)
		}
	}
}

// linkTargetSet 出链 → 目标 slug 集合(与 rebuildGraph 同过滤:跳过 raw/ 前缀,去 .md)。
func linkTargetSet(links []string) map[string]struct{} {
	set := make(map[string]struct{}, len(links))
	for _, l := range links {
		if strings.HasPrefix(l, "raw/") {
			continue
		}
		set[strings.TrimSuffix(l, ".md")] = struct{}{}
	}
	return set
}

// addBackEntry 向 Back[target] 追加链接者 slug(幂等:已存在不重复)。
func addBackEntry(back map[string][]string, target, slug string) {
	for _, s := range back[target] {
		if s == slug {
			return
		}
	}
	back[target] = append(back[target], slug)
}

// removeBackEntry 从 Back[target] 移除链接者 slug;清空后删除该 key。
func removeBackEntry(back map[string][]string, target, slug string) {
	xs := back[target]
	for i, s := range xs {
		if s == slug {
			back[target] = append(xs[:i], xs[i+1:]...)
			break
		}
	}
	if len(back[target]) == 0 {
		delete(back, target)
	}
}

func (idx *Index) addPage(absPath string) {
	rel := idx.rel(absPath)
	meta := parsePage(absPath, rel)
	if _, ok := idx.Pages[rel]; ok {
		idx.removeInvertEntries(rel) // 旧版可能未水合(Tokens 空),按 rel 反扫才正确
	}
	idx.Pages[rel] = meta
	for t := range meta.Tokens {
		if idx.Invert[t] == nil {
			idx.Invert[t] = map[string]struct{}{}
		}
		idx.Invert[t][rel] = struct{}{}
	}
}

func (idx *Index) removePage(rel string) {
	if idx.Pages[rel] == nil {
		return
	}
	idx.removeInvertEntries(rel)
	delete(idx.Pages, rel)
}

// removeInvertEntries 从倒排摘除一页的全部条目。不依赖该页 Tokens——
// 快照载入的页未水合时 Tokens 为空,逐 token 删会留脏条目;按 rel 反扫 Invert 恒正确。
func (idx *Index) removeInvertEntries(rel string) {
	for t, set := range idx.Invert {
		if _, ok := set[rel]; ok {
			delete(set, rel)
			if len(set) == 0 {
				delete(idx.Invert, t)
			}
		}
	}
}

func parsePage(absPath, rel string) *PageMeta {
	meta := &PageMeta{Path: rel, Tokens: map[string]int{}, Dir: filepath.Base(filepath.Dir(absPath)), hydrated: true}
	b, err := os.ReadFile(absPath)
	if err != nil {
		return meta
	}
	meta.Text = string(b)
	if fi, err := os.Stat(absPath); err == nil {
		meta.MTime, meta.Size = fi.ModTime().UnixNano(), fi.Size()
	}
	for _, t := range retrieval.Tokenize(meta.Text) {
		meta.Tokens[t]++
	}
	meta.OutLinks = wiki.ParseWikilinks(meta.Text)
	vals, _ := wiki.ParseFrontmatter(meta.Text)
	if o, ok := vals["origin"].(string); ok {
		meta.Origin = o
	}
	return meta
}

// rebuildGraph 重建反向链接与 slug 索引(全量重建,千页级开销可忽略)。
// 与 lint 的入站统计同语义:跳过 raw/ 前缀链接;与 syncBack 同语义:按去重后的
// 目标 slug 记账(同一页重复列出同一 wikilink 时,Back[target] 不出现重复 slug)。
func (idx *Index) rebuildGraph() {
	idx.Back = map[string][]string{}
	idx.slugIndex = map[string]string{}
	for rel, m := range idx.Pages {
		slug := slugOf(rel)
		idx.slugIndex[slug] = rel
		for target := range linkTargetSet(m.OutLinks) {
			idx.Back[target] = append(idx.Back[target], slug)
		}
	}
}

// ---- 查询接口 ----

// Rank 混合检索(词法走预计算 tf/df,向量层只对候选页计算)。
// 与旧全扫路径的行为差异:向量层限定在词法候选集内——纯语义命中(零词法重叠)
// 的页面不再入选;词法权重 0.55 为基,实际排序影响极小。
func (idx *Index) Rank(query string, opts retrieval.Options) []retrieval.Result {
	if opts.Top <= 0 {
		opts.Top = 5
	}
	terms := retrieval.Tokenize(query)
	if len(terms) == 0 || len(idx.Pages) == 0 {
		return nil
	}
	// 候选集 = 含任一查询词的页面(倒排求并)
	cand := map[string]*PageMeta{}
	for _, t := range terms {
		for rel := range idx.Invert[t] {
			if m, ok := idx.Pages[rel]; ok {
				cand[rel] = m
			}
		}
	}
	if len(cand) == 0 {
		return nil
	}

	n := float64(len(idx.Pages))
	type scored struct {
		rel string
		m   *PageMeta
		lex float64
	}
	var cands []scored
	maxLex := 0.0
	for rel, m := range cand {
		idx.hydrate(m) // 快照载入的页 Text/Tokens 为空,打分前读盘
		var s float64
		head := m.Text
		if len(head) > 300 {
			head = head[:300]
		}
		lowHead := strings.ToLower(head)
		for _, t := range terms {
			cnt := m.Tokens[t]
			if cnt == 0 {
				continue
			}
			cnt += strings.Count(lowHead, t) // 标题/开头命中加权,与旧实现同语义
			df := float64(len(idx.Invert[t]))
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			s += idf * float64(cnt) / (float64(cnt) + 1.2)
		}
		if s > 0 {
			cands = append(cands, scored{rel, m, s})
			if s > maxLex {
				maxLex = s
			}
		}
	}

	// 可选向量层:仅候选页(缓存键免 stat,直用索引里的 mtime|size)
	var sem map[string]float64
	useVec := opts.Embedder != nil
	if useVec {
		sem, useVec = idx.embedCandidates(query, cand, opts)
	}

	out := make([]retrieval.Result, 0, len(cands))
	for _, c := range cands {
		lexN := 0.0
		if maxLex > 0 {
			lexN = c.lex / maxLex
		}
		score, semN := lexN, 0.0
		if useVec {
			if sem[c.rel] < 0 {
				sem[c.rel] = 0
			}
			semN = sem[c.rel]
			score = 0.55*lexN + 0.45*semN
		}
		out = append(out, retrieval.Result{
			Path:     filepath.Join(idx.root, c.rel),
			Label:    strings.TrimSuffix(filepath.Base(c.rel), ".md"),
			Score:    score,
			Lexical:  lexN,
			Semantic: semN,
			Summary:  retrieval.SummaryOf(c.m.Text),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > opts.Top {
		out = out[:opts.Top]
	}
	return out
}

// embedCandidates 只对候选页跑向量嵌入并回填缓存,返回 sem 得分与是否成功。
// 失败时降级纯词法((nil,false));缓存命中免重复嵌入(键用 mtime|size)。
func (idx *Index) embedCandidates(query string, cand map[string]*PageMeta, opts retrieval.Options) (map[string]float64, bool) {
	vecs := map[string][]float32{}
	var pending []string
	for rel, m := range cand {
		if opts.Cache != nil {
			if v, ok := opts.Cache.Get(embed.KeyParts(m.Path, m.MTime, m.Size), opts.Embedder.Dim()); ok {
				vecs[rel] = v
				continue
			}
		}
		pending = append(pending, rel)
	}
	for start := 0; start < len(pending); start += 16 {
		end := start + 16
		if end > len(pending) {
			end = len(pending)
		}
		texts := make([]string, 0, end-start)
		for _, rel := range pending[start:end] {
			texts = append(texts, idx.Pages[rel].Text)
		}
		b, err := opts.Embedder.Embed(texts)
		if err != nil {
			break // 嵌入失败:向量层降级,词法分照常
		}
		for j, rel := range pending[start:end] {
			vecs[rel] = b[j]
			if opts.Cache != nil {
				m := idx.Pages[rel]
				opts.Cache.Set(embed.KeyParts(m.Path, m.MTime, m.Size), b[j])
			}
		}
	}
	if opts.Cache != nil {
		opts.Cache.Save()
	}
	qv, err := opts.Embedder.Embed([]string{query})
	if err != nil || len(qv) != 1 {
		return nil, false
	}
	sem := map[string]float64{}
	for rel := range cand {
		if v, ok := vecs[rel]; ok {
			sem[rel] = embed.Cosine(qv[0], v)
		}
	}
	return sem, true
}

// Mentions 批量统计源页提及(大小写不敏感 Contains,与全扫实现语义一致)。
func (idx *Index) Mentions(terms []string) []MentionResult {
	out := make([]MentionResult, 0, len(terms))
	for _, term := range terms {
		if term == "" {
			continue
		}
		low := strings.ToLower(term)
		var hits []string
		for rel, m := range idx.Pages {
			if m.Dir != "sources" {
				continue
			}
			idx.hydrate(m) // Contains 需要全文,按需读盘
			if strings.Contains(strings.ToLower(m.Text), low) {
				hits = append(hits, slugOf(rel))
			}
		}
		sort.Strings(hits)
		out = append(out, MentionResult{Term: term, Hits: hits})
	}
	return out
}

// FormatMentions 三档建页话术 + 确定性护栏警示。护栏防 Contains 语义伪影:
// 实测「泄漏」作为「goroutine泄漏」的子串继承了全部提及数,工具说 ≥2、模型照建——
// 计数没错,判据太粗;把警示写进返回文本,便宜模型就能被机械拦住。
func (idx *Index) FormatMentions(rs []MentionResult) string {
	var b strings.Builder
	for _, r := range rs {
		switch n := len(r.Hits); n {
		case 0:
			fmt.Fprintf(&b, "「%s」未被任何源页提及。\n", r.Term)
		case 1:
			fmt.Fprintf(&b, "「%s」仅 1 个源提及(%s)——单次提及,进该源「术语」节,不建页。\n", r.Term, r.Hits[0])
		default:
			fmt.Fprintf(&b, "「%s」被 %d 个源提及(%s)——≥2,满足建页纪律,可创建/更新对应页面。\n", r.Term, n, strings.Join(r.Hits, "、"))
		}
		for _, w := range idx.mentionGuards(r.Term, rs) {
			fmt.Fprintf(&b, "  ⚠️ %s\n", w)
		}
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// mentionGuards 三层护栏:①术语过短(单汉字泛化词)②同批更长术语的子串 ③已有页面名的子串。
// nil 接收者安全(降级路径仅少第③层)。
func (idx *Index) mentionGuards(term string, rs []MentionResult) []string {
	var warns []string
	if utf8.RuneCountInString(term) < 2 {
		warns = append(warns, "术语过短,子串匹配可能高估提及——泛化词慎建页。")
	}
	for _, o := range rs {
		if o.Term != term && len(o.Term) > len(term) && strings.Contains(o.Term, term) {
			warns = append(warns, fmt.Sprintf("「%s」是同批术语「%s」的子串,命中可能来自后者——优先并入完整术语的页面,勿单独建页。", term, o.Term))
		}
	}
	if idx != nil {
		for slug := range idx.slugIndex {
			if slug != term && len(slug) > len(term) && strings.Contains(slug, term) {
				warns = append(warns, fmt.Sprintf("「%s」是已有页面「%s」名称的子串,命中可能继承自该页主题——慎建独立页,优先并入。", term, slug))
				break // 一个例证足够
			}
		}
	}
	return warns
}

// GetPage 按 slug 读页(命中索引定位,正文按需读盘)。
func (idx *Index) GetPage(slug string) (text, dir string, ok bool) {
	slug = strings.TrimSuffix(slug, ".md")
	rel, exists := idx.slugIndex[slug]
	if !exists {
		return "", "", false
	}
	m := idx.Pages[rel]
	idx.hydrate(m)
	return m.Text, m.Dir, true
}

// Backlinks 双向链接查询:谁链向 slug / slug 链向谁。
func (idx *Index) Backlinks(slug string) (inbound, outbound []string) {
	slug = strings.TrimSuffix(slug, ".md")
	if rel, ok := idx.slugIndex[slug]; ok {
		outbound = idx.Pages[rel].OutLinks
	}
	inbound = idx.Back[slug]
	sort.Strings(inbound)
	return inbound, outbound
}

// BrokenLinks 断链检测(图查询):出链目标不在 slug 集(跳过 raw/ 前缀)。
func (idx *Index) BrokenLinks() map[string][]string {
	out := map[string][]string{}
	for rel, m := range idx.Pages {
		slug := slugOf(rel)
		for _, l := range m.OutLinks {
			if strings.HasPrefix(l, "raw/") {
				continue
			}
			if _, ok := idx.slugIndex[strings.TrimSuffix(l, ".md")]; !ok {
				out[slug] = append(out[slug], l)
			}
		}
	}
	return out
}

// OrphanSlugs 孤儿页(零入站链接)。
func (idx *Index) OrphanSlugs() []string {
	var out []string
	for slug := range idx.slugIndex {
		if len(idx.Back[slug]) == 0 {
			out = append(out, slug)
		}
	}
	sort.Strings(out)
	return out
}

// HasSlug 判断 slug 对应页面是否存在(断链检测用)。
func (idx *Index) HasSlug(slug string) bool {
	_, ok := idx.slugIndex[strings.TrimSuffix(slug, ".md")]
	return ok
}

// Stats 索引规模(诊断用)。
func (idx *Index) Stats() string {
	return fmt.Sprintf("索引:%d 页,%d 词条,%d 反链目标", len(idx.Pages), len(idx.Invert), len(idx.Back))
}

// ---- 辅助 ----

func (idx *Index) rel(absPath string) string {
	r, err := filepath.Rel(idx.root, absPath)
	if err != nil {
		return absPath
	}
	return r
}

// slugOf 页面 rel 路径 → slug(去 .md 后缀)。
func slugOf(rel string) string {
	return strings.TrimSuffix(filepath.Base(rel), ".md")
}

func snapshotPath(root string) string {
	return filepath.Join(root, ".kcp", "index.gob")
}
