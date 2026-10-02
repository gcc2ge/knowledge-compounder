#!/usr/bin/env bash
# ============================================================================
# pdf2md — PDF to clean Markdown converter
# Usage: pdf2md.sh <input.pdf> [output.md]
#
# Dependencies: python3 (+ pdfminer.six), or pdftotext (poppler-utils)
# Falls back: pdfminer → pdftotext → fail
#
# Output: clean markdown with YAML frontmatter, stashed in raw/books/ by default
# ============================================================================

set -uo pipefail

# --- Config ----------------------------------------------------------------
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
RAW_BOOKS_DIR="${STRATEGY_WIKI:-$SCRIPT_DIR/..}/raw/books"

# --- Help ------------------------------------------------------------------
if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  sed -n '3,10p' "$0"
  exit 0
fi

# --- Resolve paths ---------------------------------------------------------
INPUT_PDF="$(cd "$(dirname "$1")" 2>/dev/null && pwd)/$(basename "$1")" || {
  echo "ERROR: Cannot resolve input path: $1" >&2
  exit 1
}
if [[ ! -f "$INPUT_PDF" ]]; then
  echo "ERROR: File not found: $INPUT_PDF" >&2
  exit 1
fi

if [[ -n "${2:-}" ]]; then
  OUTPUT_MD="$2"
else
  BASENAME="$(basename "$INPUT_PDF" .pdf)"
  OUTPUT_MD="${RAW_BOOKS_DIR}/${BASENAME}.md"
fi

# --- Temp files (macOS compat: X's must be at end) -------------------------
TEXT_TMP="$(mktemp /tmp/pdf2md_XXXXXXXX)"
PY_SCRIPT="$(mktemp /tmp/pdf2md_XXXXXXXX)"
trap 'rm -f "$TEXT_TMP" "$PY_SCRIPT"' EXIT

# --- Step 1: Extract text from PDF -----------------------------------------
echo "Extracting text from: $INPUT_PDF" >&2

if python3 -c "import pdfminer" 2>/dev/null; then
  # Use pipe to avoid shell injection from Chinese chars in filename
  python3 << PYEOF
from pdfminer.high_level import extract_text
text = extract_text("$INPUT_PDF")
with open("$TEXT_TMP", "w", encoding="utf-8") as f:
    f.write(text)
print(f"Extracted {len(text)} chars via pdfminer")
PYEOF
elif command -v pdftotext &>/dev/null; then
  pdftotext "$INPUT_PDF" "$TEXT_TMP"
  echo "Extracted via pdftotext" >&2
else
  echo "ERROR: Need pdfminer (pip install pdfminer.six) or pdftotext (poppler)" >&2
  exit 1
fi

# --- Step 2: Count lines to decide if extraction was meaningful ------------
LINE_COUNT=$(wc -l < "$TEXT_TMP")
CHAR_COUNT=$(wc -c < "$TEXT_TMP")
if [[ "$CHAR_COUNT" -lt 100 ]]; then
  echo "WARNING: Only $CHAR_COUNT chars extracted — PDF may be scanned/image-based" >&2
fi
echo "Raw text: $LINE_COUNT lines, $CHAR_COUNT chars" >&2

