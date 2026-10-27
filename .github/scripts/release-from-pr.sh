#!/usr/bin/env bash
# 按已合并 PR 上的 release:* 标签打 tag，并触发 Release 工作流。
#
# 用法：release-from-pr.sh <PR 号>
# 依赖环境变量：GH_TOKEN（contents/actions/pull-requests 写权限）、GITHUB_REPOSITORY。
# 需要在完整历史（fetch-depth: 0，含 tags）的仓库里运行。
#
# 规则：
#   - PR 必须已合并；没有 release:* 标签就什么都不做。
#   - 同时有多个 release:* 标签时取最大的（major > minor > patch）。
#   - 新版本 = 最新正式 tag（vX.Y.Z，忽略 -rc 等预发布）按标签递增。
#   - 合并提交上已经有 v* tag 时直接退出，保证重跑不会重复发版。
#   - tag 打在 PR 的合并提交上，而不是 main 的最新提交。
#   - 用工作流自带的 token 推 tag 不会触发 Release（GitHub 防止工作流连环触发），
#     所以推完 tag 后用 workflow_dispatch 显式触发 release.yml。
set -euo pipefail

pr="${1:?用法：release-from-pr.sh <PR 号>}"
repo="${GITHUB_REPOSITORY:?缺少 GITHUB_REPOSITORY}"
here="$(cd "$(dirname "$0")" && pwd)"

info="$(gh pr view "$pr" --repo "$repo" --json state,mergeCommit,labels)"
state="$(jq -r '.state' <<<"$info")"
sha="$(jq -r '.mergeCommit.oid // empty' <<<"$info")"
labels="$(jq -r '.labels[].name' <<<"$info")"

if [ "$state" != "MERGED" ] || [ -z "$sha" ]; then
  echo "PR #$pr 尚未合并（$state），不发版。"
  exit 0
fi

bump=""
for level in major minor patch; do
  if grep -qx "release:$level" <<<"$labels"; then
    bump="$level"
    break
  fi
done
if [ -z "$bump" ]; then
  echo "PR #$pr 没有 release:* 标签，不发版。"
  exit 0
fi

# 两个发版 PR 几乎同时合并时可能算出同一个版本号：tag 已存在会推送失败，
# 这时重新拉取 tags 再算一次，最多试 3 次。
next=""
for attempt in 1 2 3; do
  git fetch --force --tags origin >/dev/null
  existing="$(git tag --points-at "$sha" --list 'v*' | head -n 1)"
  if [ -n "$existing" ]; then
    echo "合并提交 $sha 已有 tag $existing，跳过。"
    exit 0
  fi

  latest="$(git tag --list 'v*' --sort=-v:refname | grep -E '^v[0-9]+\.[0-9]+\.[0-9]+$' | head -n 1 || true)"
  candidate="$("$here/next-version.sh" "$latest" "$bump")"
  echo "PR #$pr：${latest:-（无）} --$bump--> $candidate，合并提交 $sha（第 $attempt 次）"

  git -c user.name="github-actions[bot]" \
      -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
      tag -a "$candidate" "$sha" -m "跬步 ${candidate#v}"
  if git push origin "refs/tags/$candidate"; then
    next="$candidate"
    break
  fi
  git tag -d "$candidate" >/dev/null
  sleep 5
done
if [ -z "$next" ]; then
  echo "推送 tag 连续失败，请手动处理。" >&2
  exit 1
fi

gh workflow run release.yml --repo "$repo" --ref main -f tag="$next"

gh pr comment "$pr" --repo "$repo" --body "已打 tag \`$next\`（$bump），Release 正在构建：https://github.com/$repo/actions/workflows/release.yml"
