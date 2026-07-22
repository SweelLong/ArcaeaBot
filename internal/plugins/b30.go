package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/font"
	"path/filepath"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type B30Plugin struct {
	utils.Base
	ctx       utils.Context
	assetRoot string
	songs     []utils.Song
	byID      map[string]utils.Song
}

type bestRow struct {
	Rating       float64
	Score        int64
	SongID       string
	Difficulty   int
	ClearType    int
	StdRating    int64
	Name         string
	Perfect      int64
	ShinyPerfect int64
	Near         int64
	Miss         int64
	Jacket       string
	Side         int
}

var b30IndexOnce sync.Once

func (p *B30Plugin) best(mode int) utils.Handler {
	return func(ctx context.Context, ev napcat.Event, _ string) error {
		var userID int64
		var name, code string
		var ptt int64
		var characterID int
		var uncapped, override bool
		userID, bindingErr := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
		err := bindingErr
		if err == nil {
			err = p.ctx.Arcaea.QueryRowContext(ctx, "SELECT user_id, name, user_code, rating_ptt, COALESCE(character_id,0), COALESCE(is_char_uncapped,0), COALESCE(is_char_uncapped_override,0) FROM user WHERE user_id=?", userID).Scan(&userID, &name, &code, &ptt, &characterID, &uncapped, &override)
		}
		if err != nil {
			return err
		}
		best, err := p.queryBest(ctx, userID, "best_score", mode, 30)
		if err != nil {
			return err
		}
		recent, _ := p.queryBest(ctx, userID, "recent30", mode, 10)
		if len(best) == 0 {
			return onebot.Reply(ctx, p.ctx.Client, ev, "没有可用于生成成绩卡的数据。")
		}
		b30IndexOnce.Do(func() {
			for _, idx := range []string{
				"CREATE INDEX IF NOT EXISTS idx_best_score_user_rating ON best_score(user_id, rating DESC)",
				"CREATE INDEX IF NOT EXISTS idx_best_score_user_score ON best_score(user_id, score DESC)",
				"CREATE INDEX IF NOT EXISTS idx_user_rating_ptt ON user(rating_ptt DESC)",
			} {
				if _, e := p.ctx.Arcaea.ExecContext(ctx, idx); e != nil {
					slog.Warn("b30: create index", "error", e)
				}
			}
		})
		for i := range best {
			best[i].Jacket = utils.FindJacket(p.ctx.Config.BundleSongsPath, best[i].SongID)
			if song, ok := p.byID[best[i].SongID]; ok {
				best[i].Name = utils.SongTitle(song)
				best[i].Side = song.Side
			}
		}
		for i := range recent {
			recent[i].Jacket = utils.FindJacket(p.ctx.Config.BundleSongsPath, recent[i].SongID)
			if song, ok := p.byID[recent[i].SongID]; ok {
				recent[i].Name = utils.SongTitle(song)
				recent[i].Side = song.Side
			}
		}
		hashRank := p.hashRankForUser(ctx, userID)
		path, err := p.render(b30UserInfo{
			Name: name, Code: code, RatingPTT: ptt, HashRank: hashRank,
			CharacterID: characterID, CharUncapped: uncapped, CharOverride: override, Mode: mode,
		}, best, recent)
		if err != nil {
			return err
		}
		file, err := utils.Base64File(path)
		if err != nil {
			return err
		}
		return onebot.Reply(ctx, p.ctx.Client, ev, []onebot.Segment{onebot.At(onebot.UserID(ev)), onebot.Image(file)})
	}
}

