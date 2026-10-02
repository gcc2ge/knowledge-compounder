#!/usr/bin/env python3
"""
wiki-relink.py — Deterministic wikilink cross-reference builder.

Usage:
  python3 scripts/wiki-relink.py suggest <raw_path> [--max-n 15]

Given a source page's content, this script:
1. Extracts all noun phrases and terms from the page
2. Matches them against the wiki registry for existing pages
3. Returns a ranked list of candidate [[wikilinks]]

This is a pure heuristic (no LLM) — it finds exact/partial matches
in page titles, tags, and summaries from the registry.
"""

import json
import re
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
REGISTRY_FILE = REPO_ROOT / ".wiki-registry.json"


def load_registry():
    if not REGISTRY_FILE.exists():
        print("Registry not found. Run `python3 scripts/wiki-registry.py build` first.", file=sys.stderr)
        return None
    return json.loads(REGISTRY_FILE.read_text(encoding="utf-8"))


def extract_terms(text):
    """Extract potential linkable terms from text."""
    terms = set()

    # Already existing wikilinks — skip these
    existing = set()
    for m in re.finditer(r"\[\[([^\]]+?)(?:\|([^\]]+))?\]\]", text):
        existing.add(m.group(1).strip())

    # Remove wikilinks from text for further analysis
    clean = re.sub(r"\[\[[^\]]+\]\]", "", text)

    # Chinese terms: 2-6 character phrases that aren't common words
    # Look for patterns like: 流水庄, 注意力套利, 排行榜, 刷量, 版本切换
    # Also extract English terms
    for m in re.finditer(r'[A-Z][a-z]+(?:\s+[A-Z][a-z]+)*', clean):
        terms.add(m.group(0).strip())

    # Extract quoted terms (「」 or "" )
    for m in re.finditer(r'「([^」]+)」|"([^"]+)"', clean):
        term = (m.group(1) or m.group(2)).strip()
        if len(term) >= 2:
            terms.add(term)

    # Extract Chinese bigrams/trigrams with domain-specific patterns
    # Common patterns in crypto/defi writing
    for m in re.finditer(r'[一-鿿]{2,8}', clean):
        term = m.group(0).strip()
        # Skip if it looks like a generic phrase
        if len(term) >= 2 and not any(
            skip in term for skip in ["一个", "这个", "那个", "这些", "那些", "可以", "没有", "不是", "什么", "如何", "为什么", "因为", "所以", "如果", "虽然", "但是", "而且", "或者", "然后", "接着", "之后", "之前", "时候", "地方", "方式", "方法", "结果", "原因", "部分", "方面", "一些", "很多", "非常", "比较", "已经", "正在", "可能", "应该", "需要", "能够", "成为", "作为", "进行", "提供", "使用", "通过", "根据", "关于", "对于"]
        ):
            terms.add(term)

    return terms, existing


def find_candidates(terms, existing, registry, max_n=15):
    """Match terms against registry pages."""
    candidates = []
    seen_slugs = set()

    for term in terms:
        term_lower = term.lower()
        for page in registry["pages"]:
            slug = page["slug"]
            if slug in seen_slugs:
                continue

            searchable = (
                page.get("title", "").lower() + " " +
                page.get("slug", "").lower() + " " +
                " ".join(page.get("tags", [])).lower()
            )

            # Exact match on title/slug gets highest score
            score = 0
            if term_lower == page.get("title", "").lower():
                score = 10
            elif term_lower == page.get("slug", "").split("/")[-1].lower():
                score = 10
            elif term_lower in searchable:
                # Check if it's a substring match
                score = 3
            elif term_lower in page.get("summary", "").lower():
                score = 1

            if score > 0:
                seen_slugs.add(slug)
                candidates.append((score, slug, page["title"], page["type"]))

    candidates.sort(key=lambda x: -x[0])
    return candidates[:max_n]


def cmd_suggest():
    if len(sys.argv) < 3:
        print("Usage: python3 scripts/wiki-relink.py suggest <file_path> [--max-n 15]", file=sys.stderr)
        sys.exit(1)

    file_path = Path(sys.argv[2])
    if not file_path.exists():
        print(f"File not found: {file_path}", file=sys.stderr)
        sys.exit(1)

    max_n = 15
    for i, arg in enumerate(sys.argv):
        if arg == "--max-n" and i + 1 < len(sys.argv):
            max_n = int(sys.argv[i + 1])

    registry = load_registry()
    if not registry:
        sys.exit(1)

    text = file_path.read_text(encoding="utf-8")
    terms, existing = extract_terms(text)
    candidates = find_candidates(terms, existing, registry, max_n)

    # Filter out self-references and already-linked pages
    slug = file_path.stem
    filtered = [(s, sl, t, tp) for s, sl, t, tp in candidates if sl.split("/")[-1] != slug and sl not in existing]

    print(f"Source: {file_path.name}")
    print(f"Terms extracted: {len(terms)}")
    print(f"Existing wikilinks: {len(existing)}")
    print(f"Suggested candidates: {len(filtered)}\n")

    for score, slug, title, ptype in filtered:
        print(f"  [[{slug}]]  (score={score}, type={ptype})")
        print(f"    → {title}")
        print()


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__.strip())
        sys.exit(0)

    cmd = sys.argv[1]
    if cmd == "suggest":
        cmd_suggest()
    else:
        print(f"Unknown command: {cmd}", file=sys.stderr)
        sys.exit(1)