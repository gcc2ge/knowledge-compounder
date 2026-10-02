#!/usr/bin/env python3
"""
wiki-registry.py — Pre-computed wiki page registry for efficient discovery.

Usage:
  python3 scripts/wiki-registry.py build          # Build/update the registry cache
  python3 scripts/wiki-registry.py query <terms>  # Find pages matching keywords (comma-separated)
  python3 scripts/wiki-registry.py lookup <slug>  # Show a single page's metadata
  python3 scripts/wiki-registry.py stats          # Show registry statistics

The registry caches page metadata (title, type, tags, wikilinks, summary)
so compiler agents don't need 20+ grep + 36 reads to find relevant pages.
"""

import json
import os
import re
import sys
from pathlib import Path

WIKI_DIR = Path(__file__).resolve().parent.parent / "wiki"
REGISTRY_FILE = Path(__file__).resolve().parent.parent / ".wiki-registry.json"


def extract_frontmatter(path):
    """Extract YAML frontmatter fields as a dict."""
    meta = {}
    try:
        content = path.read_text(encoding="utf-8")
    except Exception:
        return meta, ""

    text = content
    if content.startswith("---"):
        end = content.find("---", 3)
        if end != -1:
            front = content[3:end].strip()
            text = content[end + 3:].strip()
            for line in front.split("\n"):
                if ":" in line:
                    key, _, val = line.partition(":")
                    key = key.strip()
                    val = val.strip()
                    if val.startswith("[") and val.endswith("]"):
                        val = [v.strip().strip("\"'") for v in val[1:-1].split(",")]
                    elif val.lower() in ("true", "false"):
                        val = val.lower() == "true"
                    else:
                        val = val.strip("\"'")
                    meta[key] = val
    return meta, text


def extract_title(text):
    """Extract the first H1 heading."""
    m = re.search(r"^#\s+(.+)$", text, re.MULTILINE)
    return m.group(1).strip() if m else ""


def extract_wikilinks(text):
    """Extract all [[wikilinks]] from text."""
    return re.findall(r"\[\[([^\]]+?)(?:\|([^\]]+))?\]\]", text)


def extract_tags(meta):
    """Normalize tags from frontmatter."""
    tags = meta.get("tags", [])
    if isinstance(tags, str):
        tags = [t.strip() for t in tags.split(",")]
    return tags


def compute_summary(text, max_chars=200):
    """First meaningful sentence after the title."""
    # Remove title
    text = re.sub(r"^#\s+.+$", "", text, count=1, flags=re.MULTILINE).strip()
    # Remove empty lines and markdown formatting
    text = re.sub(r"[#*`>_\[\]]", "", text)
    text = re.sub(r"\s+", " ", text).strip()
    if len(text) <= max_chars:
        return text
    # Try to cut at sentence boundary
    cut = text[:max_chars]
    last_period = cut.rfind("。")
    if last_period > max_chars * 0.5:
        return text[: last_period + 1]
    last_space = cut.rfind(" ")
    if last_space > max_chars * 0.5:
        return text[:last_space] + "…"
    return cut + "…"


def scan_page(path):
    """Scan a single wiki page and return its metadata."""
    meta, text = extract_frontmatter(path)
    title = meta.get("title") or extract_title(text)
    page_type = meta.get("type", "unknown")
    tags = extract_tags(meta)
    wikilinks = [slug for slug, _ in extract_wikilinks(text)]
    summary = compute_summary(text)

    # Relative path from wiki/
    rel_path = path.relative_to(WIKI_DIR)
    slug = rel_path.with_suffix("").as_posix()

    return {
        "slug": slug,
        "title": title,
        "type": page_type,
        "tags": tags,
        "wikilinks": wikilinks,
        "summary": summary,
        "file": str(rel_path),
        "updated": meta.get("updated", ""),
        "confidence": meta.get("confidence", ""),
        "sources": meta.get("sources", []),
    }


