#!/usr/bin/env python3
"""
source-skeleton.py — Generate source page skeleton (frontmatter + template sections).

Usage:
  python3 scripts/source-skeleton.py generate <raw_path> [--tags tag1,tag2] [--origin external|self]
  python3 scripts/source-skeleton.py template                      # Print the template structure

This generates the deterministic parts of a source page (frontmatter + section headers)
so the LLM only needs to fill in the content, not the structure.
"""

import json
import os
import re
import sys
from datetime import date
from pathlib import Path

WIKI_DIR = Path(__file__).resolve().parent.parent / "wiki"
RAW_DIR = Path(__file__).resolve().parent.parent / "raw"
SOURCES_DIR = WIKI_DIR / "sources"


def slugify(filename):
    """Convert a raw filename to a source page slug."""
    name = Path(filename).stem
    # Remove common prefixes
    name = re.sub(r'^[\d\-_\[\]【】（）()]+', '', name)
    # Keep it short but meaningful
    if len(name) > 60:
        name = name[:60]
    return name


def extract_raw_info(raw_path):
    """Extract useful info from the raw file."""
    path = Path(raw_path)
    info = {
        "filename": path.name,
        "stem": path.stem,
        "source_files": [str(path.relative_to(path.parent.parent)) if path.is_absolute() else path.name],
    }
    # Try to read first line for title
    try:
        content = path.read_text(encoding="utf-8")
        first_line = content.split("\n", 1)[0].strip()
        if first_line.startswith("# "):
            info["title"] = first_line[2:].strip()
        elif first_line.startswith("#"):
            info["title"] = first_line[1:].strip()
        else:
            info["title"] = path.stem
    except Exception:
        info["title"] = path.stem
    return info


def generate_skeleton(raw_path, tags=None, origin="external"):
    """Generate the source page skeleton."""
    raw_info = extract_raw_info(raw_path)

    if tags:
        tag_list = [t.strip() for t in tags.split(",") if t.strip()]
    else:
        tag_list = []

    # Determine slug
    slug = slugify(raw_info["stem"])

    # Build frontmatter
    frontmatter = {
        "source_files": raw_info["source_files"],
        "origin": origin,
        "compiled": date.today().isoformat(),
        "type": "source",
        "tags": tag_list,
    }

    # Format frontmatter as YAML
    fm_lines = ["---"]
    for key, val in frontmatter.items():
        if isinstance(val, list):
            if val:
                fm_lines.append(f"{key}:")
                for v in val:
                    fm_lines.append(f"  - {v}")
            else:
                fm_lines.append(f"{key}: []")
        else:
            fm_lines.append(f"{key}: {val}")
    fm_lines.append("---")

    # Build template sections
    sections = [
        f"# {raw_info['title']}",
        "",
        "## 一句话结论",
        "",
        "> 用一段话概括全文最核心的论点，让不读 raw 的人也能带走结论",
        "",
        "## 论证链",
        "",
        "> 按作者论证顺序写出逻辑链条，每步附关键引用。保留作者的核心框架、推理过程、关键转折点。",
        "",
        "**论证链中的每个环节必须直接包含 raw 对应位置的所有具体内容**，包括但不限于：",
        "- **建表 SQL / 查询 SQL / 伪代码 / 算法**：逐字保留原文或精确改写",
        "- **对比表 / 选型表 / 特征矩阵**：保留整表",
        "- **业务分录 / 输入输出示例**：每个场景保留完整样例",
        "- **公式 / 校验逻辑**：保留完整表达式",
        "- **运行结果 / 数值输出**：保留原文中的结果",
        "",
        "## 关键细节",
        "",
        "> 具体的数字、参数、配置项、阈值、API 字段名、协议细节。不放论证，只放可独立引用的工程事实。",
        "",
        "## 作者立场与定位",
        "",
        "> 作者是谁、什么视角、核心假设、结论的适用范围和局限性。",
        "",
        "## 意外发现",
        "",
        "> 与预期不符的、最有价值的信息。",
        "",
        "## 疑点",
        "",
        '> 本源的未验证主张、有争议处、不确定处。论点支撑充分时写「本源的论点有充分支撑，未发现重大疑点」。',
        "",
        "## 术语",
        "",
        "> 本源引入的重要术语，附简短定义。单次提及的概念/实体在此落地，不单独建页。",
        "",
        "## 连接",
        "",
        "> 指向相关概念页和实体页的 [[wikilinks]]，附具体关联说明。",
        "",
        "## 引用",
        "",
        f"- 原文位置：[[{raw_info['source_files'][0]}]]",
        "",
    ]

    return "\n".join(fm_lines + [""] + sections)


def cmd_generate():
    if len(sys.argv) < 3:
        print("Usage: python3 scripts/source-skeleton.py generate <raw_path> [--tags tag1,tag2] [--origin external|self]", file=sys.stderr)
        sys.exit(1)

    raw_path = sys.argv[2]
    tags = None
    origin = "external"

    i = 3
    while i < len(sys.argv):
        if sys.argv[i] == "--tags" and i + 1 < len(sys.argv):
            tags = sys.argv[i + 1]
            i += 2
        elif sys.argv[i] == "--origin" and i + 1 < len(sys.argv):
            origin = sys.argv[i + 1]
            i += 2
        else:
            i += 1

    # Verify raw file exists
    raw_path_obj = Path(raw_path)
    if not raw_path_obj.exists():
        # Try relative to repo root
        repo_root = Path(__file__).resolve().parent.parent
        raw_path_obj = repo_root / raw_path
        if not raw_path_obj.exists():
            print(f"Error: raw file not found: {raw_path}", file=sys.stderr)
            sys.exit(1)

    skeleton = generate_skeleton(str(raw_path_obj), tags, origin)
    print(skeleton)


def cmd_template():
    """Print the template structure only."""
    print("""Source page template structure (deterministic, no LLM needed):

---                    ← auto-generated frontmatter
source_files: [raw/...]
origin: external|self
compiled: YYYY-MM-DD  ← auto (today)
type: source
tags: [tag1, tag2]    ← from CLI args
---

# [Title]  ← from raw file first line

## 一句话结论
## 论证链
## 关键细节
## 作者立场与定位
## 意外发现
## 疑点
## 术语
## 连接
## 引用

Sections are fixed. LLM only fills content under each section head.
""")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__.strip())
        sys.exit(0)

    cmd = sys.argv[1]
    handlers = {
        "generate": cmd_generate,
        "template": cmd_template,
    }
    handler = handlers.get(cmd)
    if handler:
        handler()
    else:
        print(f"Unknown command: {cmd}", file=sys.stderr)
        sys.exit(1)