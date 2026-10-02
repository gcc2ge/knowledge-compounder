#!/usr/bin/env python3
"""
wiki-update.py — Deterministic concept/entity page updates (no LLM needed).

Usage:
  # Add a source to a concept page's frontmatter sources list
  python3 scripts/wiki-update.py add-source <slug> <source_slug>

  # Update the 'updated' date field in frontmatter to today
  python3 scripts/wiki-update.py touch <slug>

  # Add a wikilink to the '相关' section
  python3 scripts/wiki-update.py add-link <slug> <target_slug> [--comment "description"]

  # Append content to a specific section (before the next ## or end of file)
  python3 scripts/wiki-update.py append-to <slug> <section> <content_file>

The script reads the page, makes the deterministic change, and writes it back.
This replaces 7 re-reads + 5 edit calls with a single script call.
"""

import json
import os
import re
import sys
from datetime import date
from pathlib import Path

WIKI_DIR = Path(__file__).resolve().parent.parent / "wiki"


def resolve_slug(slug):
    """Find a wiki page by slug."""
    # Try direct path
    for subdir in ["sources", "concepts", "entities", "synthesis", "notes"]:
        p = WIKI_DIR / subdir / f"{slug}.md"
        if p.exists():
            return p
        # Also try with subdir prefix
        p2 = WIKI_DIR / f"{slug}.md"
        if p2.exists():
            return p2
    # Try glob
    for md in WIKI_DIR.rglob("*.md"):
        if md.stem == slug:
            return md
    return None


def extract_frontmatter(text):
    """Extract frontmatter as lines and body."""
    if not text.startswith("---"):
        return [], text
    end = text.find("---", 3)
    if end == -1:
        return [], text
    front_lines = text[3:end].strip().split("\n")
    body = text[end + 3:].strip()
    return front_lines, body


def parse_frontmatter(lines):
    """Parse YAML-like frontmatter lines into a dict."""
    meta = {}
    current_key = None
    current_list = []
    for line in lines:
        # 兼容两种合法 YAML 列表风格："  - item"（缩进）与 "- item"（无缩进）。
        # 旧版只匹配缩进风格，导致无缩进列表被静默丢弃——add-source 写回时
        # 表现为"替换而非追加"（原 sources 全部丢失，只剩新加的一条）。
        if line.startswith("  - ") or line.startswith("- "):
            if current_key:
                item = line[4:] if line.startswith("  - ") else line[2:]
                current_list.append(item.strip())
        else:
            if current_key and current_list:
                meta[current_key] = current_list
                current_list = []
            if ":" in line:
                key, _, val = line.partition(":")
                current_key = key.strip()
                val = val.strip()
                if val.startswith("[") and val.endswith("]"):
                    meta[current_key] = [v.strip().strip("\"'") for v in val[1:-1].split(",")]
                    current_key = None
                elif val:
                    meta[current_key] = val.strip("\"'")
                    current_key = None
                else:
                    current_list = []
    if current_key and current_list:
        meta[current_key] = current_list
    return meta


def serialize_frontmatter(meta):
    """Serialize meta dict back to YAML-like lines."""
    lines = []
    for key, val in meta.items():
        if isinstance(val, list):
            if val and all(isinstance(v, str) for v in val):
                lines.append(f"{key}:")
                for v in val:
                    lines.append(f"  - {v}")
            elif val:
                lines.append(f"{key}: {json.dumps(val, ensure_ascii=False)}")
            else:
                lines.append(f"{key}: []")
        else:
            lines.append(f"{key}: {val}")
    return lines


def cmd_add_source():
    """Add a source to a concept page's frontmatter sources list."""
    if len(sys.argv) < 4:
        print("Usage: python3 scripts/wiki-update.py add-source <slug> <source_slug>", file=sys.stderr)
        sys.exit(1)

    slug = sys.argv[2]
    source_slug = sys.argv[3]

    path = resolve_slug(slug)
    if not path:
        print(f"Error: page not found: {slug}", file=sys.stderr)
        sys.exit(1)

    text = path.read_text(encoding="utf-8")
    front_lines, body = extract_frontmatter(text)
    meta = parse_frontmatter(front_lines)

    sources = meta.get("sources", [])
    if isinstance(sources, str):
        sources = [sources]

    if source_slug not in sources:
        sources.append(source_slug)
        meta["sources"] = sources

    meta["updated"] = date.today().isoformat()

    new_front = serialize_frontmatter(meta)
    new_text = "---\n" + "\n".join(new_front) + "\n---\n\n" + body + "\n"
    path.write_text(new_text, encoding="utf-8")
    print(f"Updated {path.relative_to(WIKI_DIR.parent)}: added source '{source_slug}'")


