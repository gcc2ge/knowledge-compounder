#!/usr/bin/env python3
"""
wiki-lint.py — 一次性扫描，全量分析

Phase 1: 扫描所有 wiki 页面，收集数据
Phase 2: 内存分析：断链、孤儿、格式、index、覆盖、矛盾、过时
Phase 3: 输出健康报告

用法:
  python3 scripts/wiki-lint.py               # 标准报告
  python3 scripts/wiki-lint.py --json        # JSON 输出（供 QA agent 解析）
  python3 scripts/wiki-lint.py --fix         # 自动修复安全类问题
  python3 scripts/wiki-lint.py --summary     # 仅输出概要（快速状态）
"""

import os
import re
import sys
import json
from datetime import datetime, date, timedelta
from collections import defaultdict
from pathlib import Path

BASE = Path(__file__).resolve().parent.parent  # project root
WIKI = BASE / "wiki"
RAW = BASE / "raw"
INDEX_FILE = WIKI / "index.md"
LOG_FILE = BASE / "log.md"
TODAY = date.today()
THIRTY_DAYS_AGO = TODAY - timedelta(days=30)


# ─── helpers ────────────────────────────────────────────────────────────────

def slug(path: Path) -> str:
    return path.with_suffix("").name


def read_file(path: Path) -> str:
    try:
        return path.read_text(encoding="utf-8")
    except Exception:
        return ""


def parse_frontmatter(text: str) -> dict:
    """手动解析 YAML frontmatter，不依赖 yaml 库"""
    m = re.match(r"^---\s*\n(.*?)\n---\s*\n", text, re.DOTALL)
    if not m:
        return {}
    fm = {}
    lines = m.group(1).split("\n")
    i = 0
    while i < len(lines):
        line = lines[i].strip()
        i += 1
        if not line or line.startswith("#"):
            continue
        if ":" in line:
            k, v = line.split(":", 1)
            k = k.strip()
            v = v.strip()
            # 多行 YAML 列表（下一行以 - 开头）
            if v == "" or v == "[]":
                arr = []
                while i < len(lines):
                    next_line = lines[i].strip()
                    if next_line.startswith("- "):
                        arr.append(next_line[2:].strip())
                        i += 1
                    else:
                        break
                fm[k] = arr if arr else []
            elif v.startswith("["):
                fm[k] = [x.strip().strip("'\"") for x in v.strip("[]").split(",") if x.strip()]
            else:
                fm[k] = v
    return fm


def extract_wikilinks(text: str) -> list[str]:
    """提取真正的 [[wikilink]]，过滤代码片段中的误匹配"""
    # 先移除行内代码和代码块，避免误匹配
    cleaned = re.sub(r"```[\s\S]*?```", "", text)  # 多行代码块
    cleaned = re.sub(r"`[^`]+`", "", cleaned)        # 行内代码
    links = []
    for m in re.finditer(r"\[\[([^\]]+?)\]\]", cleaned):
        target = m.group(1).split("|")[0].split("#")[0].strip()
        if not target:
            continue
        # 过滤代码片段
        if "'" in target or '"' in target:
            continue
        if target[0] in ("'", '"', "(", "[", "{", "`"):
            continue
        if re.search(r"[\d.]+[,\s][\d.]+", target) and not re.search(r"[a-zA-Z一-鿿]", target):
            continue
        if re.match(r"^[\d_]+$", target):
            continue
        links.append(target)
    return links


def extract_raw_paths(text: str) -> list[str]:
    """从 source 页提取 raw 路径"""
    fm = parse_frontmatter(text)
    paths = []
    sf = fm.get("source_files", [])
    if isinstance(sf, list):
        paths.extend(sf)
    elif isinstance(sf, str):
        paths.append(sf)
    for link in extract_wikilinks(text):
        if link.startswith("raw/") or link.startswith("/raw/"):
            paths.append(link.replace("/raw/", "raw/").lstrip("/"))
    return sorted(set(paths))


# 占位节内容检测（P3 lint 增强）
# 概念页专项 idiom：schema 允许的"暂无…"占位，但补全优先；命中即计入 placeholder debt
PLACEHOLDER_IDIOMS = [
    ("我的实践", "暂无自己的实践，待 synthesis 回灌"),
    ("张力与缺口", "暂无已发现的张力与缺口"),
    ("外部观点", "待补充外部观点来源"),
]
# 通用占位标记：任意页面出现都值得警示（TODO/待补）。
# 注意不含「占位」——在中文金融/工程语境它是正当术语（幂等"先占位再处理"、交易"抢位"、表格"占位数值"），误报远高于信号价值。
GENERIC_PLACEHOLDER = re.compile(r"待补|TODO|TBD|placeholder", re.IGNORECASE)


