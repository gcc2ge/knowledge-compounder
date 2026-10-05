// 读覆盖台账:每个文件已读行区间的确定性记录。
// 用途:compiler 退出前检查长源是否有「中间未读」gap——glm 实测只读头+尾就写页,
// 存在性判据与 preflight 硬资产都拦不住(浅页带一个代码块即可过检),
// 但读覆盖台账能确定性地抓住:>80KB 的 raw 中间存在未读区间 = 内容没读完 = 拒收。
package tools

// Coverage 记录已读行区间。Key = read_file 使用的相对路径(如 raw/xxx.md)。
type Coverage struct {
	files map[string]*fileCov
}

type fileCov struct {
	total  int // 文件总行数
	merged []span
}

// span 闭区间 [lo, hi](1 起始行号)。
type span struct{ lo, hi int }

// Mark 记录一次读取覆盖 [lo,hi] 并入并合并到已读区间表。
func (c *Coverage) Mark(path string, lo, hi, total int) {
	if c.files == nil {
		c.files = map[string]*fileCov{}
	}
	fc := c.files[path]
	if fc == nil {
		fc = &fileCov{total: total}
		c.files[path] = fc
	}
	if lo < 1 {
		lo = 1
	}
	if hi > total {
		hi = total
	}
	if lo > hi {
		return
	}
	cur := span{lo, hi}
	var out []span
	inserted := false
	for _, s := range fc.merged {
		if cur.hi < s.lo { // 完全在 s 之前
			if !inserted {
				out = append(out, cur)
				inserted = true
			}
			out = append(out, s)
		} else if cur.lo > s.hi { // 完全在 s 之后
			out = append(out, s)
		} else { // 相交 → 吞并
			if s.lo < cur.lo {
				cur.lo = s.lo
			}
			if s.hi > cur.hi {
				cur.hi = s.hi
			}
		}
	}
	if !inserted {
		out = append(out, cur)
	}
	fc.merged = out
}

// Gap 返回第一个未读区间 [lo,hi]。ok=true 表示存在未读区间。
// 文件从未被读过(无台账)或未知 → 返回 false(不阻断,交由存在性/质量判据)。
func (c *Coverage) Gap(path string) (lo, hi int, ok bool) {
	fc := c.files[path]
	if fc == nil || fc.total <= 0 {
		return 0, 0, false
	}
	cursor := 1
	for _, s := range fc.merged {
		if s.lo > cursor {
			return cursor, s.lo - 1, true
		}
		if s.hi >= cursor {
			cursor = s.hi + 1
		}
	}
	if cursor <= fc.total {
		return cursor, fc.total, true
	}
	return 0, 0, false
}