def build_registry():
    """Scan all wiki pages and build the registry JSON."""
    registry = {"pages": [], "stats": {}}

    type_counts = {}
    for md_file in sorted(WIKI_DIR.rglob("*.md")):
        # Skip health reports and index
        rel = md_file.relative_to(WIKI_DIR).as_posix()
        if rel.startswith("health/") or rel == "index.md":
            continue
        try:
            entry = scan_page(md_file)
            registry["pages"].append(entry)
            t = entry["type"]
            type_counts[t] = type_counts.get(t, 0) + 1
        except Exception as e:
            print(f"  [warn] Failed to scan {rel}: {e}", file=sys.stderr)

    registry["stats"] = {
        "total_pages": len(registry["pages"]),
        "by_type": type_counts,
        "built_at": __import__("datetime").datetime.now().isoformat(),
    }

    REGISTRY_FILE.write_text(json.dumps(registry, ensure_ascii=False, indent=2), encoding="utf-8")
    print(f"Registry built: {registry['stats']['total_pages']} pages → {REGISTRY_FILE}")
    return registry


def load_registry():
    """Load the cached registry, rebuilding if missing."""
    if not REGISTRY_FILE.exists():
        print("Registry not found, building...", file=sys.stderr)
        return build_registry()
    return json.loads(REGISTRY_FILE.read_text(encoding="utf-8"))


def query_pages(terms, registry=None):
    """Search registry for pages matching any of the comma-separated terms."""
    if registry is None:
        registry = load_registry()

    terms = [t.strip().lower() for t in terms.split(",") if t.strip()]
    results = []

    for page in registry["pages"]:
        score = 0
        searchable = (
            page.get("title", "").lower() + " " +
            " ".join(page.get("tags", [])) + " " +
            page.get("summary", "").lower()
        )
        for term in terms:
            if term in searchable:
                score += 1
            # Also check wikilink targets
            for wl in page.get("wikilinks", []):
                if term in wl.lower():
                    score += 0.5
                    break

        if score > 0:
            results.append((score, page))

    results.sort(key=lambda x: -x[0])
    return [r[1] for r in results]


def lookup_page(slug, registry=None):
    """Look up a single page by slug."""
    if registry is None:
        registry = load_registry()
    for page in registry["pages"]:
        if page["slug"] == slug or page["slug"].endswith("/" + slug):
            return page
    return None


def cmd_build():
    build_registry()


def cmd_query():
    if len(sys.argv) < 3:
        print("Usage: python3 scripts/wiki-registry.py query <term1,term2,...>", file=sys.stderr)
        sys.exit(1)
    registry = load_registry()
    results = query_pages(sys.argv[2], registry)
    print(f"Matching pages for '{sys.argv[2]}':\n")
    if not results:
        print("  (none)")
        return
    for p in results:
        tags = ", ".join(p["tags"][:5]) if p["tags"] else "-"
        wls = len(p["wikilinks"])
        print(f"  [[{p['slug']}]]")
        print(f"    type={p['type']}  tags={tags}")
        print(f"    wikilinks={wls}  updated={p['updated'] or '-'}")
        print(f"    {p['summary'][:120]}")
        print()


def cmd_lookup():
    if len(sys.argv) < 3:
        print("Usage: python3 scripts/wiki-registry.py lookup <slug>", file=sys.stderr)
        sys.exit(1)
    registry = load_registry()
    page = lookup_page(sys.argv[2], registry)
    if not page:
        print(f"Page '{sys.argv[2]}' not found.")
        return
    print(json.dumps(page, ensure_ascii=False, indent=2))


def cmd_stats():
    registry = load_registry()
    stats = registry["stats"]
    print(f"Total pages: {stats['total_pages']}")
    print(f"By type: {json.dumps(stats['by_type'], ensure_ascii=False)}")
    print(f"Built at: {stats['built_at']}")


def cmd_rebuild():
    """Force rebuild."""
    if REGISTRY_FILE.exists():
        REGISTRY_FILE.unlink()
    build_registry()


if __name__ == "__main__":
    if len(sys.argv) < 2:
        print(__doc__.strip())
        sys.exit(0)

    cmd = sys.argv[1]
    handlers = {
        "build": cmd_build,
        "query": cmd_query,
        "lookup": cmd_lookup,
        "stats": cmd_stats,
        "rebuild": cmd_rebuild,
    }
    handler = handlers.get(cmd)
    if handler:
        handler()
    else:
        print(f"Unknown command: {cmd}", file=sys.stderr)
        sys.exit(1)