def code_spans(text: str) -> list:
    """返回所有多行代码块 (```) 的 (start, end) 区间，用于排除代码内的占位标记"""
    return [(m.start(), m.end()) for m in re.finditer(r"```[\s\S]*?```", text)]


def section_of(text: str, pos: int) -> str:
    """返回 pos 前最近的 ## 节名"""
    prev = None
    for m in re.finditer(r"^## (.+)", text, re.M):
        if m.start() > pos:
            break
        prev = m.group(1)
    return prev if prev else "(frontmatter/顶部)"


# ─── Phase 1: Scan ──────────────────────────────────────────────────────────

def scan():
    pages = {}
    raw_paths_in_sources = set()
    slug_conflicts = []

    for fpath in sorted(WIKI.rglob("*.md")):
        if "health" in fpath.parts:
            continue  # 排除 wiki/health/ 健康报告目录
        rel = fpath.relative_to(WIKI)
        s = slug(fpath)
        text = read_file(fpath)
        fm = parse_frontmatter(text)
        links = extract_wikilinks(text)
        wiki_links = [l for l in links if not l.startswith("raw/") and not l.startswith("/raw/")]

        if s in pages:
            slug_conflicts.append((s, pages[s]["path"], str(rel)))

        pages[s] = {
            "path": str(rel),
            "abs_path": str(fpath),
            "text": text,
            "fm": fm,
            "links": wiki_links,
            "inbound": set(),
            "type": fm.get("type", "unknown"),
            "compiled": fm.get("compiled", ""),
            "updated": fm.get("updated", ""),
            "confidence": fm.get("confidence", ""),
            "has_source_files": "source_files" in fm,
            "has_contradiction": bool(re.search(
                r"(?:<!--\s*CONTRADICTION\s*-->)|(?:⚠️\s*(?:矛盾声明|冲突))|(?:^##\s*(?:矛盾|冲突)(?:与|及|和)?.*$)",
                text
            )),
        }

        # 收集 source 页引用的 raw 路径
        if fm.get("type") == "source":
            raw_paths_in_sources.update(extract_raw_paths(text))

    # 建立入站链接
    for s, p in pages.items():
        for link in p["links"]:
            if link in pages:
                pages[link]["inbound"].add(s)

    # 扫描 raw 文件
    raw_files = set()
    for fpath in sorted(RAW.rglob("*")):
        if fpath.is_dir():
            continue
        if "assets" in fpath.parts or "resume-arsenal" in fpath.parts:
            continue
        ext = fpath.suffix.lower()
        if ext in (".md", ".txt", ".pdf", ".png", ".jpg", ".jpeg", ".gif", ".svg"):
            raw_files.add(str(fpath.relative_to(BASE)))

    # 读取 index.md
    index_text = read_file(INDEX_FILE)
    index_links = extract_wikilinks(index_text)

    return pages, raw_files, raw_paths_in_sources, index_text, index_links, slug_conflicts


# ─── Phase 2: Analysis ──────────────────────────────────────────────────────

