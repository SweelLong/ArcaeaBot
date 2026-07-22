package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"

	"golang.org/x/image/font"
	"path/filepath"
	"strings"
	"time"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type FunPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *FunPlugin) poster(sad bool) utils.Handler {
	return func(ctx context.Context, ev napcat.Event, args string) error {
		args = strings.TrimSpace(args)
		if args == "" {
			return onebot.Reply(ctx, p.ctx.Client, ev, "请输入要生成的文字。")
		}
		path := filepath.Join(p.ctx.Config.TmpPath, "fun", fmt.Sprintf("poster_%d.png", time.Now().UnixNano()))
		defer os.Remove(path)
		if err := renderXibao(path, p.ctx.Config.ResourcesPath, args, sad); err != nil {
			return onebot.Reply(ctx, p.ctx.Client, ev, "图片生成失败，请稍后再试。")
		}
		file, err := utils.Base64File(path)
		if err != nil {
			return err
		}
		return onebot.Reply(ctx, p.ctx.Client, ev, onebot.Sticker(file))
	}
}

func (p *FunPlugin) pet(ctx context.Context, ev napcat.Event, _ string) error {
	target, ok := mentionedQQ(onebot.RawMessage(ev))
	if !ok {
		target = onebot.UserID(ev)
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("@%d 摸摸头！", target))
}

func (p *FunPlugin) emoji(ctx context.Context, ev napcat.Event, args string) error {
	emojiID := strings.TrimSpace(args)
	if emojiID == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "用法：#表情回应 [表情ID]")
	}
	_, err := p.ctx.Client.Call(ctx, "set_msg_emoji_like", map[string]any{
		"message_id": ev.Int("message_id"), "emoji_id": emojiID, "set": true,
	})
	return err
}

func renderXibao(path, resourcesPath, text string, sad bool) error {
	background := "bg.png"
	fontName := "font.ttf"
	fill := color.RGBA{230, 0, 18, 255}
	stroke := color.RGBA{252, 248, 141, 255}
	lineSpacing := 16
	if sad {
		background = "beibao.png"
		fill = color.RGBA{255, 255, 255, 255}
		stroke = color.RGBA{30, 30, 30, 255}
		lineSpacing = 14
	}

	bg := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(resourcesPath, "xibao", background)))
	if bg == nil {
		return os.ErrNotExist
	}
	areaW := bg.Bounds().Dx() * 88 / 100
	areaH := bg.Bounds().Dy() * 85 / 100
	fontPath := filepath.Join(resourcesPath, "xibao", fontName)
	face, lines := fitXibaoText(text, fontPath, areaW, areaH)
	if face == nil {
		return os.ErrNotExist
	}

	img := image.NewRGBA(bg.Bounds())
	draw.Draw(img, img.Bounds(), bg, bg.Bounds().Min, draw.Src)
	totalH := len(lines)*face.Metrics().Height.Ceil() + max(len(lines)-1, 0)*lineSpacing
	y := (img.Bounds().Dy() - totalH) / 2
	for _, line := range lines {
		x := (img.Bounds().Dx() - utils.RenderTextWidth(face, line)) / 2
		for dx := -6; dx <= 6; dx++ {
			for dy := -6; dy <= 6; dy++ {
				if dx*dx+dy*dy <= 36 {
					utils.RenderDrawTextTop(img, face, x+dx, y+dy, line, stroke)
				}
			}
		}
		utils.RenderDrawTextTop(img, face, x, y, line, fill)
		y += face.Metrics().Height.Ceil() + lineSpacing
	}

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

func fitXibaoText(text, fontPath string, areaW, areaH int) (font.Face, []string) {
	var best font.Face
	var bestLines []string
	low, high := 20, 200
	for low <= high {
		mid := (low + high) / 2
		face := utils.LoadRenderFontPath(fontPath, float64(mid))
		lines := wrapXibaoText(face, text, areaW)
		totalH := len(lines) * face.Metrics().Height.Ceil()
		if len(lines) > 1 {
			totalH += (len(lines) - 1) * 16
		}
		fits := totalH <= areaH
		for _, line := range lines {
			if utils.RenderTextWidth(face, line) > areaW {
				fits = false
				break
			}
		}
		if fits {
			best, bestLines = face, lines
			low = mid + 1
		} else {
			high = mid - 1
		}
	}
	return best, bestLines
}

func wrapXibaoText(face font.Face, text string, maxWidth int) []string {
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		line := ""
		for _, r := range paragraph {
			candidate := line + string(r)
			if line != "" && utils.RenderTextWidth(face, candidate) > maxWidth {
				lines = append(lines, line)
				line = string(r)
			} else {
				line = candidate
			}
		}
		if line != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

func init() {
	utils.Add("fun", func(ctx utils.Context) *FunPlugin {
		p := &FunPlugin{ctx: ctx}
		p.Init("fun")
		p.Command("#喜报", nil, p.poster(false), "生成喜报图片")
		p.Command("#悲报", nil, p.poster(true), "生成悲报图片")
		p.Command("#摸", nil, p.pet, "发送摸头回应")
		p.Command("#表情回应", nil, p.emoji, "发送表情回应")
		return p
	})
}
