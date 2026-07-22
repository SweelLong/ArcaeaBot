package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
	"gopkg.in/yaml.v3"
)

type AIPlugin struct {
	utils.Base
	ctx   utils.Context
	texts []string
}

func (p *AIPlugin) recommend(ctx context.Context, ev napcat.Event, _ string) error {
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if err != nil {
		return err
	}
	row := p.ctx.Arcaea.QueryRowContext(ctx, `SELECT best_score.song_id, best_score.difficulty, best_score.score,
chart.name, chart.rating_pst, chart.rating_prs, chart.rating_ftr, chart.rating_byn, chart.rating_etr
FROM best_score
LEFT JOIN chart ON best_score.song_id=chart.song_id
WHERE best_score.user_id=? ORDER BY RANDOM() LIMIT 1`, userID)
	var songID, name sql.NullString
	var diff int
	var score int64
	var pst, prs, ftr, byn, etr sql.NullInt64
	if err := row.Scan(&songID, &diff, &score, &name, &pst, &prs, &ftr, &byn, &etr); err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "Ai酱找不到您的分数信息呢，暂时无法为你推荐吖~")
	}
	constants := []sql.NullInt64{pst, prs, ftr, byn, etr}
	constant := "0.0"
	if diff >= 0 && diff < len(constants) && constants[diff].Valid {
		constant = fmt.Sprintf("%.1f", float64(constants[diff].Int64)/10)
	}
	text := p.texts[rand.IntN(len(p.texts))]
	replacer := strings.NewReplacer(
		"songName", nullString(name, songID.String),
		"difficulty", utils.DifficultyName(diff),
		"constant", constant,
		"score", strconv.FormatInt(score, 10),
	)
	text = replacer.Replace(text)
	jacket := utils.FindJacket(p.ctx.Config.BundleSongsPath, songID.String)
	if jacket == "" {
		jacket = filepath.Join(p.ctx.Config.ResourcesPath, "common", "random.jpg")
	}
	out := filepath.Join(p.ctx.Config.TmpPath, "ai_chan", "ai_chan.jpg")
	if err := renderAiChan(out, p.ctx.Config.ResourcesPath, jacket); err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "Ai酱那边出错了，请稍后再试哦~")
	}
	file, err := utils.Base64File(out)
	if err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, []onebot.Segment{
		onebot.At(onebot.UserID(ev)),
		onebot.Sticker(file),
		onebot.Text(text),
	})
}

func nullString(v sql.NullString, fallback string) string {
	if v.Valid && v.String != "" {
		return v.String
	}
	return fallback
}

func renderAiChan(path, resourcesPath, jacketPath string) error {
	jacket := utils.RenderRGBA(utils.LoadRenderImage(jacketPath))
	if jacket == nil {
		jacket = utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(resourcesPath, "common", "random.jpg")))
	}
	if jacket == nil {
		jacket = image.NewRGBA(image.Rect(0, 0, 256, 256))
	}
	ai := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(resourcesPath, "ai_chan", "ai_chan.png")))
	if ai != nil {
		jacketW := jacket.Bounds().Dx()
		jacketH := jacket.Bounds().Dy()
		maxW := jacketW / 2
		maxH := jacketH / 2
		aiW := ai.Bounds().Dx()
		aiH := ai.Bounds().Dy()
		if aiW > 0 && aiH > 0 {
			scale := minFloat(float64(maxW)/float64(aiW), float64(maxH)/float64(aiH))
			newW := max(1, int(float64(aiW)*scale))
			newH := max(1, int(float64(aiH)*scale))
			ai = utils.ResizeRenderImage(ai, newW, newH)
			x := jacketW - newW - 10
			y := jacketH - newH - 10
			draw.Draw(jacket, image.Rect(x, y, x+newW, y+newH), ai, image.Point{}, draw.Over)
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return jpeg.Encode(f, jacket, &jpeg.Options{Quality: 85})
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func init() {
	utils.Add("ai", func(ctx utils.Context) *AIPlugin {
		p := &AIPlugin{ctx: ctx}
		p.Init("ai")
		p.texts = []string{"Ai酱推荐你再试试 songName 的 difficulty 哦，定数 constant，之前分数是 score。"}
		if raw, err := os.ReadFile(filepath.Join(ctx.Config.ResourcesPath, "ai_chan", "ai_chan.yaml")); err == nil {
			var data []string
			if yaml.Unmarshal(raw, &data) == nil && len(data) > 0 {
				p.texts = data
			}
		}
		p.Command("#推荐", []string{"#推荐歌曲"}, p.recommend, "根据成绩推荐歌曲")
		return p
	}, "ai_chan")
}
