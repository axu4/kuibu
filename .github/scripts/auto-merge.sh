#!/usr/bin/env bash
# 带 automerge 标签的 PR 在 CI 通过后自动 squash 合并；带 release:* 标签的再接着发版。
#
# 用法：auto-merge.sh [PR 号] [head SHA]
#   - 加标签时（pull_request_target labeled）传 PR 号；
#   - CI 跑完时（workflow_run）传 head SHA，由它反查 PR。
# 依赖环境变量：GH_TOKEN、GITHUB_REPOSITORY。
#
# 只在以下条件都满足时合并：PR 打开、不是草稿、带 automerge 标签、来自本仓库分支
# （不自动合并 fork）、该 head 提交上最新一次 CI 已成功完成。
# 合并时用 --match-head-commit 锁定已通过 CI 的提交，防止期间又有新推送。
set -euo pipefail

pr="${1:-}"
head_sha="${2:-}"
repo="${GITHUB_REPOSITORY:?缺少 GITHUB_REPOSITORY}"
here="$(cd "$(dirname "$0")" && pwd)"

if [ -z "$pr" ]; then
  [ -n "$head_sha" ] || { echo "需要 PR 号或 head SHA" >&2; exit 1; }
  pr="$(gh api "repos/$repo/commits/$head_sha/pulls" --jq '[.[] | select(.state == "open")][0].number // empty')"
  if [ -z "$pr" ]; then
    echo "提交 $head_sha 没有对应的打开 PR，跳过。"
    exit 0
  fi
fi

info="$(gh pr view "$pr" --repo "$repo" \
  --json number,title,state,isDraft,headRefOid,isCrossRepository,labels)"
state="$(jq -r '.state' <<<"$info")"
draft="$(jq -r '.isDraft' <<<"$info")"
cross="$(jq -r '.isCrossRepository' <<<"$info")"
sha="$(jq -r '.headRefOid' <<<"$info")"
title="$(jq -r '.title' <<<"$info")"
labels="$(jq -r '.labels[].name' <<<"$info")"

if ! grep -qx "automerge" <<<"$labels"; then
  echo "PR #$pr 没有 automerge 标签，跳过。"
  exit 0
fi
if [ "$state" != "OPEN" ] || [ "$draft" = "true" ]; then
  echo "PR #$pr 状态为 $state（草稿：$draft），跳过。"
  exit 0
fi
if [ "$cross" = "true" ]; then
  echo "PR #$pr 来自 fork，不自动合并。"
  exit 0
fi
if [ -n "$head_sha" ] && [ "$head_sha" != "$sha" ]; then
  echo "这次 CI 跑的是 $head_sha，PR 当前 head 已是 $sha，等新提交的 CI。"
  exit 0
fi

ci="$(gh run list --repo "$repo" --workflow ci.yml --commit "$sha" --event pull_request \
  --limit 1 --json status,conclusion --jq '.[0] | "\(.status) \(.conclusion)"')"
case "$ci" in
  "completed success") ;;
  completed*)
    echo "PR #$pr 的 CI 未通过（$ci），不合并；修好后新的 CI 通过会再触发。"
    exit 0
    ;;
  *)
    echo "PR #$pr 的 CI 还在跑（${ci:-未运行}），等 CI 跑完再合并。"
    exit 0
    ;;
esac

echo "PR #$pr CI 已通过，squash 合并 $sha。"
gh pr merge "$pr" --repo "$repo" --squash --match-head-commit "$sha" --subject "$title (#$pr)"

# 自己用工作流 token 合并不会触发 auto-release 的 closed 事件，这里直接接着发版。
"$here/release-from-pr.sh" "$pr"
