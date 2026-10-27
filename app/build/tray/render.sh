#!/bin/sh
# 把托盘模板图标 SVG 渲染成 44×44 PNG（菜单栏显示为 22pt，Retina 下清晰）。
# 模板图标只用纯黑 + 透明，系统自动适配深浅色菜单栏。需要本机安装 Google Chrome。
#
# 用法（在 app/ 目录下）：
#   sh build/tray/render.sh                       # tray-template.svg → tray-template.png
#   sh build/tray/render.sh build/tray/candidates/b-two-steps.svg   # 换成某个候选
set -e
dir=$(cd "$(dirname "$0")" && pwd)
src=${1:-"$dir/tray-template.svg"}
src=$(cd "$(dirname "$src")" && pwd)/$(basename "$src")
chrome="/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
if [ "$src" != "$dir/tray-template.svg" ]; then
	cp "$src" "$dir/tray-template.svg"
fi
"$chrome" --headless=new --disable-gpu --hide-scrollbars --default-background-color=00000000 \
	--window-size=44,44 --screenshot="$dir/tray-template.png" "file://$dir/tray-template.svg" >/dev/null 2>&1
echo "已生成 $dir/tray-template.png"