# --- Step 3: Auto-detect metadata from first page --------------------------
TITLE=""
while IFS= read -r LINE; do
  CLEAN="$(echo "$LINE" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  [[ -z "$CLEAN" ]] && continue
  [[ "$CLEAN" =~ ^[0-9]+$ ]] && continue
  [[ "$CLEAN" =~ ^[a-zA-Z]$ ]] && continue
  [[ "$CLEAN" =~ ^[A-Z][a-z]+.*[0-9],?$ ]] && continue
  [[ "$CLEAN" == "Abstract" ]] && continue
  if [[ ${#CLEAN} -gt 20 && ! "$CLEAN" =~ ^[0-9] && ! "$CLEAN" =~ [*] ]]; then
    TITLE="$CLEAN"
    break
  fi
done < "$TEXT_TMP"

TITLE="$(echo "$TITLE" | sed 's/^[0-9][[:space:]]*//')"

# Extract year from PDF metadata or first page
YEAR=""
if command -v pdfinfo &>/dev/null; then
  YEAR="$(pdfinfo "$INPUT_PDF" 2>/dev/null | grep -i 'CreationDate' | grep -oE '[0-9]{4}' | head -1 || true)"
fi
if [[ -z "$YEAR" ]]; then
  YEAR=$(grep -oE '20[0-9]{2}' "$TEXT_TMP" | sort | uniq | head -1 || true)
fi

# --- Step 4: Generate the cleanup Python script ----------------------------
# (embedded as a heredoc to avoid quoting issues)
cat > "$PY_SCRIPT" << 'PYEOF'
#!/usr/bin/env python3
"""Generic PDF-to-markdown cleaner. Reads stdin, writes cleaned markdown to stdout."""
import re, sys

raw = sys.stdin.read()
lines = raw.splitlines()

# ===== STEP 1: Strip garbled PDF header =====
start = 0
for i, l in enumerate(lines):
    stripped = l.strip()
    if stripped and len(stripped) > 5 and not re.match(r'^[0-9\s\[\]\(\)]+$', stripped):
        start = i
        break
body = lines[start:]

body = [l.replace('\f', '') for l in body]
body = [re.sub(r'[\x00-\x08\x0b\x0c\x0e-\x1f]', '', l) for l in body]

# ===== STEP 2: Generic section heading detection =====
processed = []
skip_until = -1
for i, line in enumerate(body):
    if i <= skip_until:
        continue
    stripped = line.strip()
    if not stripped:
        processed.append('')
        continue

    # Handle "N" + "Title" on separate lines
    if re.match(r'^\d+$', stripped) and len(stripped) <= 3:
        for j in range(i + 1, min(i + 5, len(body))):
            next_stripped = body[j].strip()
            if next_stripped and len(next_stripped) > 3 and re.match(r'^[A-Z]', next_stripped):
                processed.append(f'# {stripped} {next_stripped}')
                skip_until = j
                break
        if skip_until >= j:
            continue
        processed.append(stripped)
        continue

    # Main section: "N Section Title" or "A Section Title" (appendix)
    m = re.match(r'^(\d+|[A-Z])\s+(.+)$', stripped)
    if m:
        prefix = m.group(1)
        title = m.group(2)
        if re.match(r'^(doi|http|www|arxiv)', title, re.IGNORECASE):
            processed.append(stripped)
            continue
        if re.match(r'^[\d\s]+$', title):
            processed.append(stripped)
            continue
        if len(title) < 3 and re.match(r'^\d+$', title):
            processed.append(stripped)
            continue
        processed.append(f'# {prefix} {title}')
        continue

    # Subsection: "N.N Title" or "A.N Title"
    m = re.match(r'^(\d+\.\d+|[A-Z]\.\d+)\s+(.+)$', stripped)
    if m:
        processed.append(f'## {m.group(1)} {m.group(2)}')
        continue

    if stripped == 'Abstract':
        processed.append('**Abstract**')
        continue
    if stripped == 'References':
        processed.append('## References')
        continue

    processed.append(stripped)

text = '\n'.join(processed)

# ===== STEP 3: Remove (cid:NNN) artifacts =====
text = re.sub(r'\(cid:\d+\)', '', text)

# ===== STEP 4: Fix LaTeX commands broken across lines =====
text = re.sub(r'\\(\s*\n\s*)([a-zA-Z]+)', lambda m: '\\' + m.group(2), text)

for cmd in ['mathbb', 'mathcal', 'mathrm', 'textbf', 'textit', 'sum', 'partial', 'int']:
    text = re.sub(rf'\\[\s\n]*{cmd}', rf'\\{cmd}', text)

# ===== STEP 5: Fix hyphenation line breaks =====
text = re.sub(r'([a-z])-\n([a-z])', r'\1\2', text)

# ===== STEP 6: Remove figure data artifacts and page numbers =====
blocks = re.split(r'\n\n+', text)
cleaned_blocks = []
for block in blocks:
    block = block.strip()
    if not block:
        continue
    if re.match(r'^\d{1,3}$', block):
        continue
    if re.match(r'^[\d\s]+$', block) and len(block) > 10:
        continue
    if re.match(r'^[\d]{4,}', block) and ('2600' in block or '2800' in block or '3000' in block):
        if not any(c in block for c in '.!?\"'):
            continue
    cleaned_blocks.append(block)
text = '\n\n'.join(cleaned_blocks)

# ===== STEP 7: Reflow paragraphs =====
blocks = re.split(r'\n\n+', text)
refined = []
for block in blocks:
    block = block.strip()
    if not block:
        continue
    blines = block.split('\n')
    first_line = blines[0].strip()

    if first_line.startswith('#'):
        refined.append(block)
        continue
    if any(re.match(r'^\[\d+\]', l) for l in blines[:3]):
        refined.append(block)
        continue
    if any(l.startswith('**') or l.startswith('*') or l.startswith('-') or l.startswith('1.') for l in blines[:2]):
        refined.append(block)
        continue
    if any(l.strip().startswith('\\[') or l.strip().startswith('\\(') or l.strip().startswith('$$') for l in blines):
        refined.append(block)
        continue

    if len(blines) <= 1:
        refined.append(block)
    else:
        joined = ' '.join(l.strip() for l in blines if l.strip())
        joined = re.sub(r'\s+', ' ', joined)
        refined.append(joined)

text = '\n\n'.join(refined)

# ===== STEP 8: Fix specific formatting patterns =====
text = re.sub(r'^\*\*Keywords\*\*:', r'**Keywords:**', text, flags=re.MULTILINE)
text = re.sub(r'^Notation\.', r'**Notation.**', text, flags=re.MULTILINE)
text = re.sub(r'^(\d+)(By contrast|Priority fees|The framework)', r'[\1]: \2', text, flags=re.MULTILINE)
text = re.sub(r'^Proof\.\s*', r'*Proof.* ', text, flags=re.MULTILINE)
text = re.sub(r'^(Case \d+:)\s*', r'*\1* ', text, flags=re.MULTILINE)

# ===== STEP 9: Clean up whitespace =====
text = re.sub(r' {2,}', ' ', text)
text = re.sub(r'\n{4,}', '\n\n\n', text)
text = re.sub(r'\n+(#)', r'\n\n\1', text)
text = text.strip()

print(text)
PYEOF

# --- Step 5: Run the Python cleaner ----------------------------------------
echo "Cleaning markdown..." >&2
CLEAN_TEXT="$(python3 "$PY_SCRIPT" < "$TEXT_TMP")"

# --- Step 6: Build frontmatter ---------------------------------------------
if [[ -z "$TITLE" ]]; then
  TITLE="$(echo "$CLEAN_TEXT" | grep -m1 '^# ' | sed 's/^# //')"
fi
if [[ -z "$TITLE" ]]; then
  TITLE="$(basename "$INPUT_PDF" .pdf | sed 's/[_-]/ /g')"
fi

KEYWORDS_LINE="$(echo "$CLEAN_TEXT" | grep -A1 'Keywords:' | tail -1)"
if [[ -z "$KEYWORDS_LINE" ]]; then
  KEYWORDS_LINE="PDF document"
fi

# --- Step 7: Write output --------------------------------------------------
OUTPUT_DIR="$(dirname "$OUTPUT_MD")"
mkdir -p "$OUTPUT_DIR"

{
  echo "---"
  echo "title: \"$TITLE\""
  echo "origin: external"
  echo "type: paper"
  echo "preview: true"
  [[ -n "$YEAR" ]] && echo "year: $YEAR"
  echo "keywords: \"$KEYWORDS_LINE\""
  echo "---"
  echo ""
  echo "$CLEAN_TEXT"
} > "$OUTPUT_MD"

LINE_COUNT=$(wc -l < "$OUTPUT_MD")
echo "✅ Written to: $OUTPUT_MD ($LINE_COUNT lines)" >&2