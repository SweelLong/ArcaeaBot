package utils

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

func loadFont(resourcesPath, name string, size float64) font.Face {
	raw, err := os.ReadFile(filepath.Join(resourcesPath, "fonts", name))
	if err == nil {
		if ft, parseErr := opentype.Parse(raw); parseErr == nil {
			if face, faceErr := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull}); faceErr == nil {
				return face
			}
		}
	}
	return basicfont.Face7x13
}

func loadFontPath(path string, size float64) font.Face {
	raw, err := os.ReadFile(path)
	if err == nil {
		if ft, parseErr := opentype.Parse(raw); parseErr == nil {
			if face, faceErr := opentype.NewFace(ft, &opentype.FaceOptions{Size: size, DPI: 72, Hinting: font.HintingFull}); faceErr == nil {
				return face
			}
		}
	}
	return basicfont.Face7x13
}

func alphaMask(src *image.RGBA) *image.Alpha {
	mask := image.NewAlpha(src.Bounds())
	for y := src.Bounds().Min.Y; y < src.Bounds().Max.Y; y++ {
		for x := src.Bounds().Min.X; x < src.Bounds().Max.X; x++ {
			_, _, _, a := src.At(x, y).RGBA()

			mask.SetAlpha(x, y, color.Alpha{uint8(a >> 8)})

		}
	}
	return mask
}

func scaleAlpha(img *image.RGBA, ratio float64) {
	for y := img.Bounds().Min.Y; y < img.Bounds().Max.Y; y++ {
		for x := img.Bounds().Min.X; x < img.Bounds().Max.X; x++ {
			c := img.RGBAAt(x, y)
			c.A = uint8(float64(c.A) * ratio)
			img.SetRGBA(x, y, c)
		}
	}
}

func scaledRect(x, y int, path string, scale float64) image.Rectangle {
	img := loadAnyImage(path)
	if img == nil {
		return image.Rect(x, y, x, y)
	}
	return image.Rect(x, y, x+int(float64(img.Bounds().Dx())*scale), y+int(float64(img.Bounds().Dy())*scale))
}

func drawTextBottomRight(img *image.RGBA, face font.Face, text string, right, bottom int, main, stroke color.Color, strokeRatio float64) {
	x := right - textW(face, text) + 20
	y := bottom - 30
	sw := max(1, int(math.Round(float64(textH(face))*strokeRatio)))
	drawStrokeText(img, face, x, y, text, main, stroke, sw)
}

func drawTextSpaced(img *image.RGBA, face font.Face, text string, cx, cy int, c color.Color, spacingRatio float64) {
	if text == "" {
		return
	}
	rs := []rune(text)
	widths := make([]int, len(rs))
	total := 0.0
	for i, r := range rs {
		widths[i] = textW(face, string(r))
		total += float64(widths[i])
	}
	for i := 0; i < len(widths)-1; i++ {
		total += float64(widths[i]) * spacingRatio
	}
	x := float64(cx) - total/2
	y := cy - textH(face)/2
	for i, r := range rs {
		drawTextTop(img, face, int(math.Round(x)), y, string(r), c)
		x += float64(widths[i]) + float64(widths[i])*spacingRatio
	}
}

func loadAnyImage(path string) image.Image {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil
	}
	return img
}

func loadCharacterIcon(charPath, fallbackPath string, characterID int, uncapped, override bool) image.Image {
	name := fmt.Sprintf("%d_icon.png", characterID)
	if uncapped && !override {
		name = fmt.Sprintf("%du_icon.png", characterID)
	}
	if icon := loadAnyImage(filepath.Join(charPath, name)); icon != nil {

		return icon
	}
	if icon := loadAnyImage(filepath.Join(charPath, "unknown_icon.png")); icon != nil {
		return icon
	}
	return loadAnyImage(fallbackPath)
}

func toRGBA(src image.Image) *image.RGBA {
	if src == nil {
		return nil
	}
	if rgba, ok := src.(*image.RGBA); ok {
		return rgba
	}
	dst := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min, draw.Src)
	return dst
}

func resizeRGBA(src image.Image, w, h int) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.ApproxBiLinear.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	return dst
}

func pasteAsset(dst *image.RGBA, path string, rect image.Rectangle) {
	if img := loadAnyImage(path); img != nil {
		pasteImage(dst, img, rect)
	}
}

func pasteAssetAt(dst *image.RGBA, path string, x, y int) {
	if img := loadAnyImage(path); img != nil {
		pasteOriginal(dst, img, x, y)
	}
}

