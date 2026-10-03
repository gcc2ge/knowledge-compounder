// skeleton:从 raw 生成 source 页骨架(无 LLM)。对齐 source-skeleton.py。
package wiki

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Skeleton 从 raw 文件生成 source 页骨架文本。
// tags 以逗号分隔;origin 为空时默认 external。
func Skeleton(root, rawPath, tags, origin string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, rawPath))
	if err != nil {
		return "", err
	}
	title := firstHeading(string(b))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(rawPath), filepath.Ext(rawPath))
	}
	if origin == "" {
		origin = "external"
	}
	var tagList []string
	for _, t := range strings.Split(tags, ",") {
		if t = strings.TrimSpace(t); t != "" {
			tagList = append(tagList, t)
		}
	}
	vals := map[string]any{
		"source_files": []string{rawPath},
		"origin":       origin,
		"compiled":     time.Now().Format("2006-01-02"),
		"type":         "source",
		"tags":         tagList,
	}
	sections := strings.Join([]string{
		"# " + title,
		"",
		"## 一句话结论",
		"> 用一段话概括全文最核心的论点,让不读 raw 的人也能带走结论",
		"",
		"## 论证链",
		"> 按作者论证顺序写出逻辑链条,每步附关键引用。保留作者的核心框架、推理过程、关键转折点。",
		"",
		"**论证链中的每个环节必须直接包含 raw 对应位置的所有具体内容**,包括:",
		"- **建表 SQL / 查询 SQL / 伪代码 / 算法**:逐字保留原文或精确改写",
		"- **对比表 / 选型表 / 特征矩阵**:保留整表",
		"- **业务分录 / 输入输出示例**:每个场景保留完整样例",
		"- **公式 / 校验逻辑**:保留完整表达式",
		"- **运行结果 / 数值输出**:保留原文中的结果",
		"",
		"## 关键细节",
		"> 具体的数字、参数、配置项、阈值、API 字段名、协议细节。不放论证,只放可独立引用的工程事实。",
		"",
		"## 作者立场与定位",
		"> 作者是谁、什么视角、核心假设、结论的适用范围和局限性。",
		"",
		"## 意外发现",
		"> 与预期不符的、最有价值的信息。写:原文说了什么 + 这在用户场景中意味着什么。",
		"",
		"## 疑点",
		"> 本源的未验证主张、有争议处、不确定处。论点支撑充分时写「本源的论点有充分支撑,未发现重大疑点」。",
		"",
		"## 术语",
		"> 本源引入的重要术语,附简短定义。单次提及的概念/实体在此落地,不单独建页。",
		"",
		"## 连接",
		"> 指向相关概念页和实体页的 [[wikilinks]],附具体关联意义。",
		"",
		"## 引用",
		"- 原文位置:[[raw/" + rawPath + "]]",
		"",
	}, "\n")
	return SerializeFrontmatter(vals) + "\n\n" + sections, nil
}

// firstHeading 取正文第一个 # 标题行。
func firstHeading(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if trim := strings.TrimSpace(line); strings.HasPrefix(trim, "# ") {
			return strings.TrimSpace(strings.TrimPrefix(trim, "# "))
		}
	}
	return ""
}
