// Package wiki 知识库操作:根定位、状态扫描、检索、lint。
package wiki

import (
	"fmt"
	"os"
	"path/filepath"
)

// Root 从当前目录向上找项目根(含 SCHEMA.md)。
func Root() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "SCHEMA.md")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("未找到项目根(向上未发现 SCHEMA.md),请在 knowledge-compounder 目录下运行")
		}
		dir = parent
	}
}

// Subdirs wiki 的子目录(用于扫描/检索)。
var Subdirs = []string{"sources", "concepts", "entities", "synthesis", "notes"}

// Pages 返回 wiki 子目录下所有 .md 页。
func Pages(root string) []string {
	var out []string
	for _, d := range Subdirs {
		matches, _ := filepath.Glob(filepath.Join(root, "wiki", d, "*.md"))
		out = append(out, matches...)
	}
	return out
}

// Read 读文件并容忍错误。
func Read(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}