func pasteAssetFitWidth(dst *image.RGBA, path string, x, y, width int) {
	img := loadAnyImage(path)
	if img == nil || img.Bounds().Dx() == 0 {
		return
	}
	height := int(math.Round(float64(img.Bounds().Dy()) * float64(width) / float64(img.Bounds().Dx())))
	pasteImage(dst, img, image.Rect(x, y, x+width, y+height))
}

func pasteImage(dst *image.RGBA, src image.Image, rect image.Rectangle) {
	tmp := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	xdraw.ApproxBiLinear.Scale(tmp, tmp.Bounds(), src, src.Bounds(), draw.Src, nil)
	draw.Draw(dst, rect, tmp, image.Point{}, draw.Over)
}

func pasteOriginal(dst *image.RGBA, src image.Image, x, y int) {

	src = toRGBA(src)
	rect := image.Rect(x, y, x+src.Bounds().Dx(), y+src.Bounds().Dy())
	draw.Draw(dst, rect, src, src.Bounds().Min, draw.Over)
}

func fillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	draw.Draw(img, image.Rect(x1, y1, x2, y2), image.NewUniform(c), image.Point{}, draw.Over)
}

func drawRoundedRect(img *image.RGBA, x1, y1, x2, y2, radius int, c color.Color) {
	if x2 <= x1 || y2 <= y1 {
		return
	}
	mask := image.NewAlpha(image.Rect(0, 0, x2-x1, y2-y1))
	w, h := mask.Bounds().Dx(), mask.Bounds().Dy()
	r := min(radius, min(w, h)/2)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			ok := true
			if x < r && y < r {
				ok = (x-r)*(x-r)+(y-r)*(y-r) <= r*r
			} else if x >= w-r && y < r {
				ok = (x-(w-r-1))*(x-(w-r-1))+(y-r)*(y-r) <= r*r
			} else if x < r && y >= h-r {
				ok = (x-r)*(x-r)+(y-(h-r-1))*(y-(h-r-1)) <= r*r
			} else if x >= w-r && y >= h-r {
				ok = (x-(w-r-1))*(x-(w-r-1))+(y-(h-r-1))*(y-(h-r-1)) <= r*r
			}
			if ok {
				mask.SetAlpha(x, y, color.Alpha{255})
			}
		}
	}
	draw.DrawMask(img, image.Rect(x1, y1, x2, y2), image.NewUniform(c), image.Point{}, mask, image.Point{}, draw.Over)
}

func drawEllipse(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	w, h := x2-x1, y2-y1
	if w <= 0 || h <= 0 {
		return
	}
	mask := image.NewAlpha(image.Rect(0, 0, w, h))
	rx, ry := float64(w)/2, float64(h)/2
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			dx := (float64(x) + 0.5 - rx) / rx
			dy := (float64(y) + 0.5 - ry) / ry
			if dx*dx+dy*dy <= 1 {
				mask.SetAlpha(x, y, color.Alpha{255})
			}
		}
	}
	draw.DrawMask(img, image.Rect(x1, y1, x2, y2), image.NewUniform(c), image.Point{}, mask, image.Point{}, draw.Over)
}

func drawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	dx := abs(x2 - x1)
	dy := -abs(y2 - y1)
	sx, sy := -1, -1
	if x1 < x2 {
		sx = 1
	}
	if y1 < y2 {
		sy = 1
	}
	err := dx + dy
	for {
		if image.Pt(x1, y1).In(img.Bounds()) {

			img.Set(x1, y1, c)

		}
		if x1 == x2 && y1 == y2 {
			break
		}
		e2 := 2 * err
		if e2 >= dy {
			err += dy
			x1 += sx
		}
		if e2 <= dx {
			err += dx
			y1 += sy
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func drawTextTop(img *image.RGBA, face font.Face, x, y int, text string, c color.Color) {
	d := &font.Drawer{Dst: img, Src: image.NewUniform(c), Face: face, Dot: fixed.P(x, y+face.Metrics().Ascent.Ceil())}
	d.DrawString(text)
}

func drawTextCenter(img *image.RGBA, face font.Face, x, y int, text string, c color.Color) {
	drawTextTop(img, face, x-textW(face, text)/2, y-textH(face)/2, text, c)
}

func drawTextLeftMiddle(img *image.RGBA, face font.Face, x, y int, text string, c color.Color) {
	drawTextTop(img, face, x, y-textH(face)/2, text, c)
}

func drawShadowText(img *image.RGBA, face font.Face, x, y int, text string, main, shadow color.Color) {
	drawTextTop(img, face, x+2, y+2, text, shadow)
	drawTextTop(img, face, x, y, text, main)
}

func drawPurpleText(img *image.RGBA, face font.Face, x, y int, text string) {
	border := color.RGBA{128, 0, 128, 255}
	main := color.White
	drawTextTop(img, face, x-1, y, text, border)
	drawTextTop(img, face, x+1, y, text, border)
	drawTextTop(img, face, x, y-1, text, border)
	drawTextTop(img, face, x, y+1, text, border)
	drawTextTop(img, face, x, y, text, main)
}

func drawStrokeText(img *image.RGBA, face font.Face, x, y int, text string, main, stroke color.Color, width int) {
	for _, off := range [][2]int{{-1, -1}, {0, -1}, {1, -1}, {-1, 0}, {1, 0}, {-1, 1}, {0, 1}, {1, 1}} {
		drawTextTop(img, face, x+off[0]*width, y+off[1]*width, text, stroke)
	}
	drawTextTop(img, face, x, y, text, main)
}

func textW(face font.Face, text string) int {
	return (&font.Drawer{Face: face}).MeasureString(text).Ceil()
}

func textH(face font.Face) int {
	m := face.Metrics()
	return (m.Ascent + m.Descent).Ceil()
}

func wrapRunes(face font.Face, text string, maxWidth int) []string {
	var lines []string
	var b strings.Builder
	for _, r := range text {
		next := b.String() + string(r)
		if textW(face, next) <= maxWidth {
			b.WriteRune(r)
			continue
		}
		if b.Len() > 0 {
			lines = append(lines, b.String())
			b.Reset()
		}
		b.WriteRune(r)
	}
	if b.Len() > 0 {
		lines = append(lines, b.String())

	}
	return lines
}

func ellipsizeFace(face font.Face, text string, maxWidth int) string {
	if textW(face, text) <= maxWidth {
		return text
	}
	rs := []rune(text)
	for len(rs) > 1 && textW(face, string(rs)+"...") > maxWidth {
		rs = rs[:len(rs)-1]
	}
	return string(rs) + "..."
}

func writeJPEG(path string, img image.Image, quality int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, img, &jpeg.Options{Quality: quality})
}

func writePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	return png.Encode(f, img)
}

func difficultyColor(diff string) color.RGBA {
	switch diff {
	case "pst":
		return color.RGBA{0, 153, 255, 255}
	case "prs":
		return color.RGBA{0, 255, 136, 255}
	case "ftr":
		return color.RGBA{153, 50, 204, 255}
	case "byn":
		return color.RGBA{220, 20, 60, 255}
	case "etr":
		return color.RGBA{147, 112, 219, 255}
	default:
		return color.RGBA{0, 0, 0, 255}
	}
}

func truncateRatingName(name string) string {
	if name == "" {
		return "未知曲目"
	}
	name = strings.ReplaceAll(strings.ReplaceAll(name, `"`, ""), `'`, "")
	if len([]rune(name)) > 30 {
		reParen := regexp.MustCompile(`\([^)]*\)`)
		reBracket := regexp.MustCompile(`\[[^\]]*\]`)
		s := strings.TrimSpace(reBracket.ReplaceAllString(reParen.ReplaceAllString(name, ""), ""))
		if s != "" && len([]rune(s)) < len([]rune(name)) {
			return s
		}
	}
	return name
}

func courseClearTypeFile(clearType int) string {
	switch clearType {
	case 0:
		return "fail.png"
	case 1:
		return "normal.png"
	case 2:
		return "full.png"
	case 3, 6:
		return "pure.png"
	case 4:
		return "easy.png"
	case 5:
		return "hard.png"
	default:

		return "normal.png"
	}
}

func ratingBoxFile(ptt float64) string {
	switch {
	case ptt >= 13.5:
		return "rating_8.png"
	case ptt >= 13.0:

		return "rating_7.png"
	case ptt >= 12.5:
		return "rating_6.png"
	case ptt >= 12.0:
		return "rating_5.png"
	case ptt >= 11.0:
		return "rating_4.png"
	case ptt >= 10.0:
		return "rating_3.png"
	case ptt >= 7.0:
		return "rating_2.png"
	case ptt >= 3.5:
		return "rating_1.png"
	default:
		return "rating_0.png"
	}
}

func drawTinyCup(img *image.RGBA, x, y int, c color.RGBA) {
	fillRect(img, x+4, y, x+16, y+2, c)
	drawEllipse(img, x+2, y+2, x+8, y+10, c)
	drawEllipse(img, x+12, y+2, x+18, y+10, c)
	drawEllipse(img, x+6, y+4, x+14, y+14, c)
	fillRect(img, x+8, y+10, x+12, y+14, c)
	drawEllipse(img, x+5, y+14, x+15, y+20, c)
	fillRect(img, x+7, y+18, x+13, y+22, c)
	drawEllipse(img, x+3, y+22, x+17, y+26, c)
}