def analyze(pages, raw_files, raw_paths_in_sources, index_text, index_links, slug_conflicts):
    issues = []
    warnings = []
    source_slugs = {s for s, p in pages.items() if p["type"] == "source"}

    # ── 0. slug 冲突（同名文件，wikilink 歧义，需重命名）──────────────
    for s, p1, p2 in slug_conflicts:
        issues.append({
            "type": "slug_conflict",
            "severity": "error",
            "page": s,
            "detail": f"同名文件冲突：{p1} 与 {p2} 同 slug，Obsidian wikilink 有歧义，需重命名一个",
            "fixable": False,
        })

    # ── 2a. 断链检查 ──────────────────────────────────────────────────────
    for s, p in pages.items():
        for link in p["links"]:
            if link.startswith("raw/") or link.startswith("/raw/"):
                continue
            # Normalize: strip directory prefix (sources/, concepts/, entities/)
            # and check if the slug (filename) exists in pages dict
            target_slug = link.split("/")[-1]
            if link not in pages and target_slug not in pages:
                issues.append({
                    "type": "broken_link",
                    "severity": "error",
                    "page": s,
                    "detail": f"[[{link}]] 不存在",
                    "fixable": False,
                })

    # ── 2b. 孤儿页检查 ────────────────────────────────────────────────────
    for s, p in pages.items():
        if s == "index":
            continue
        real_inbound = [src for src in p["inbound"] if src != "index"]
        if len(real_inbound) == 0:
            detail = "零入站链接"
            if p["type"] == "source":
                detail += "（source 孤岛：可能有值得提取的概念）"
            issues.append({
                "type": "orphan",
                "severity": "warning",
                "page": s,
                "detail": detail,
                "fixable": False,
            })

    # ── 2c. 格式合规检查 ─────────────────────────────────────────────────
    for s, p in pages.items():
        fm = p["fm"]
        ptype = p["type"]

        if not fm.get("type"):
            issues.append({
                "type": "format",
                "severity": "error",
                "page": s,
                "detail": "缺少 frontmatter type 字段",
                "fixable": True,
                "fix": "type: source|concept|entity|synthesis",
            })

        if ptype == "source" and not p.get("compiled"):
            issues.append({
                "type": "format",
                "severity": "error",
                "page": s,
                "detail": "source 缺少 compiled 字段",
                "fixable": True,
                "fix": f"compiled: {TODAY}",
            })

        if ptype == "source" and not p.get("has_source_files"):
            issues.append({
                "type": "format",
                "severity": "error",
                "page": s,
                "detail": "source 缺少 source_files 字段",
                "fixable": False,
            })

        if ptype == "source" and not fm.get("origin"):
            issues.append({
                "type": "format",
                "severity": "error",
                "page": s,
                "detail": "source 缺少 origin 字段（external|self）",
                "fixable": True,
                "fix": "origin: external",
            })

        if ptype == "concept" and not p.get("confidence"):
            warnings.append({
                "type": "format",
                "severity": "warning",
                "page": s,
                "detail": "concept 缺少 confidence 字段",
                "fixable": True,
                "fix": "confidence: medium",
            })

        wiki_links = [l for l in p["links"] if l in pages]
        if len(wiki_links) < 2 and s != "index":
            warnings.append({
                "type": "format",
                "severity": "warning",
                "page": s,
                "detail": f"出站 wikilinks 不足 2 个（当前 {len(wiki_links)} 个）",
                "fixable": False,
            })

        if ptype == "source" and "意外发现" not in p["text"]:
            warnings.append({
                "type": "format",
                "severity": "warning",
                "page": s,
                "detail": "source 缺少「意外发现」一节",
                "fixable": False,
            })

        # 行锚定：正文中的 "#### 术语区分" 这类子标题不能算作「术语」节
        # （曾被子串误判为已通过，导致 118 个源实际全部缺失仍显示 1 个通过）
        if ptype == "source" and not re.search(r"^## 术语\s*$", p["text"], re.M):
            warnings.append({
                "type": "format",
                "severity": "warning",
                "page": s,
                "detail": "source 缺少「术语」一节（单次提及的概念/实体落地处）",
                "fixable": False,
            })

        if ptype == "entity":
            refs = set()
            for m in re.finditer(r"（来源：\[\[([^\]]+)\]\]）", p["text"]):
                refs.add(m.group(1))
            for l in p["links"]:
                if l in source_slugs:
                    refs.add(l)
            if len(refs) < 2:
                warnings.append({
                    "type": "format",
                    "severity": "warning",
                    "page": s,
                    "detail": f"实体页仅引用 {len(refs)} 个源（阈值 2+），单源实体考虑并入 source「术语」节",
                    "fixable": False,
                })

    # ── 2d. Index 一致性检查 ─────────────────────────────────────────────
    for s, p in pages.items():
        if s == "index":
            continue
        in_index = s in index_links or s in index_text
        if not in_index:
            issues.append({
                "type": "index_missing",
                "severity": "error",
                "page": s,
                "detail": "页面存在于 wiki/ 但未在 index.md 中列出",
                "fixable": True,
                "fix": "在 index.md 表格添加条目",
            })

    for link in index_links:
        # Normalize: strip directory prefix and check slug
        target_slug = link.split("/")[-1]
        if link not in pages and target_slug not in pages:
            issues.append({
                "type": "index_stale",
                "severity": "error",
                "page": "index",
                "detail": f"index.md 引用了 [[{link}]] 但页面不存在",
                "fixable": True,
                "fix": f"从 index.md 移除 [[{link}]]",
            })

    # 概要计数验证
    type_counts = defaultdict(int)
    for p in pages.values():
        type_counts[p["type"]] += 1
    type_counts["source_file"] = type_counts.get("source", 0)

    for label, key in [
        ("源文件数", "source"),
        ("概念页数", "concept"),
        ("实体页数", "entity"),
    ]:
        actual = type_counts.get(key, 0)
        m = re.search(rf"- {label}：(\d+)", index_text)
        if m:
            declared = int(m.group(1))
            if actual != declared:
                issues.append({
                    "type": "index_count",
                    "severity": "error",
                    "page": "index",
                    "detail": f"{label} 声明 {declared}，实际 {actual}",
                    "fixable": True,
                    "fix": f"{label}：{actual}",
                })

    # ── 2e. 覆盖检查 ─────────────────────────────────────────────────────
    # PDF 常是已编译 Markdown/书籍转换的中间产物；若同名 stem 或 arXiv ID
    # 已由 source 页引用，则不重复报告。统一状态口径见 scripts/wiki-status.py。
    compiled_stems = {Path(p).stem for p in raw_paths_in_sources}
    arxiv_re = re.compile(r"\d{4}\.\d{4,5}")
    compiled_ids = {m.group(0) for p in raw_paths_in_sources for m in [arxiv_re.search(p)] if m}
    uncompiled = []
    for path in sorted(raw_files - raw_paths_in_sources):
        p = Path(path)
        if p.suffix.lower() == ".pdf":
            stem = p.stem
            pdf_id = arxiv_re.search(stem)
            if (stem in compiled_stems or
                any(stem in c or c in stem for c in compiled_stems) or
                (pdf_id and pdf_id.group(0) in compiled_ids)):
                continue
        uncompiled.append(path)
    if uncompiled:
        warnings.append({
            "type": "uncompiled",
            "severity": "warning",
            "page": "raw/",
            "detail": f"{len(uncompiled)} 个 raw 文件未编译",
            "files": uncompiled,
            "fixable": False,
        })

    # ── 2f. 矛盾检测 ─────────────────────────────────────────────────────
    for s, p in pages.items():
        if p.get("has_contradiction"):
            issues.append({
                "type": "contradiction",
                "severity": "warning",
                "page": s,
                "detail": "页面包含矛盾/冲突标记，需人工审核",
                "fixable": False,
            })

    # ── 2g. 过时检查 ─────────────────────────────────────────────────────
    for s, p in pages.items():
        last_date = p.get("updated") or p.get("compiled")
        if last_date:
            try:
                d = datetime.strptime(last_date, "%Y-%m-%d").date()
                if d < THIRTY_DAYS_AGO:
                    warnings.append({
                        "type": "outdated",
                        "severity": "warning",
                        "page": s,
                        "detail": f"最后更新 {last_date}（超过 30 天）",
                        "fixable": False,
                    })
            except ValueError:
                pass

    # ── 2h. 知识网络健康检查（借鉴 health-check.md）────────────────────────
    # 概念页单源（脆弱概念）
    for s, p in pages.items():
        if p["type"] != "concept":
            continue
        srcs = p["fm"].get("sources", [])
        if isinstance(srcs, str):
            srcs = [srcs]
        if len(srcs) < 2:
            warnings.append({
                "type": "single_source_concept",
                "severity": "warning",
                "page": s,
                "detail": f"概念页仅 {len(srcs)} 个来源（脆弱概念，建议补充佐证源）",
                "fixable": False,
            })

    # 概念页未迁移新模板（缺 我的实践/外部观点/张力与缺口）
    for s, p in pages.items():
        if p["type"] != "concept":
            continue
        have = set(re.findall(r"^## (.+)", p["text"], re.M))
        miss = {"我的实践", "外部观点", "张力与缺口"} - have
        if miss:
            warnings.append({
                "type": "concept_template",
                "severity": "warning",
                "page": s,
                "detail": f"概念页缺新模板节 {sorted(miss)}（未迁移，origin 双轨未生效）",
                "fixable": False,
            })

    # ── 2i. 占位节内容检测（P3）─────────────────────────────────────────────
    # 概念页专项 idiom：schema 允许的占位语，但意味着该节无真实内容，补全有优先级
    for s, p in pages.items():
        if p["type"] != "concept":
            continue
        for section, idiom in PLACEHOLDER_IDIOMS:
            if idiom in p["text"]:
                warnings.append({
                    "type": "placeholder_section",
                    "severity": "warning",
                    "page": s,
                    "section": section,
                    "detail": f"「{section}」为占位内容：「{idiom}」",
                    "fixable": False,
                })
    # 通用占位标记：任意页面出现 TODO/待补/占位 都值得警示（跳过已由 idiom 覆盖的匹配）
    for s, p in pages.items():
        idiom_spans = []
        for _section, idiom in PLACEHOLDER_IDIOMS:
            for m in re.finditer(re.escape(idiom), p["text"]):
                idiom_spans.append((m.start(), m.end()))
        code_sp = code_spans(p["text"])
        for m in GENERIC_PLACEHOLDER.finditer(p["text"]):
            if any(a <= m.start() < b for a, b in idiom_spans):
                continue  # 已由专项 idiom 覆盖
            if any(a <= m.start() < b for a, b in code_sp):
                continue  # 代码块内的 placeholder/TODO 是源码语汇，非 wiki 占位债
            warnings.append({
                "type": "placeholder_section",
                "severity": "warning",
                "page": s,
                "section": section_of(p["text"], m.start()),
                "detail": f"含通用占位标记「{m.group(0)}」（节：{section_of(p['text'], m.start())}）",
                "fixable": False,
            })

    # 「相关」节双向链接（回归防线：保证互链对称性不被未来编译破坏）
    rel_adj = {}
    for s, p in pages.items():
        if p["type"] not in ("concept", "entity"):
            continue
        m = re.search(r"^## 相关\s*\n(.*?)(?=\n## )", p["text"], re.S | re.M)
        if m:
            rel_adj[s] = {l.split("|")[0].strip() for l in extract_wikilinks(m.group(1))}
        else:
            rel_adj[s] = set()
    one_way = [(a, b) for a in rel_adj for b in rel_adj[a]
               if b in rel_adj and a not in rel_adj[b]]
    if one_way:
        warnings.append({
            "type": "one_way_link",
            "severity": "warning",
            "page": "wiki（相关节）",
            "detail": f"{len(one_way)} 对单向链接（「相关」节 A→B 但 B↛A）",
            "pairs": one_way,
            "fixable": False,
        })

    # 候选概念：术语节聚合（≥2 次出现且无独立概念页）
    term_counts = defaultdict(int)
    for s, p in pages.items():
        if p["type"] != "source":
            continue
        m = re.search(r"^## 术语\s*\n(.*?)(?=\n## )", p["text"], re.S | re.M)
        if not m:
            continue
        for line in m.group(1).split("\n"):
            line = line.strip()
            tm = re.match(r"^- \*\*(.+?)\*\*", line) or re.match(r"^- (.+?)[：:]", line)
            if tm and tm.group(1).strip():
                term_counts[tm.group(1).strip()] += 1
    candidates = [(t, n) for t, n in term_counts.items() if n >= 2 and t not in pages]
    if candidates:
        warnings.append({
            "type": "candidate_concept",
            "severity": "warning",
            "page": "wiki（术语节聚合）",
            "detail": f"{len(candidates)} 个候选概念（术语节 ≥2 次出现但无独立概念页）",
            "candidates": candidates,
            "fixable": False,
        })

    # slug 大小写变体（回归防线）
    slugs_lower = defaultdict(list)
    for s in pages:
        if s != "index":
            slugs_lower[s.lower()].append(s)
    for low, v in slugs_lower.items():
        if len(v) > 1:
            warnings.append({
                "type": "slug_variant",
                "severity": "warning",
                "page": str(v),
                "detail": f"slug 大小写变体共存：{v}",
                "fixable": False,
            })

    return issues, warnings, type_counts, uncompiled


