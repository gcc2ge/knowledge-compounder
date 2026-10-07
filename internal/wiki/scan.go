package wiki

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Status wiki 状态:页面计数 + 未编译 raw。
type Status struct {
	Sources         int      `json:"sources"`
	Concepts        int      `json:"concepts"`
	Entities        int      `json:"entities"`
	Synthesis       int      `json:"synthesis"`
	Notes           int      `json:"notes"`
	Total           int      `json:"total_pages"`
	Uncompiled      int      `json:"uncompiled"`
	UncompiledFiles []string `json:"uncompiled_files"`
}

// Scan 扫描 wiki 状态:计数各子目录页数,找出未编译的 raw 源。
func Scan(root string) Status {
	st := Status{}
	// 未编译 raw 判据:文件名主干是否出现在 wiki/sources/。单遍遍历同时完成计数与 compiled map。
	compiled := map[string]bool{}
	for _, p := range Pages(root) {
		switch filepath.Base(filepath.Dir(p)) {
		case "sources":
			st.Sources++
			compiled[filepath.Base(p)] = true
		case "concepts":
			st.Concepts++
		case "entities":
			st.Entities++
		case "synthesis":
			st.Synthesis++
		case "notes":
			st.Notes++
		}
	}
	st.Total = st.Sources + st.Concepts + st.Entities + st.Synthesis + st.Notes

	var uncompiled []string
	for _, dir := range []string{filepath.Join(root, "raw"), filepath.Join(root, "raw", "books")} {
		matches, err := filepath.Glob(filepath.Join(dir, "*.md"))
		if err != nil {
			continue
		}
		for _, m := range matches {
			if !compiled[filepath.Base(m)] {
				rel, _ := filepath.Rel(root, m)
				uncompiled = append(uncompiled, rel)
			}
		}
	}
	sort.Strings(uncompiled)
	st.Uncompiled = len(uncompiled)
	st.UncompiledFiles = uncompiled
	return st
}

// StatusJSON 输出 --json 兼容格式。
func StatusJSON(root string) (string, error) {
	b, err := json.MarshalIndent(Scan(root), "", "  ")
	return string(b), err
}

// StatusText 人读格式。
func StatusText(root string) string {
	st := Scan(root)
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("总页面数 " + strconv.Itoa(st.Total) + " | sources " + strconv.Itoa(st.Sources) +
		" / concepts " + strconv.Itoa(st.Concepts) + " / entities " + strconv.Itoa(st.Entities) +
		" / synthesis " + strconv.Itoa(st.Synthesis) + " / notes " + strconv.Itoa(st.Notes) + "\n")
	if st.Uncompiled > 0 {
		b.WriteString("未编译 raw: " + strconv.Itoa(st.Uncompiled) + "\n" + strings.Join(st.UncompiledFiles, "\n"))
	} else {
		b.WriteString("未编译 raw: 0\n")
	}
	b.WriteString("---")
	return b.String()
}
