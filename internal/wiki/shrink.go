// shrink:pre-commit 防线——概念/实体页 frontmatter sources 被错误替换(缩水)的检测。
// 对齐 scripts/check-sources-shrink.py:对比 git HEAD 与 staged 版。
package wiki

import (
	"fmt"
	"os/exec"
	"strings"
)

// CheckSourcesShrink 对比 git HEAD 与 staged 的 wiki/concepts|entities 页 sources 列表。
// 返回问题描述列表;无问题返回空切片。非 git 仓库或 git 失败返回错误。
func CheckSourcesShrink(root string) ([]string, error) {
	out, err := exec.Command("git", "-C", root, "diff", "--cached", "--name-only", "--diff-filter=ACM").Output()
	if err != nil {
		return nil, fmt.Errorf("git diff 失败: %w", err)
	}
	var problems []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		parts := strings.Split(line, "/")
		if len(parts) < 3 || parts[0] != "wiki" || parts[1] != "concepts" && parts[1] != "entities" {
			continue
		}
		if !strings.HasSuffix(line, ".md") {
			continue
		}
		head := gitShowSources(root, "HEAD:"+line)
		if head == nil {
			continue // 新文件,HEAD 不存在
		}
		staged := gitShowSources(root, ":0:"+line)
		if staged == nil {
			continue
		}
		if len(staged) < len(head) {
			missing := setDiff(head, staged)
			if len(missing) > 0 {
				problems = append(problems, fmt.Sprintf(
					"%s: sources 从 %d 缩水到 %d,丢失 %v", line, len(head), len(staged), missing))
			}
		}
	}
	return problems, nil
}

// gitShowSources 提取指定 git ref 版本的页面 frontmatter sources 列表;失败返回 nil。
func gitShowSources(root, ref string) []string {
	out, err := exec.Command("git", "-C", root, "show", ref).Output()
	if err != nil {
		return nil
	}
	vals, _ := ParseFrontmatter(string(out))
	return FrontmatterList(vals, "sources")
}

// setDiff 返回 head 中有但 staged 中没有的元素。
func setDiff(head, staged []string) []string {
	inStaged := map[string]bool{}
	for _, s := range staged {
		inStaged[s] = true
	}
	var missing []string
	for _, h := range head {
		if !inStaged[h] {
			missing = append(missing, h)
		}
	}
	return missing
}