# ─── Phase 3: Report ────────────────────────────────────────────────────────

def generate_report(issues, warnings, type_counts, uncompiled, pages, output_json=False, summary_only=False):
    errors = [i for i in issues if i["severity"] == "error"]
    warns = [i for i in issues if i["severity"] == "warning"] + warnings

    if output_json:
        return json.dumps({
            "ok": len(errors) == 0,
            "errors": len(errors),
            "warnings_count": len(warns),
            "total_pages": len(pages) - (1 if "index" in pages else 0),
            "sources": type_counts.get("source", 0),
            "concepts": type_counts.get("concept", 0),
            "entities": type_counts.get("entity", 0),
            "issues": issues,
            "warning_items": warnings,
        }, ensure_ascii=False, indent=2)

    # 分组
    broken = [i for i in issues if i["type"] == "broken_link"]
    orphans = [i for i in issues if i["type"] == "orphan"]
    slug_conf = [i for i in issues if i["type"] == "slug_conflict"]
    format_issues = [i for i in issues if i["type"] == "format"]
    format_warnings = [w for w in warnings if w["type"] == "format"]
    index_missing = [i for i in issues if i["type"] == "index_missing"]
    index_stale = [i for i in issues if i["type"] == "index_stale"]
    index_count = [i for i in issues if i["type"] == "index_count"]
    contradictions = [i for i in issues if i["type"] == "contradiction"]
    outdated = [w for w in warnings if w["type"] == "outdated"]
    uncompiled_warn = [w for w in warnings if w["type"] == "uncompiled"]
    single_src = [w for w in warnings if w["type"] == "single_source_concept"]
    template_old = [w for w in warnings if w["type"] == "concept_template"]
    one_way = [w for w in warnings if w["type"] == "one_way_link"]
    cands = [w for w in warnings if w["type"] == "candidate_concept"]
    variants = [w for w in warnings if w["type"] == "slug_variant"]
    placeholders = [w for w in warnings if w["type"] == "placeholder_section"]

    if summary_only:
        page_count = len(pages) - (1 if "index" in pages else 0)
        s = f"# Wiki Health — {TODAY}\n"
        s += f"✅ {page_count} pages | {type_counts.get('source', 0)}s / {type_counts.get('concept', 0)}c / {type_counts.get('entity', 0)}e / {type_counts.get('synthesis', 0)}syn\n"
        s += f"❌ {len(errors)} errors | ⚠️ {len(warns)} warnings\n"
        if broken:
            s += f"  断链 {len(broken)}"
        if orphans:
            s += f" | 孤儿 {len(orphans)}"
        if uncompiled:
            s += f" | 未编译 {len(uncompiled)}"
        if contradictions:
            s += f" | 矛盾 {len(contradictions)}"
        if outdated:
            s += f" | 过时 {len(outdated)}"
        if placeholders:
            s += f" | 占位节 {len(placeholders)}"
        s += "\n"
        return s

    page_count = len(pages) - (1 if "index" in pages else 0)
    report = f"""# Wiki Health Report — {TODAY}

## 概要
- ✅ 总页面数：{page_count}（sources: {type_counts.get('source', 0)} / concepts: {type_counts.get('concept', 0)} / entities: {type_counts.get('entity', 0)} / synthesis: {type_counts.get('synthesis', 0)}）
- ❌ 错误：{len(errors)} 个
- ⚠️ 警告：{len(warns)} 个
"""

    if broken:
        report += f"\n## 断链（{len(broken)} 个）\n"
        for b in broken:
            report += f"- ❌ [[{b['page']}]] → [[{b['detail'].replace('[[', '').replace(']]', '')}]]\n"

    if orphans:
        report += f"\n## 孤儿页（{len(orphans)} 个）\n"
        for o in orphans:
            report += f"- ⚠️ [[{o['page']}]] — {o['detail']}\n"

    if slug_conf:
        report += f"\n## slug 冲突（{len(slug_conf)} 个）\n同名文件共存，wikilink 有歧义，需重命名一个：\n"
        for sc in slug_conf:
            report += f"- ❌ [[{sc['page']}]] — {sc['detail']}\n"

    if format_issues:
        report += f"\n## 格式错误（{len(format_issues)} 个）\n"
        for f_ in format_issues:
            report += f"- ❌ [[{f_['page']}]] — {f_['detail']}\n"

    if format_warnings:
        report += f"\n## 格式警告（{len(format_warnings)} 个）\n"
        for fw in format_warnings:
            report += f"- ⚠️ [[{fw['page']}]] — {fw['detail']}\n"

    if index_missing:
        report += f"\n## Index 缺失（{len(index_missing)} 个）\n"
        for im in index_missing:
            report += f"- ❌ [[{im['page']}]] — {im['detail']}\n"

    if index_stale:
        report += f"\n## Index 过期条目（{len(index_stale)} 个）\n"
        for is_ in index_stale:
            report += f"- ❌ [[{is_['page']}]] — {is_['detail']}\n"

    if index_count:
        report += f"\n## Index 计数不一致\n"
        for ic in index_count:
            report += f"- ❌ {ic['detail']}\n"

    if contradictions:
        report += f"\n## 矛盾标记（{len(contradictions)} 个）\n"
        for c in contradictions:
            report += f"- ⚠️ [[{c['page']}]] — {c['detail']}\n"

    if outdated:
        report += f"\n## 可能过时（{len(outdated)} 个）\n"
        for o in outdated:
            report += f"- ⚠️ [[{o['page']}]] — {o['detail']}\n"

    if uncompiled_warn:
        for uw in uncompiled_warn:
            report += f"\n## 未编译源（{len(uncompiled)} 个）\n"
            # 只显示前 30 个，避免报告太长
            shown = uw['files'][:30]
            for f in shown:
                report += f"-   {f}\n"
            if len(uw['files']) > 30:
                report += f"-   ... 还有 {len(uw['files']) - 30} 个\n"

    if placeholders:
        by_sec = defaultdict(list)
        for w in placeholders:
            by_sec[w.get("section", "通用")].append(w["page"])
        report += f"\n## 占位节内容（{len(placeholders)} 处）\n按节聚合，补全优先级参考：\n"
        for sec in sorted(by_sec, key=lambda s: -len(by_sec[s])):
            pl = by_sec[sec]
            shown = "、".join(f"[[{x}]]" for x in pl[:12])
            more = f" … 还有 {len(pl) - 12} 个" if len(pl) > 12 else ""
            report += f"- 「{sec}」{len(pl)} 处：{shown}{more}\n"

    report += f"\n## 建议操作\n"
    actions = []
    if broken:
        actions.append(f"1. 修复 {len(broken)} 个断链")
    if orphans:
        actions.append(f"2. 审核 {len(orphans)} 个孤儿页是否保留")
    if index_missing or index_stale:
        actions.append(f"3. 同步 index.md 与实际页面")
    if uncompiled:
        actions.append(f"4. 编译 {len(uncompiled)} 个未编译 raw 源")
    if outdated:
        actions.append(f"5. 更新 {len(outdated)} 个可能过时的页面")
    if single_src:
        actions.append(f"6. 为 {len(single_src)} 个单源概念补充佐证源")
    if template_old:
        actions.append(f"7. 迁移 {len(template_old)} 个概念页到新模板（origin 双轨）")
    if slug_conf:
        actions.append(f"8. 重命名 {len(slug_conf)} 个 slug 冲突文件（同名概念/源页）")
    if placeholders:
        actions.append(f"9. 补全 {len(placeholders)} 个占位节内容（见上方，优先「外部观点」「我的实践」）")
    if not actions:
        actions.append("✓ 无待办事项，wiki 健康")
    report += "\n".join(actions) + "\n"

    # 知识网络检查分组（借鉴 health-check.md）
    net = []
    if single_src:
        net.append(f"\n## 单源概念（{len(single_src)} 个）\n脆弱概念，建议补充佐证源：")
        for w in single_src[:40]:
            net.append(f"- ⚠️ [[{w['page']}]] — {w['detail']}")
        if len(single_src) > 40:
            net.append(f"- ... 还有 {len(single_src) - 40} 个")
    if template_old:
        net.append(f"\n## 概念页未迁移新模板（{len(template_old)} 个）\n缺 我的实践/外部观点/张力与缺口：")
        for w in template_old[:40]:
            net.append(f"- ⚠️ [[{w['page']}]] — {w['detail']}")
        if len(template_old) > 40:
            net.append(f"- ... 还有 {len(template_old) - 40} 个")
    if one_way:
        pairs = one_way[0].get("pairs", [])
        net.append(f"\n## 单向链接（{len(pairs)} 对）\n「相关」节 A→B 但 B↛A：")
        for a, b in pairs[:15]:
            net.append(f"- ⚠️ [[{a}]] → [[{b}]]")
        if len(pairs) > 15:
            net.append(f"- ... 还有 {len(pairs) - 15} 对")
    if cands:
        cl = cands[0].get("candidates", [])
        net.append(f"\n## 候选概念（{len(cl)} 个）\n术语节 ≥2 次出现但无独立概念页：")
        for t, n in cl[:15]:
            net.append(f"- 💡 {t}（{n} 次出现）")
        if len(cl) > 15:
            net.append(f"- ... 还有 {len(cl) - 15} 个")
    if variants:
        net.append(f"\n## slug 大小写变体（{len(variants)} 组）")
        for w in variants:
            net.append(f"- ⚠️ {w['page']}")
    if net:
        report += "\n".join(net) + "\n"

    return report