func (p *B30Plugin) queryBest(ctx context.Context, userID int64, table string, mode int, limit int) ([]bestRow, error) {
	where := ""
	switch mode {
	case 1:
		where = " AND near_count=0 AND miss_count=0"
	case 2:
		where = " AND shiny_perfect_count=perfect_count AND near_count=0 AND miss_count=0"
	}
	query := fmt.Sprintf(`SELECT s.rating, s.score, s.song_id, s.difficulty, s.best_clear_type,
CASE s.difficulty WHEN 0 THEN c.rating_pst WHEN 1 THEN c.rating_prs WHEN 2 THEN c.rating_ftr WHEN 3 THEN c.rating_byn WHEN 4 THEN c.rating_etr ELSE 0 END AS standard_rating,
COALESCE(c.name, s.song_id),
COALESCE(s.perfect_count,0), COALESCE(s.shiny_perfect_count,0), COALESCE(s.near_count,0), COALESCE(s.miss_count,0)
FROM %s s INNER JOIN chart c ON s.song_id=c.song_id
WHERE s.user_id=?%s
GROUP BY s.song_id, s.difficulty, s.rating, s.score, s.best_clear_type, s.perfect_count, s.shiny_perfect_count, s.near_count, s.miss_count, c.rating_pst, c.rating_prs, c.rating_ftr, c.rating_byn, c.rating_etr, c.name
ORDER BY s.rating DESC LIMIT ?`, table, where)
	rows, err := p.ctx.Arcaea.QueryContext(ctx, query, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bestRow
	for rows.Next() {
		var r bestRow
		if err := rows.Scan(&r.Rating, &r.Score, &r.SongID, &r.Difficulty, &r.ClearType, &r.StdRating, &r.Name, &r.Perfect, &r.ShinyPerfect, &r.Near, &r.Miss); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *B30Plugin) hashRankForUser(ctx context.Context, userID int64) int64 {
	rows, err := p.ctx.Arcaea.QueryContext(ctx, `SELECT user_id, MAX(score) AS score FROM best_score GROUP BY user_id ORDER BY score DESC`)
	if err != nil {
		return 0
	}
	defer rows.Close()
	rank := int64(1)
	for rows.Next() {
		var id int64
		var score sql.NullInt64
		if err := rows.Scan(&id, &score); err != nil {
			return 0
		}
		if id == userID {
			return rank
		}
		rank++
	}
	return 0
}

type b30UserInfo struct {
	Name         string
	Code         string
	RatingPTT    int64
	HashRank     int64
	CharacterID  int
	CharUncapped bool
	CharOverride bool
	Mode         int
}

type b30CardRow struct {
	Order                             int
	Rating                            float64
	Score                             int64
	SongID, SongName                  string
	Difficulty, ClearType             int
	StdRating                         int64
	Perfect, ShinyPerfect, Near, Miss int64
	Jacket                            string
	Side                              int
}

func (p *B30Plugin) render(user b30UserInfo, best, overflow []bestRow) (string, error) {
	out := filepath.Join(p.ctx.Config.TmpPath, "b30", fmt.Sprintf("mode_%d.jpg", user.Mode))
	const width, height = 1800, 2092
	b30 := filepath.Join(p.ctx.Config.ResourcesPath, "b30")
	bg := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(b30, "world", "4.jpg")))
	if bg == nil {
		bg = image.NewRGBA(image.Rect(0, 0, width, height))
		utils.RenderFillRect(bg, 0, 0, width, height, color.RGBA{40, 40, 58, 255})
	} else {
		bg = utils.ResizeRenderImage(bg, width, height)
	}
	if blur := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(b30, "blur.png"))); blur != nil {
		mask := utils.RenderAlphaMask(utils.ResizeRenderImage(blur, width, height))
		display := utils.ResizeRenderImage(blur, width, height)
		utils.RenderScaleAlpha(display, 0.2)
		draw.DrawMask(bg, bg.Bounds(), bg, image.Point{}, mask, image.Point{}, draw.Over)
		draw.Draw(bg, bg.Bounds(), display, image.Point{}, draw.Over)
	}
	utils.RenderPasteAsset(bg, filepath.Join(b30, "logos.png"), image.Rect(0, 0, width, height))
	usernameFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "GeosansLight.ttf", 41)
	pttIntFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "Exo-Semibold.ttf", 35)
	pttDecFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "Exo-Semibold.ttf", 27)
	dateFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "Titillium-Semibold.ttf", 41)
	footerFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "GeosansLight.ttf", 32)
	numFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "Titillium-Semibold.ttf", 27)
	ratingFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "Exo-Semibold.ttf", 23)
	songFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "L2-Semibold.ttf", 23)
	scoreFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "Exo-Semibold.ttf", 24)
	stdFace := utils.LoadRenderFont(p.ctx.Config.ResourcesPath, "Exo-Semibold.ttf", 25)
	if banner := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(b30, "course_banner_12.png"))); banner != nil {
		cropW := max(banner.Bounds().Dx()-103, 1)
		banner = utils.RenderRGBA(banner.SubImage(image.Rect(0, 0, cropW, banner.Bounds().Dy())))
		draw.Draw(bg, image.Rect(636, 196, 636+banner.Bounds().Dx(), 196+banner.Bounds().Dy()), banner, image.Point{}, draw.Over)
	}
	iconBorder := filepath.Join(p.ctx.Config.BundleCharPath, "char_icon_border.png")
	utils.RenderPasteAsset(bg, iconBorder, renderScaledRect(1016, 156, iconBorder, 1.07))
	if avatar := utils.LoadCharacterIcon(p.ctx.Config.BundleCharPath, filepath.Join(p.ctx.Config.ResourcesPath, "char", "unknown_icon.png"), user.CharacterID, user.CharUncapped, user.CharOverride); avatar != nil {
		w, h := int(float64(avatar.Bounds().Dx())*0.764), int(float64(avatar.Bounds().Dy())*0.764)
		utils.RenderPasteImage(bg, avatar, image.Rect(1032, 172, 1032+w, 172+h))
	}
	utils.RenderDrawTextBottomRight(bg, usernameFace, user.Name, 991, 239, color.White, color.RGBA{0, 0, 0, 160}, 0.02)
	ratingFile := filepath.Join(p.ctx.Config.ResourcesPath, "rating", utils.RatingBoxFile(float64(user.RatingPTT)/100))
	utils.RenderPasteAsset(bg, ratingFile, renderScaledRect(1101, 224, ratingFile, 0.83))
	drawB30PTT(bg, pttIntFace, pttDecFace, fmt.Sprintf("%.2f", float64(user.RatingPTT)/100))
	date := time.Now().Format("2006/01/02")
	utils.RenderDrawTextSpaced(bg, dateFace, date, 1329+utils.RenderTextWidth(dateFace, date)/2, 178+utils.RenderTextHeight(dateFace)/2, color.White, -0.01)
	base := loadB30TileBase(b30)
	for i := 0; i < len(best) && i < 30; i++ {
		tile := drawB30Tile(b30, p.ctx.Config.ResourcesPath, p.ctx.Config.BundleSongsPath, toB30Card(best[i], i+1), base, numFace, ratingFace, songFace, scoreFace, stdFace)
		x, y := 77+(i%5)*334, 303+(i/5)*272
		draw.Draw(bg, image.Rect(x, y, x+tile.Bounds().Dx(), y+tile.Bounds().Dy()), tile, image.Point{}, draw.Over)
	}
	utils.RenderDrawTextCenter(bg, footerFace, width/2, height-60, "Copyright 2026 © NulCorePivot (Nullear)", color.White)
	return out, utils.RenderWriteJPEG(out, bg, 70)
}

