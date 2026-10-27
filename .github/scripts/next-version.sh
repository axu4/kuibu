#!/usr/bin/env bash
# 按语义化版本计算下一个 tag。
#
# 用法：next-version.sh <当前最新 tag> <patch|minor|major>
#   next-version.sh v0.4.0 patch  → v0.4.1
#   next-version.sh v0.4.0 minor  → v0.5.0
#   next-version.sh v0.4.2 major  → v1.0.0
#   next-version.sh ""     minor  → v0.1.0（还没有任何 tag 时从 v0.0.0 算）
set -euo pipefail

latest="${1:-}"
bump="${2:-}"
[ -n "$latest" ] || latest="v0.0.0"

if ! [[ "$latest" =~ ^v([0-9]+)\.([0-9]+)\.([0-9]+)$ ]]; then
  echo "当前 tag $latest 不是 vMAJOR.MINOR.PATCH 格式" >&2
  exit 1
fi
major="${BASH_REMATCH[1]}"
minor="${BASH_REMATCH[2]}"
patch="${BASH_REMATCH[3]}"

case "$bump" in
  major) echo "v$((major + 1)).0.0" ;;
  minor) echo "v${major}.$((minor + 1)).0" ;;
  patch) echo "v${major}.${minor}.$((patch + 1))" ;;
  *)
    echo "bump 必须是 patch / minor / major，收到：$bump" >&2
    exit 1
    ;;
esac