# ─── Health Report Persistence（借鉴 health-check.md：落盘 + 前后比较）──────

def build_stats(pages, issues, warnings, uncompiled):
    return {
        "total_pages": len(pages),
        "sources": sum(1 for p in pages.values() if p["type"] == "source"),
        "concepts": sum(1 for p in pages.values() if p["type"] == "concept"),
        "entities": sum(1 for p in pages.values() if p["type"] == "entity"),
        "errors": sum(1 for i in issues if i["severity"] == "error"),
        "warnings": len(issues) + len(warnings),
        "single_source_concepts": sum(1 for w in warnings if w["type"] == "single_source_concept"),
        "unmigrated_concepts": sum(1 for w in warnings if w["type"] == "concept_template"),
        "one_way_links": sum(len(w.get("pairs", [])) for w in warnings if w["type"] == "one_way_link"),
        "placeholder_sections": sum(1 for w in warnings if w["type"] == "placeholder_section"),
        "uncompiled": len(uncompiled),
    }


def read_prev_stats(health_dir, today_iso):
    # 注意：不跳过同日文件——write_health_report 是"先读基线再写"，
    # 磁盘上的同日文件正是上次报告的状态，跳过它会导致同日重跑基线丢失（占位债等指标无法追踪）
    prevs = sorted(health_dir.glob("*.md"))
    for f in reversed(prevs):
        fm = parse_frontmatter(read_file(f))
        raw = fm.get("stats", "")
        if isinstance(raw, str) and raw.strip():
            try:
                return json.loads(raw), f.name
            except Exception:
                pass
    return None, None


