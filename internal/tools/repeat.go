// 选择性深读(切片 B):模板化长源里逐字重复的段落,首次读入时已在上下文里,
// 之后的重复段不必再占上下文——read_file 分页读时按内容哈希检测重复段,
// 用紧凑标记替换,而 Coverage 照记整段已读(内容由首次读覆盖,FinishGuard 的
// gap 判据不受影响)。纯粹确定性判定,不靠模型自觉。
//
// 触发判据:连续 ≥minRepeatRun 行与已读内容逐字相同(模板段)。
// 短重复(< minRepeatRun 行)视为正常内容原样返回,防误杀惯用代码块/惯用短语。
package tools

import "hash/fnv"

const (
	repeatWindow  = 4 // 行窗口宽度(哈希粒度)
	minRepeatRun  = 8 // 连续多少行才算模板重复段(防误杀短重复)
	minRepeatWins = 5 // 最少连续命中的窗口数(= minRepeatRun - repeatWindow + 1)
)

// RepeatDetector 记录每个文件的行窗口哈希 → 首次出现的绝对起始行。
type RepeatDetector struct {
	files map[string]*fileRep
}

type fileRep struct {
	win map[uint64]int // 4 行窗口哈希 → 首次出现的起始行(1 起始)
}

// outSeg 一次分页读的输出段:普通内容(lo..hi 原样)或省略的重复段。
type outSeg struct {
	lo, hi int // 绝对行区间(1 起始,闭区间)
	first  int // 重复段:首次出现的起始行;0 = 普通内容
}

// compact 把 [offset,end] 的 raw 行经重复检测后切成输出段。
// lines 为文件全部行(lines[i] = 第 i+1 行);返回段覆盖 [offset,end]。
// 每次调用都会把新窗口登记进台账,供后续页检测。
func (d *RepeatDetector) compact(path string, lines []string, offset, end int) []outSeg {
	if d.files == nil {
		d.files = map[string]*fileRep{}
	}
	fr := d.files[path]
	if fr == nil {
		fr = &fileRep{win: map[uint64]int{}}
		d.files[path] = fr
	}

	// 第一遍:登记 + 标记命中的窗口(首次出现早于当前行 = 与已读内容重复)。
	// 防御:end 行数不足以构成 repeatWindow 窗口时,无窗口可检测,原样返回整段。
	lastStart := end - repeatWindow + 1
	if lastStart < offset {
		return []outSeg{{offset, end, 0}}
	}
	matched := make([]bool, lastStart-offset+1)
	for i := offset; i <= lastStart; i++ {
		h := windowHash(lines, i)
		first, ok := fr.win[h]
		if !ok {
			fr.win[h] = i
			continue
		}
		if first < i {
			matched[i-offset] = true
		}
	}

	// 第二遍:把连续命中的窗口合并成省略段;其余为普通内容。
	var segs []outSeg
	segLo := offset
	for i := offset; i <= lastStart; {
		if !matched[i-offset] {
			i++
			continue
		}
		j := i
		for j <= lastStart && matched[j-offset] {
			j++
		}
		// [i, j-1] 连续命中窗口 → 重复行区间 [i, j-1+repeatWindow-1]
		hi := j - 1 + repeatWindow - 1
		if hi-i+1 >= minRepeatRun {
			first := fr.win[windowHash(lines, i)]
			if segLo <= i-1 {
				segs = append(segs, outSeg{segLo, i - 1, 0})
			}
			segs = append(segs, outSeg{i, hi, first})
			i = hi + 1
			segLo = i
			continue
		}
		i = j
	}
	if segLo <= end {
		segs = append(segs, outSeg{segLo, end, 0})
	}
	return segs
}

// windowHash 对第 line 行起的 repeatWindow 行做 FNV-64 哈希(行内容,不含行号)。
// 行不足 repeatWindow 时返回 0(不参与检测——文件尾部残行不可能构成模板段)。
func windowHash(lines []string, line int) uint64 {
	if line+repeatWindow-1 > len(lines) {
		return 0
	}
	h := fnv.New64a()
	for i := line; i < line+repeatWindow; i++ {
		h.Write([]byte(lines[i-1]))
		h.Write([]byte{0})
	}
	return h.Sum64()
}
