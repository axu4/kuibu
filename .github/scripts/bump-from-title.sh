#!/usr/bin/env bash
# 由 PR 标题判断发版级别，输出 major / minor / patch，或空（不发版）。
#
# 用法：bump-from-title.sh "<PR 标题>"
#   docs: / ci: / chore: / test: / refactor: / style: / build: 开头 → 不发版
#   xxx!: 或 xxx(范围)!: 开头（破坏性改动）                       → major
#   feat: 或 feat(范围): 开头                                      → minor
#   其他（app:、core:、fix:、M2 … 等）                             → patch
# 前缀不区分大小写，中文冒号「：」也认。
set -euo pipefail

title="${1:-}"
# 取冒号前的「类型(范围)!」部分；标题不是这种格式时 type 为空，按 patch 处理。
re='^[[:space:]]*([A-Za-z]+)(\([^)]*\))?(!)?[[:space:]]*(:|：)'
type=""
breaking=""
if [[ "$title" =~ $re ]]; then
  type="$(tr '[:upper:]' '[:lower:]' <<<"${BASH_REMATCH[1]}")"
  breaking="${BASH_REMATCH[3]}"
fi

if [ -n "$breaking" ]; then
  echo major
  exit 0
fi
case "$type" in
  docs|ci|chore|test|refactor|style|build) echo "" ;;
  feat) echo minor ;;
  *) echo patch ;;
esac