def diff_section(prev_stats, stats):
    if not prev_stats:
        return "## 与上次比较\n（首次报告，无历史基线）\n"
    lines = ["## 与上次比较", ""]
    for k in ["total_pages", "sources", "concepts", "entities", "errors",
              "warnings", "single_source_concepts", "unmigrated_concepts",
              "one_way_links", "placeholder_sections", "uncompiled"]:
        if k in prev_stats and k in stats:
            pv, cv = prev_stats[k], stats[k]
            arrow = "→" if cv == pv else ("▲ 上升" if cv > pv else "▼ 下降")
            lines.append(f"- {k}: {pv} → {cv} {arrow}")
    return "\n".join(lines) + "\n"


def write_health_report(report, pages, issues, warnings, uncompiled):
    health_dir = WIKI / "health"
    health_dir.mkdir(exist_ok=True)
    today_iso = TODAY.isoformat()
    stats = build_stats(pages, issues, warnings, uncompiled)
    prev_stats, prev_name = read_prev_stats(health_dir, today_iso)
    diff = diff_section(prev_stats, stats)
    header = (f"---\ndate: {today_iso}\nscope: wiki/\n"
              f"stats: {json.dumps(stats, ensure_ascii=False)}\n---\n\n")
    path = health_dir / f"{today_iso}.md"
    path.write_text(header + diff + report, encoding="utf-8")
    baseline = f"（对比基线：{prev_name}）" if prev_name else "（首次报告，无基线）"
    return path, baseline


