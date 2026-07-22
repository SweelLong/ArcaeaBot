package utils

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
)

func LoadRenderImage(path string) image.Image { return loadAnyImage(path) }
func LoadRenderFont(resourcesPath, name string, size float64) font.Face {
	return loadFont(resourcesPath, name, size)
}
func LoadRenderFontPath(path string, size float64) font.Face { return loadFontPath(path, size) }
func LoadCharacterIcon(charPath, fallbackPath string, characterID int, uncapped, override bool) image.Image {
	return loadCharacterIcon(charPath, fallbackPath, characterID, uncapped, override)
}
func RatingBoxFile(ptt float64) string                        { return ratingBoxFile(ptt) }
func CourseClearTypeFile(clearType int) string                { return courseClearTypeFile(clearType) }
func RenderRGBA(src image.Image) *image.RGBA                  { return toRGBA(src) }
func ResizeRenderImage(src image.Image, w, h int) *image.RGBA { return resizeRGBA(src, w, h) }
func RenderPasteAsset(dst *image.RGBA, path string, rect image.Rectangle) {
	pasteAsset(dst, path, rect)
}
func RenderPasteAssetAt(dst *image.RGBA, path string, x, y int) { pasteAssetAt(dst, path, x, y) }
func RenderPasteAssetFitWidth(dst *image.RGBA, path string, x, y, width int) {
	pasteAssetFitWidth(dst, path, x, y, width)
}
func RenderPasteImage(dst *image.RGBA, src image.Image, rect image.Rectangle) {
	pasteImage(dst, src, rect)
}
func RenderFillRect(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	fillRect(img, x1, y1, x2, y2, c)
}
func RenderDrawLine(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	drawLine(img, x1, y1, x2, y2, c)
}
func RenderDrawEllipse(img *image.RGBA, x1, y1, x2, y2 int, c color.Color) {
	drawEllipse(img, x1, y1, x2, y2, c)
}
func RenderDrawRoundedRect(img *image.RGBA, x1, y1, x2, y2, radius int, c color.Color) {
	drawRoundedRect(img, x1, y1, x2, y2, radius, c)
}
func RenderDrawTinyCup(img *image.RGBA, x, y int, c color.RGBA) { drawTinyCup(img, x, y, c) }
func RenderDifficultyColor(diff string) color.RGBA              { return difficultyColor(diff) }
func RenderTruncateRatingName(name string) string               { return truncateRatingName(name) }
func RenderCourseClearTypeFile(clearType int) string            { return courseClearTypeFile(clearType) }
func RenderDrawTextTop(img *image.RGBA, face font.Face, x, y int, text string, c color.Color) {
	drawTextTop(img, face, x, y, text, c)
}
func RenderDrawTextCenter(img *image.RGBA, face font.Face, x, y int, text string, c color.Color) {
	drawTextCenter(img, face, x, y, text, c)
}
func RenderDrawTextLeftMiddle(img *image.RGBA, face font.Face, x, y int, text string, c color.Color) {
	drawTextLeftMiddle(img, face, x, y, text, c)
}
func RenderDrawShadowText(img *image.RGBA, face font.Face, x, y int, text string, main, shadow color.Color) {
	drawShadowText(img, face, x, y, text, main, shadow)
}
func RenderDrawTextBottomRight(img *image.RGBA, face font.Face, text string, right, bottom int, main, stroke color.Color, ratio float64) {
	drawTextBottomRight(img, face, text, right, bottom, main, stroke, ratio)
}
func RenderDrawTextSpaced(img *image.RGBA, face font.Face, text string, cx, cy int, c color.Color, spacing float64) {
	drawTextSpaced(img, face, text, cx, cy, c, spacing)
}
func RenderDrawPurpleText(img *image.RGBA, face font.Face, x, y int, text string) {
	drawPurpleText(img, face, x, y, text)
}
func RenderDrawStrokeText(img *image.RGBA, face font.Face, x, y int, text string, main, stroke color.Color, width int) {
	drawStrokeText(img, face, x, y, text, main, stroke, width)
}
func RenderTextWidth(face font.Face, text string) int { return textW(face, text) }
func RenderTextHeight(face font.Face) int             { return textH(face) }
func RenderWrapRunes(face font.Face, text string, maxWidth int) []string {
	return wrapRunes(face, text, maxWidth)
}
func RenderEllipsizeFace(face font.Face, text string, maxWidth int) string {
	return ellipsizeFace(face, text, maxWidth)
}
func RenderWritePNG(path string, img image.Image) error { return writePNG(path, img) }
func RenderWriteJPEG(path string, img image.Image, quality int) error {
	return writeJPEG(path, img, quality)
}
func RenderAlphaMask(src *image.RGBA) *image.Alpha          { return alphaMask(src) }
func RenderScaleAlpha(img *image.RGBA, ratio float64)       { scaleAlpha(img, ratio) }
func RenderDrawMask(dst *image.RGBA, r image.Rectangle, src image.Image, sp image.Point, mask image.Image, mp image.Point, op draw.Op) {
	draw.DrawMask(dst, r, src, sp, mask, mp, op)
}

func RenderDraw(dst *image.RGBA, r image.Rectangle, src image.Image, sp image.Point, op draw.Op) {
	draw.Draw(dst, r, src, sp, op)
}