def cmd_touch():
    """Update the 'updated' date to today."""
    if len(sys.argv) < 3:
        print("Usage: python3 scripts/wiki-update.py touch <slug>", file=sys.stderr)
        sys.exit(1)

    slug = sys.argv[2]
    path = resolve_slug(slug)
    if not path:
        print(f"Error: page not found: {slug}", file=sys.stderr)
        sys.exit(1)

    text = path.read_text(encoding="utf-8")
    front_lines, body = extract_frontmatter(text)
    meta = parse_frontmatter(front_lines)
    meta["updated"] = date.today().isoformat()

    new_front = serialize_frontmatter(meta)
    new_text = "---\n" + "\n".join(new_front) + "\n---\n\n" + body + "\n"
    path.write_text(new_text, encoding="utf-8")
    print(f"Touched {path.relative_to(WIKI_DIR.parent)}: updated → {date.today()}")


def cmd_add_link():
    """Add a wikilink to the '相关' section."""
    if len(sys.argv) < 4:
        print("Usage: python3 scripts/wiki-update.py add-link <slug> <target_slug> [--comment 'description']", file=sys.stderr)
        sys.exit(1)

    slug = sys.argv[2]
    target = sys.argv[3]
    comment = None
    for i, arg in enumerate(sys.argv):
        if arg == "--comment" and i + 1 < len(sys.argv):
            comment = sys.argv[i + 1]

    path = resolve_slug(slug)
    if not path:
        print(f"Error: page not found: {slug}", file=sys.stderr)
        sys.exit(1)

    text = path.read_text(encoding="utf-8")

    # Check if link already exists
    if f"[[{target}]]" in text:
        print(f"Link [[{target}]] already exists in {slug}, skipping")
        return

    # Find the "相关" section and add after it
    link_line = f"- [[{target}]]"
    if comment:
        link_line += f" — {comment}"

    if re.search(r"^##\s+相关", text, re.MULTILINE):
        # Add after the last existing link
        last_link = list(re.finditer(r"^- \[\[[^\]]+\]\]", text, re.MULTILINE))
        if last_link:
            pos = last_link[-1].end()
            text = text[:pos] + "\n" + link_line + text[pos:]
        else:
            # Add right after "## 相关" line
            m = re.search(r"^##\s+相关\s*$", text, re.MULTILINE)
            if m:
                pos = m.end()
                text = text[:pos] + "\n" + link_line + text[pos:]
    else:
        # No "相关" section, append at end
        text = text.rstrip() + "\n\n## 相关\n" + link_line + "\n"

    path.write_text(text, encoding="utf-8")
    print(f"Added link [[{target}]] to {slug}")


def cmd_append_to():
    """Append content to a specific section."""
    if len(sys.argv) < 5:
        print("Usage: python3 scripts/wiki-update.py append-to <slug> <section_name> <content_file>", file=sys.stderr)
        sys.exit(1)

    slug = sys.argv[2]
    section = sys.argv[3]
    content_file = Path(sys.argv[4])

    if not content_file.exists():
        print(f"Error: content file not found: {content_file}", file=sys.stderr)
        sys.exit(1)

    content = content_file.read_text(encoding="utf-8").strip()

    path = resolve_slug(slug)
    if not path:
        print(f"Error: page not found: {slug}", file=sys.stderr)
        sys.exit(1)

    text = path.read_text(encoding="utf-8")

    # Find the section header
    pattern = rf"^##\s+{re.escape(section)}\s*$"
    m = re.search(pattern, text, re.MULTILINE)
    if not m:
        print(f"Error: section '## {section}' not found in {slug}", file=sys.stderr)
        sys.exit(1)

    # Find where this section ends (next ## or end of file)
    section_start = m.end()
    next_section = re.search(r"^##\s+", text[section_start:], re.MULTILINE)
    if next_section:
        insert_pos = section_start + next_section.start()
    else:
        insert_pos = len(text)

    # Insert content before the next section
    new_text = text[:insert_pos] + "\n" + content + "\n" + text[insert_pos:]
    path.write_text(new_text, encoding="utf-8")
    print(f"Appended {len(content)} chars to ## {section} in {slug}")


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__.strip())
        sys.exit(0)

    cmd = sys.argv[1]
    handlers = {
        "add-source": cmd_add_source,
        "touch": cmd_touch,
        "add-link": cmd_add_link,
        "append-to": cmd_append_to,
    }
    handler = handlers.get(cmd)
    if handler:
        handler()
    else:
        print(f"Unknown command: {cmd}", file=sys.stderr)
        sys.exit(1)