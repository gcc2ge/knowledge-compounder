#!/usr/bin/env python3
"""
check-sources-shrink.py — Pre-commit defense: detect concept/entity pages
whose 'sources' frontmatter list was replaced (shrunk) vs HEAD.

Called from .git/hooks/pre-commit (or standalone for CI).
Returns exit code 1 if any page lost sources, 0 otherwise.
"""

import os
import re
import subprocess
import sys
from pathlib import Path

REPO_ROOT = Path(__file__).resolve().parent.parent
WIKI_DIR = REPO_ROOT / "wiki"

# Directories that MUST have 'sources' in frontmatter
SOURCE_DIRS = ("concepts", "entities")


def get_staged_md_files():
    """Return list of staged md files in wiki/concepts/ and wiki/entities/."""
    result = subprocess.run(
        ["git", "diff", "--cached", "--name-only", "--diff-filter=ACM"],
        capture_output=True, text=True, cwd=REPO_ROOT,
    )
    if result.returncode != 0:
        print(f"check-sources-shrink: git diff failed: {result.stderr}", file=sys.stderr)
        sys.exit(1)

    files = []
    for line in result.stdout.strip().split("\n"):
        line = line.strip()
        if not line:
            continue
        # Only check wiki/concepts/ and wiki/entities/
        parts = Path(line).parts
        if len(parts) >= 3 and parts[0] == "wiki" and parts[1] in SOURCE_DIRS:
            files.append(Path(line))
    return files


def get_head_sources(filepath):
    """Extract sources list from HEAD version of a file."""
    result = subprocess.run(
        ["git", "show", f"HEAD:{filepath}"],
        capture_output=True, text=True, cwd=REPO_ROOT,
    )
    if result.returncode != 0:
        # File doesn't exist in HEAD (new file) — skip
        return None

    return extract_sources_from_text(result.stdout)


def get_staged_sources(filepath):
    """Extract sources list from staged (index) version of a file."""
    result = subprocess.run(
        ["git", "show", f":0:{filepath}"],
        capture_output=True, text=True, cwd=REPO_ROOT,
    )
    if result.returncode != 0:
        return None
    return extract_sources_from_text(result.stdout)


def extract_sources_from_text(text):
    """Extract the 'sources' list from frontmatter."""
    fm_match = re.match(r"^---\s*\n(.*?)\n---", text, re.DOTALL)
    if not fm_match:
        return None

    fm = fm_match.group(1)
    in_sources = False
    sources = []
    for line in fm.split("\n"):
        stripped = line.strip()
        if stripped.startswith("sources:") and not stripped.startswith("sources: "):
            in_sources = True
            continue
        if stripped.startswith("sources: ") and not stripped.startswith("sources: ["):
            # inline list: "sources: [a, b, c]"
            val = stripped[len("sources: "):].strip()
            if val.startswith("[") and val.endswith("]"):
                sources = [v.strip().strip("\"'") for v in val[1:-1].split(",")]
            in_sources = False
            continue
        if in_sources:
            if stripped.startswith("- "):
                sources.append(stripped[2:].strip())
            elif stripped.startswith("  - "):
                sources.append(stripped[4:].strip())
            elif ":" in stripped and not stripped.startswith("-"):
                # New key — stop
                break
            elif not stripped:
                continue
            else:
                break
    return sources


def main():
    staged = get_staged_md_files()
    if not staged:
        return 0

    has_errors = False
    for relpath in staged:
        filepath = str(relpath)

        head_sources = get_head_sources(filepath)
        if head_sources is None:
            continue  # New file or no HEAD version

        staged_sources = get_staged_sources(filepath)
        if staged_sources is None:
            continue

        # Allow staged to be equal or larger, but not smaller
        if len(staged_sources) < len(head_sources):
            # Check if it's a real shrink or just reordering/duplicates
            head_set = set(head_sources)
            staged_set = set(staged_sources)
            if not staged_set.issubset(head_set):
                # Some items in staged are not in HEAD — could be replace+add
                # This is still suspicious if the count dropped
                pass

            # Count how many old sources are missing
            missing = head_set - staged_set
            if missing and len(staged_sources) < len(head_sources):
                print(
                    f"\n❌ {filepath}: sources list shrank from {len(head_sources)} to {len(staged_sources)}. "
                    f"Missing: {missing}"
                )
                has_errors = True

    if has_errors:
        print("\n⚠️  Sources list shrink detected in concept/entity pages.")
        print("   This usually means the frontmatter 'sources' was replaced instead of appended.")
        print("   Fix: python3 scripts/wiki-update.py add-source <slug> <missing_source>")
        print("   Or: git checkout -- <file> && re-apply your changes with add-source\n")
        return 1

    return 0


if __name__ == "__main__":
    sys.exit(main())