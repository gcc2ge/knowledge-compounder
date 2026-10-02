#!/bin/bash
# export-public.sh — 从完整版 wiki 生成公开版
# 用法: bash scripts/export-public.sh
# 输出: wiki-public/ 目录

set -euo pipefail

WIKI_DIR="wiki"
OUTPUT_DIR="wiki-public"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "=== LLM Wiki Public Export ==="

# 1. 清理旧输出
rm -rf "$OUTPUT_DIR"
mkdir -p "$OUTPUT_DIR"

# 2. 复制所有 wiki 文件
cp -r "$WIKI_DIR"/* "$OUTPUT_DIR"/

# 3. 删除 public: false 的页面
echo "Removing private pages..."
PRIVATE_COUNT=0
find "$OUTPUT_DIR" -name "*.md" | while read f; do
  # 扫描 frontmatter（--- 到 --- 之间），不限行数
  if awk '/^---$/{n++; next} n==1{print} n>=2{exit}' "$f" | grep -q "^public: false"; then
    rm "$f"
    PRIVATE_COUNT=$((PRIVATE_COUNT + 1))
    echo "  removed: $(basename "$f")"
  fi
done

# 4. 剥离 <!-- PRIVATE --> 段落
echo "Stripping private sections..."
find "$OUTPUT_DIR" -name "*.md" | while read f; do
  if grep -q "<!-- PRIVATE -->" "$f"; then
    # 使用 awk 删除 PRIVATE 标记之间的内容
    awk '
      /<!-- PRIVATE -->/ { skip=1; next }
      /<!-- \/PRIVATE -->/ { skip=0; next }
      !skip { print }
    ' "$f" > "${f}.tmp" && mv "${f}.tmp" "$f"
    echo "  stripped sections: $(basename "$f")"
  fi
done

# 5. 执行文本替换（隐私脱敏）
# 根据你的需求自定义替换规则
echo "Applying privacy replacements..."
find "$OUTPUT_DIR" -name "*.md" | while read f; do
  sed -i '' \
    -e 's|/Volumes/[^ ]*|<your-path>|g' \
    -e 's|/Users/[^ ]*|<your-path>|g' \
    "$f"
done

# 6. 更新 index 中的页面计数
PUBLIC_SOURCES=$(ls "$OUTPUT_DIR"/sources/*.md 2>/dev/null | wc -l | tr -d ' ')
PUBLIC_CONCEPTS=$(ls "$OUTPUT_DIR"/concepts/*.md 2>/dev/null | wc -l | tr -d ' ')
PUBLIC_ENTITIES=$(ls "$OUTPUT_DIR"/entities/*.md 2>/dev/null | wc -l | tr -d ' ')
PUBLIC_SYNTHESIS=$(ls "$OUTPUT_DIR"/synthesis/*.md 2>/dev/null | wc -l | tr -d ' ')
PUBLIC_TOTAL=$((PUBLIC_SOURCES + PUBLIC_CONCEPTS + PUBLIC_ENTITIES + PUBLIC_SYNTHESIS))

echo ""
echo "=== Export Complete ==="
echo "Public pages: $PUBLIC_TOTAL (src:$PUBLIC_SOURCES con:$PUBLIC_CONCEPTS ent:$PUBLIC_ENTITIES syn:$PUBLIC_SYNTHESIS)"
echo "Output: $OUTPUT_DIR/"