# ─── Fix Mode ───────────────────────────────────────────────────────────────

def auto_fix(pages, issues, index_text):
    fixed = 0
    for issue in issues:
        if not issue.get("fixable"):
            continue
        page_path = pages[issue["page"]]["abs_path"]
        text = pages[issue["page"]]["text"]
        fm = pages[issue["page"]]["fm"]

        if "缺少 frontmatter type" in issue["detail"]:
            if not fm:
                ptype = ("source" if "/sources/" in page_path
                         else "concept" if "/concepts/" in page_path
                         else "entity" if "/entities/" in page_path
                         else "synthesis")
                Path(page_path).write_text(f"---\ntype: {ptype}\n---\n\n{text}")
                fixed += 1
                print(f"  ✅ [[{issue['page']}]] 添加 frontmatter type={ptype}", file=sys.stderr)

        elif "缺少 compiled 字段" in issue["detail"]:
            new_text = text.replace("---\n", f"---\ncompiled: {TODAY}\n", 1)
            Path(page_path).write_text(new_text)
            fixed += 1
            print(f"  ✅ [[{issue['page']}]] 添加 compiled: {TODAY}", file=sys.stderr)

        elif "缺少 confidence 字段" in issue["detail"]:
            new_text = text.replace("---\n", "---\nconfidence: medium\n", 1)
            Path(page_path).write_text(new_text)
            fixed += 1
            print(f"  ✅ [[{issue['page']}]] 添加 confidence: medium", file=sys.stderr)

        elif "缺少 origin 字段" in issue["detail"]:
            new_text = text.replace("---\n", "---\norigin: external\n", 1)
            Path(page_path).write_text(new_text)
            fixed += 1
            print(f"  ✅ [[{issue['page']}]] 添加 origin: external", file=sys.stderr)

    # 修复 index 计数
    for issue in issues:
        if issue["type"] == "index_count" and issue.get("fixable"):
            detail = issue["detail"]
            m = re.match(r"(.+?)声明 (\d+)，实际 (\d+)", detail)
            if m:
                label = m.group(1).strip()
                actual = m.group(3)
                old = re.search(rf"(- {re.escape(label)}：)(\d+)", index_text)
                if old:
                    index_text = index_text.replace(old.group(0), f"{old.group(1)}{actual}")
                    INDEX_FILE.write_text(index_text)
                    fixed += 1
                    print(f"  ✅ 修正 index 计数: {label} → {actual}", file=sys.stderr)

    return fixed


