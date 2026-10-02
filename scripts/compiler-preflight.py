#!/usr/bin/env python3
"""Mechanical completeness checks for a raw source and its compiled source page.

This script deliberately does not judge arguments, concepts, contradictions, or
where knowledge belongs.  It gives the compiler a deterministic inventory of
hard assets that must not disappear during semantic compilation.
"""
from __future__ import annotations

import argparse
import json
import re
from collections import Counter
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent

FENCED_BLOCK_RE = re.compile(r"^```(?P<lang>[^\n]*)\n(?P<body>.*?)^```\s*$", re.M | re.S)
INDENTED_BLOCK_RE = re.compile(r"(?:^|\n)(?: {4}|\t).+(?:\n(?: {4}|\t).+)+", re.M)
TABLE_RE = re.compile(r"^\|.+\|\s*\n\|(?:\s*:?-{3,}:?\s*\|)+", re.M)
HTML_TABLE_RE = re.compile(r"<table\b", re.I)
MATH_DELIMITER_RE = re.compile(r"\$\$(?:.|\n)+?\$\$|\\\[(?:.|\n)+?\\\]|(?<!\$)\$(?!\s)(?:\\.|[^$\n])+?\$", re.S)
MATH_COMMAND_RE = re.compile(r"\\(?:frac|sum|prod|int|sqrt|alpha|beta|gamma|lambda|mathbb|begin|end)\b")
NUMBERED_EXAMPLE_RE = re.compile(r"^(?:Example|示例|案例|输入|输出|Case)\b", re.I | re.M)


def resolve(path: str) -> Path:
    p = Path(path)
    return p if p.is_absolute() else ROOT / p


def inventory(text: str) -> dict[str, object]:
    fenced = list(FENCED_BLOCK_RE.finditer(text))
    languages = Counter((m.group("lang").strip().lower() or "plain") for m in fenced)
    return {
        "fenced_code_blocks": len(fenced),
        "fenced_code_languages": dict(sorted(languages.items())),
        "indented_code_blocks": len(INDENTED_BLOCK_RE.findall(text)),
        "markdown_tables": len(TABLE_RE.findall(text)),
        "html_tables": len(HTML_TABLE_RE.findall(text)),
        "math_expressions": len(MATH_DELIMITER_RE.findall(text)) + len(MATH_COMMAND_RE.findall(text)),
        "numbered_examples": len(NUMBERED_EXAMPLE_RE.findall(text)),
        "characters": len(text),
        "lines": text.count("\n") + 1,
    }


def source_sections(text: str) -> set[str]:
    return set(re.findall(r"^##\s+(.+?)\s*$", text, re.M))


def compare(raw: dict[str, object], compiled: dict[str, object], sections: set[str]) -> list[dict[str, str]]:
    issues: list[dict[str, str]] = []
    for key in ("fenced_code_blocks", "markdown_tables", "html_tables", "math_expressions", "numbered_examples"):
        raw_count, page_count = int(raw[key]), int(compiled[key])
        if raw_count and page_count < raw_count:
            issues.append({
                "severity": "warning",
                "kind": "hard_asset_gap",
                "detail": f"{key}: raw={raw_count}, source={page_count}; inspect whether all hard assets were preserved.",
            })
    for section in ("一句话结论", "论证链", "关键细节", "作者立场与定位", "意外发现", "疑点", "术语", "连接", "引用"):
        if section not in sections:
            issues.append({"severity": "error", "kind": "missing_section", "detail": f"source 缺少「{section}」节"})
    return issues


def main() -> int:
    parser = argparse.ArgumentParser(description="Inventory compiler hard assets before/after semantic compilation.")
    parser.add_argument("raw", help="raw source path (absolute or relative to repository root)")
    parser.add_argument("--source-page", help="compiled wiki/sources page to compare")
    parser.add_argument("--json", action="store_true", help="emit structured JSON")
    args = parser.parse_args()

    raw_path = resolve(args.raw)
    if not raw_path.is_file():
        parser.error(f"raw source not found: {raw_path}")
    raw_text = raw_path.read_text(encoding="utf-8", errors="ignore")
    result: dict[str, object] = {"raw": str(raw_path.relative_to(ROOT)), "raw_inventory": inventory(raw_text)}
    exit_code = 0

    if args.source_page:
        page_path = resolve(args.source_page)
        if not page_path.is_file():
            parser.error(f"source page not found: {page_path}")
        page_text = page_path.read_text(encoding="utf-8", errors="ignore")
        issues = compare(result["raw_inventory"], inventory(page_text), source_sections(page_text))  # type: ignore[arg-type]
        result.update({
            "source_page": str(page_path.relative_to(ROOT)),
            "source_inventory": inventory(page_text),
            "issues": issues,
            "ok": not any(i["severity"] == "error" for i in issues),
        })
        exit_code = 1 if not result["ok"] else 0

    if args.json:
        print(json.dumps(result, ensure_ascii=False, indent=2))
    else:
        print(f"Raw: {result['raw']}")
        for key, value in result["raw_inventory"].items():  # type: ignore[union-attr]
            print(f"  {key}: {value}")
        if args.source_page:
            print(f"Source page: {result['source_page']}")
            for issue in result["issues"]:  # type: ignore[union-attr]
                print(f"  {issue['severity'].upper()}: {issue['detail']}")
    return exit_code


if __name__ == "__main__":
    raise SystemExit(main())
