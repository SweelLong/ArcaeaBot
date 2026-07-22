package plugins

import (
	"arcaeabot/internal/utils"
	"bytes"
	"context"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type StickerPlugin struct {
	utils.Base
	ctx utils.Context
}

const stickerReplyCooldown = 10 * time.Second

var stickerReplyState struct {
	sync.Mutex
	last time.Time
}

type imageSegment struct {
	File string
	URL  string
}

func (p *StickerPlugin) process(ctx context.Context, ev napcat.Event) error {
	selfID := p.ctx.Client.SelfID()
	if selfID != 0 && onebot.UserID(ev) == selfID {
		return nil
	}
	images := imageSegments(ev)
	if len(images) == 0 {
		return nil
	}
	probability := 0.5
	if p.ctx.Config.StickerMirrorProbability != nil {
		probability = *p.ctx.Config.StickerMirrorProbability
	}
	if rand.Float64() >= probability {
		return nil
	}
	item := images[0]
	out, err := p.processOne(ctx, item.File, item.URL)
	if err != nil {
		slog.Warn("random mirror image processing failed", "file", item.File, "error", err)
		return nil
	}
	file, err := utils.Base64File(out)
	if err != nil {
		slog.Warn("random mirror output loading failed", "path", out, "error", err)
		return nil
	}

	stickerReplyState.Lock()
	if elapsed := time.Since(stickerReplyState.last); elapsed < stickerReplyCooldown {
		stickerReplyState.Unlock()
		return nil
	}
	stickerReplyState.last = time.Now()
	stickerReplyState.Unlock()

	msg := onebot.Sticker(file)
	if onebot.IsGroupMessage(ev) {
		if err := onebot.SendGroup(ctx, p.ctx.Client, onebot.GroupID(ev), []onebot.Segment{msg}); err != nil {
			slog.Warn("random mirror image send failed", "group_id", onebot.GroupID(ev), "error", err)
		}
	} else if onebot.IsPrivateMessage(ev) {
		if err := onebot.SendPrivate(ctx, p.ctx.Client, onebot.UserID(ev), []onebot.Segment{msg}); err != nil {
			slog.Warn("random mirror image send failed", "user_id", onebot.UserID(ev), "error", err)
		}
	}
	return nil
}

func (p *StickerPlugin) processOne(ctx context.Context, file, imgURL string) (string, error) {
	var raw []byte
	var ext string
	if imgURL != "" {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, imgURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		raw, err = io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		ct := resp.Header.Get("Content-Type")
		switch {
		case strings.Contains(ct, "gif"):
			ext = ".gif"
		case strings.Contains(ct, "png"):
			ext = ".png"
		default:
			ext = ".jpg"
		}
	} else {
		resolved := strings.TrimPrefix(file, "file://")
		if _, err := os.Stat(resolved); err != nil {
			data, callErr := p.ctx.Client.Call(ctx, "get_image", map[string]any{"file": file})
			if callErr != nil {
				return "", callErr
			}
			resolved, _ = data["file"].(string)
			resolved = strings.TrimPrefix(resolved, "file://")
			if resolved == "" {
				return "", os.ErrNotExist
			}
		}
		f, err := os.Open(resolved)
		if err != nil {
			return "", err
		}
		defer f.Close()
		raw, err = io.ReadAll(f)
		if err != nil {
			return "", err
		}
		ext = strings.ToLower(filepath.Ext(resolved))
	}
	if ext == ".gif" {
		out := filepath.Join(p.ctx.Config.TmpPath, "sticker", "sticker_processed.gif")
		return out, mirrorGIF(out, raw)
	}
	if ext != ".jpg" && ext != ".jpeg" {
		ext = ".png"
	} else {
		ext = ".jpg"
	}
	out := filepath.Join(p.ctx.Config.TmpPath, "sticker", "sticker_processed"+ext)
	return out, mirrorStatic(out, raw, ext)
}

func imageSegments(ev napcat.Event) []imageSegment {
	segs, ok := ev["message"].([]any)
	if !ok || len(segs) == 0 {
		return nil
	}
	out := make([]imageSegment, 0, len(segs))
	for _, raw := range segs {
		seg, ok := raw.(map[string]any)
		if !ok || seg["type"] != "image" {
			continue
		}
		data, _ := seg["data"].(map[string]any)
		file, _ := data["file"].(string)
		url, _ := data["url"].(string)
		if file == "" && url == "" {
			continue
		}
		out = append(out, imageSegment{File: file, URL: url})
	}
	return out
}

func mirrorStatic(out string, raw []byte, ext string) error {
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	processed := mirrorFrame(img, rand.IntN(2) == 1)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if ext == ".jpg" {
		return jpeg.Encode(f, processed, &jpeg.Options{Quality: 95})
	}
	return png.Encode(f, processed)
}

func mirrorGIF(out string, raw []byte) error {
	src, err := gif.DecodeAll(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	w, h := src.Config.Width, src.Config.Height
	canvas := image.NewRGBA(image.Rect(0, 0, w, h))
	var prev *image.RGBA
	reverse := rand.IntN(2) == 1
	dst := &gif.GIF{
		Delay:     src.Delay,
		LoopCount: src.LoopCount,
	}
	for i, frame := range src.Image {
		if src.Disposal[i] == gif.DisposalPrevious {
			prev = image.NewRGBA(image.Rect(0, 0, w, h))
			draw.Draw(prev, prev.Bounds(), canvas, image.Point{}, draw.Src)
		}
		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)
		mirrored := mirrorFrame(canvas, reverse)
		paletted := image.NewPaletted(image.Rect(0, 0, w, h), frame.Palette)
		draw.Draw(paletted, paletted.Bounds(), mirrored, image.Point{}, draw.Src)
		dst.Image = append(dst.Image, paletted)
		switch src.Disposal[i] {
		case gif.DisposalBackground:
			canvas = image.NewRGBA(image.Rect(0, 0, w, h))
		case gif.DisposalPrevious:
			if prev != nil {
				canvas = image.NewRGBA(image.Rect(0, 0, w, h))
				draw.Draw(canvas, canvas.Bounds(), prev, image.Point{}, draw.Src)
			}
		}
	}
	dst.Config = src.Config
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	return gif.EncodeAll(f, dst)
}

func mirrorFrame(src image.Image, reverse bool) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	rgba := supportToRGBA(src)
	half := w / 2
	if reverse {
		draw.Draw(dst, image.Rect(w-half, 0, w, h), rgba, image.Point{X: half}, draw.Over)
	} else {
		draw.Draw(dst, image.Rect(0, 0, half, h), rgba, image.Point{}, draw.Over)
	}
	if w%2 == 1 {
		for y := 0; y < h; y++ {
			dst.Set(half, y, rgba.At(half, y))
		}
	}
	for y := 0; y < h; y++ {
		for x := 0; x < half; x++ {
			if reverse {
				dst.Set(x, y, dst.At(w-1-x, y))
			} else {
				dst.Set(w-1-x, y, dst.At(x, y))
			}
		}
	}
	return dst
}

func supportToRGBA(src image.Image) *image.RGBA {
	b := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Over)
	return dst
}

func init() {
	utils.Add("sticker", func(ctx utils.Context) *StickerPlugin {
		p := &StickerPlugin{ctx: ctx}
		p.Init("sticker")
		p.GroupFilter(p.process)
		p.PrivateFilter(p.process)
		return p
	}, "sticker_processor")
}
