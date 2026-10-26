//go:build ignore

// 生成 macOS 菜单栏模板图标（纯黑 + 透明背景的 44×44 PNG）。
//
// 用法（在 app/ 目录下）：
//
//	go run build/tray/gen/main.go
//
// 图案是三级上升台阶，寓意「跬步」。4×4 超采样后按覆盖率写入 alpha，
// 得到平滑边缘；模板图标由系统自动适配深浅色菜单栏，所以只用黑色。
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"os"
)

const (
	size   = 44 // @2x，显示为 22pt
	scale  = 4  // 超采样倍数
	radius = 2.0
)

// rect 是台阶的矩形范围（含边界），坐标为最终 44×44 画布。
type rect struct{ x0, y0, x1, y1 float64 }

var steps = []rect{
	{5, 28, 15, 38},
	{17, 20, 27, 38},
	{29, 12, 39, 38},
}

func main() {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			inside := 0
			for sy := 0; sy < scale; sy++ {
				for sx := 0; sx < scale; sx++ {
					px := float64(x) + (float64(sx)+0.5)/scale
					py := float64(y) + (float64(sy)+0.5)/scale
					for _, r := range steps {
						if inRoundedRect(px, py, r, radius) {
							inside++
							break
						}
					}
				}
			}
			if inside == 0 {
				continue
			}
			alpha := uint8(255 * inside / (scale * scale))
			img.SetNRGBA(x, y, color.NRGBA{R: 0, G: 0, B: 0, A: alpha})
		}
	}

	out, err := os.Create("build/tray/tray-template.png")
	if err != nil {
		log.Fatal(err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		log.Fatal(err)
	}
	log.Printf("已生成 build/tray/tray-template.png（%d×%d）", size, size)
}

// inRoundedRect 判断点是否落在圆角矩形内。
func inRoundedRect(x, y float64, r rect, radius float64) bool {
	if x < r.x0 || x > r.x1 || y < r.y0 || y > r.y1 {
		return false
	}
	// 四个角用半径 radius 的圆弧裁切。
	cx := clamp(x, r.x0+radius, r.x1-radius)
	cy := clamp(y, r.y0+radius, r.y1-radius)
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= radius*radius
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
