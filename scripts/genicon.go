// genicon 生成应用占位图标：蓝色圆角方形背景 + 白色圆点。
// 用法: go run ./scripts/genicon.go <output.png> <size>
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"strconv"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "用法: genicon <output.png> <size>")
		os.Exit(2)
	}
	path := os.Args[1]
	size, err := strconv.Atoi(os.Args[2])
	if err != nil || size <= 0 {
		fmt.Fprintln(os.Stderr, "size 必须是正整数")
		os.Exit(2)
	}

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	bg := color.RGBA{R: 33, G: 118, B: 255, A: 255}   // #2176FF
	dot := color.RGBA{R: 255, G: 255, B: 255, A: 255} // 白
	inner := color.RGBA{R: 33, G: 118, B: 255, A: 255}

	r := size / 5 // 圆角半径
	cx, cy := size/2, size/2

	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			if inRoundedRect(x, y, size, r) {
				img.SetRGBA(x, y, bg)
			}
			if dist2(x, y, cx, cy) <= (size/4)*(size/4) {
				img.SetRGBA(x, y, dot)
			}
			if dist2(x, y, cx, cy) <= (size/8)*(size/8) {
				img.SetRGBA(x, y, inner)
			}
		}
	}

	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "创建文件失败:", err)
		os.Exit(1)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		fmt.Fprintln(os.Stderr, "编码 PNG 失败:", err)
		os.Exit(1)
	}
}

// inRoundedRect 判定像素是否位于圆角方形内。
func inRoundedRect(x, y, size, r int) bool {
	if x < 0 || y < 0 || x >= size || y >= size {
		return false
	}
	inXBand := x >= r && x < size-r
	inYBand := y >= r && y < size-r
	if inXBand || inYBand {
		return true
	}
	// 四个角：距对应圆心不超过 r
	corners := [][2]int{
		{r, r}, {size - 1 - r, r}, {r, size - 1 - r}, {size - 1 - r, size - 1 - r},
	}
	for _, c := range corners {
		if dist2(x, y, c[0], c[1]) <= r*r {
			return true
		}
	}
	return false
}

// dist2 返回平方距离。
func dist2(x, y, cx, cy int) int {
	dx := x - cx
	dy := y - cy
	return dx*dx + dy*dy
}