# ─── Main ────────────────────────────────────────────────────────────────────

def main():
    flags = set(sys.argv[1:])
    fix_mode = "--fix" in flags
    json_mode = "--json" in flags
    summary_mode = "--summary" in flags

    pages, raw_files, raw_paths_in_sources, index_text, index_links, slug_conflicts = scan()
    issues, warnings, type_counts, uncompiled = analyze(
        pages, raw_files, raw_paths_in_sources, index_text, index_links, slug_conflicts)

    if fix_mode:
        print("🔧 自动修复...", file=sys.stderr)
        fixed = auto_fix(pages, issues, index_text)
        print(f"   {'✅ 完成' if fixed == 0 else f'✅ 修复 {fixed} 项'}", file=sys.stderr)
        # 重新扫描
        pages, raw_files, raw_paths_in_sources, index_text, index_links, slug_conflicts = scan()
        issues, warnings, type_counts, uncompiled = analyze(
            pages, raw_files, raw_paths_in_sources, index_text, index_links, slug_conflicts)

    report = generate_report(issues, warnings, type_counts, uncompiled, pages,
                             output_json=json_mode, summary_only=summary_mode)
    print(report)

    # 落盘健康报告 + 前后比较（--json 模式下也写 markdown 版）
    if not summary_mode:
        md_report = report if not json_mode else generate_report(
            issues, warnings, type_counts, uncompiled, pages, output_json=False)
        path, baseline = write_health_report(md_report, pages, issues, warnings, uncompiled)
        print(f"📊 健康报告已保存：{path.relative_to(BASE)} {baseline}", file=sys.stderr)


if __name__ == "__main__":
    main()