func toB30Card(r bestRow, order int) b30CardRow {
	return b30CardRow{Order: order, Rating: r.Rating, Score: r.Score, SongID: r.SongID, SongName: r.Name, Difficulty: r.Difficulty, ClearType: r.ClearType, StdRating: r.StdRating, Perfect: r.Perfect, ShinyPerfect: r.ShinyPerfect, Near: r.Near, Miss: r.Miss, Jacket: r.Jacket, Side: r.Side}
}
func renderScaledRect(x, y int, path string, scale float64) image.Rectangle {
	img := utils.LoadRenderImage(path)
	if img == nil {
		return image.Rect(x, y, x, y)
	}
	return image.Rect(x, y, x+int(float64(img.Bounds().Dx())*scale), y+int(float64(img.Bounds().Dy())*scale))
}
func drawB30PTT(img *image.RGBA, intFace, decFace font.Face, ptt string) {
	a, b, _ := strings.Cut(ptt, ".")
	if a == "" {
		a = "0"
	}
	b = (b + "00")[:2]
	b = "." + b
	total := utils.RenderTextWidth(intFace, a) + utils.RenderTextWidth(decFace, b)
	x := 1149 - total/2
	utils.RenderDrawStrokeText(img, intFace, x, 273-utils.RenderTextHeight(intFace)/2-5, a, color.White, color.RGBA{91, 70, 87, 255}, 3)
	utils.RenderDrawStrokeText(img, decFace, x+utils.RenderTextWidth(intFace, a), 273-utils.RenderTextHeight(decFace)/2, b, color.White, color.RGBA{91, 70, 87, 255}, 2)
}
func loadB30TileBase(static string) *image.RGBA {
	base := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(static, "base.png")))
	if base == nil {
		base = image.NewRGBA(image.Rect(0, 0, 309, 244))
	}
	extended := image.NewRGBA(image.Rect(0, 0, base.Bounds().Dx()+25, base.Bounds().Dy()+50))
	draw.Draw(extended, image.Rect(0, 50, base.Bounds().Dx(), 50+base.Bounds().Dy()), base, image.Point{}, draw.Over)
	return extended
}
func drawB30Tile(b30Path, resourcesPath, bundleSongsPath string, row b30CardRow, base *image.RGBA, numFace, ratingFace, songFace, scoreFace, stdFace font.Face) *image.RGBA {
	tile := image.NewRGBA(base.Bounds())
	draw.Draw(tile, tile.Bounds(), base, image.Point{}, draw.Src)
	if side, ok := map[int]string{0: "light.png", 1: "conflict.png", 2: "colorless.png", 3: "lephon.png"}[row.Side]; ok {
		utils.RenderPasteAssetAt(tile, filepath.Join(b30Path, "side", side), 75, 31)
	}
	jacket := utils.LoadRenderImage(row.Jacket)
	if jacket == nil && bundleSongsPath != "" {
		jacket = utils.LoadRenderImage(utils.FindJacket(bundleSongsPath, row.SongID))
	}
	if jacket == nil {
		jacket = utils.LoadRenderImage(filepath.Join(resourcesPath, "common", "random.jpg"))
	}
	if jacket != nil {
		utils.RenderPasteImage(tile, jacket, image.Rect(107, 63, 281, 237))
	}
	songName := utils.RenderEllipsizeFace(songFace, row.SongName, 290)
	utils.RenderDrawTextCenter(tile, songFace, tile.Bounds().Dx()/2-25, 260, songName, color.White)
	num := fmt.Sprintf("#%d", row.Order)
	utils.RenderDrawTextCenter(tile, numFace, 37, 84, num, color.RGBA{71, 71, 71, 255})
	utils.RenderDrawTextCenter(tile, numFace, 35, 82, num, color.White)
	drawB30Spaced(tile, ratingFace, fmt.Sprintf("%.2f", row.Rating), 57, 142, color.White, .02)
	if grade := utils.LoadRenderImage(filepath.Join(resourcesPath, "grade", b30GradeFile(row.Score))); grade != nil {
		w, h := int(float64(grade.Bounds().Dx())*.57), int(float64(grade.Bounds().Dy())*.57)
		utils.RenderPasteImage(tile, grade, image.Rect(61-w/2-4, 203-h/2, 61-w/2-4+w, 203-h/2+h))
	}
	utils.RenderPasteAssetFitWidth(tile, filepath.Join(resourcesPath, "clear_type", utils.CourseClearTypeFile(row.ClearType)), 107, 213, 220)
	score := b30Apostrophe(row.Score)
	utils.RenderDrawStrokeText(tile, scoreFace, 247-utils.RenderTextWidth(scoreFace, score), 229-utils.RenderTextHeight(scoreFace), score, color.White, color.RGBA{27, 12, 44, 255}, 2)
	if diff := utils.LoadRenderImage(filepath.Join(b30Path, "diff", b30DiffFile(row.Difficulty))); diff != nil {
		w, h := int(float64(diff.Bounds().Dx())*1.06), int(float64(diff.Bounds().Dy())*1.06)
		utils.RenderPasteImage(tile, diff, image.Rect(255, 87-h, 255+w, 87))
	}
	drawB30Spaced(tile, stdFace, b30StdRating(row.StdRating), 290, 50, color.White, .12)
	return tile
}
func drawB30Spaced(img *image.RGBA, face font.Face, text string, cx, cy int, c color.Color, spacing float64) {
	if text == "" {
		return
	}
	rs := []rune(text)
	total := 0.
	ws := make([]int, len(rs))
	for i, r := range rs {
		ws[i] = utils.RenderTextWidth(face, string(r))
		total += float64(ws[i])
	}
	for i := 0; i < len(ws)-1; i++ {
		total += float64(ws[i]) * spacing
	}
	x := float64(cx) - total/2
	for i, r := range rs {
		utils.RenderDrawTextTop(img, face, int(math.Round(x)), cy-utils.RenderTextHeight(face)/2, string(r), c)
		x += float64(ws[i]) + float64(ws[i])*spacing
	}
}
func b30GradeFile(score int64) string {
	switch {
	case score >= 9900000:
		return "EX+.png"
	case score >= 9800000:
		return "EX.png"
	case score >= 9500000:
		return "AA.png"
	case score >= 9200000:
		return "A.png"
	case score >= 8900000:
		return "B.png"
	case score >= 8600000:
		return "C.png"
	default:
		return "D.png"
	}
}
func b30DiffFile(diff int) string {
	if diff >= 0 && diff <= 4 {
		return []string{"diff-past.png", "diff-present.png", "diff-future.png", "diff-beyond.png", "diff-eternal.png"}[diff]
	}
	return "diff-future.png"
}
func b30StdRating(raw int64) string {
	v := float64(raw) / 10
	i := int(v)
	if v-float64(i) >= .5 {
		return fmt.Sprintf("%d+", i)
	}
	return strconv.Itoa(i)
}
func b30Apostrophe(score int64) string {
	s := strconv.FormatInt(score, 10)
	var p []string
	for len(s) > 3 {
		p = append([]string{s[len(s)-3:]}, p...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, p...), "'")
}

func init() {
	utils.Add("b30", func(ctx utils.Context) *B30Plugin {
		p := &B30Plugin{ctx: ctx, assetRoot: filepath.Join(ctx.Config.ResourcesPath, "b30")}
		p.songs, p.byID = utils.LoadSongs(ctx.Config.SonglistPath)
		p.Init("b30")
		p.Command("#最佳30", []string{"#逼30", "#b30", "#逼"}, p.best(0), "生成最佳30成绩图片")
		return p
	})
}
