package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"arcaeabot/internal/database"
	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
	xdraw "golang.org/x/image/draw"
)

type StatsPlugin struct {
	utils.Base
	ctx   utils.Context
	songs []utils.Song
	byID  map[string]utils.Song
}

type RankItem struct {
	Name  string
	Value float64
}

type RatingRecord struct {
	SongID     string
	Name       string
	Difficulty string
	Rating     int
}

type CourseRankItem struct {
	Rank      int
	UserName  string
	Score     int64
	ClearType int
}

type CourseInfo struct {
	ID   string
	Name string
}

func (p *StatsPlugin) rank(ctx context.Context, ev napcat.Event, args string) error {
	if strings.Contains(args, "#") || strings.Contains(strings.ToLower(args), "hash") {
		return p.topHash(ctx, ev)
	}
	return p.topPTT(ctx, ev)
}

func (p *StatsPlugin) topPTT(ctx context.Context, ev napcat.Event) error {
	rows, err := p.ctx.Arcaea.QueryContext(ctx, "SELECT name, rating_ptt FROM user ORDER BY rating_ptt DESC LIMIT ?", p.ctx.Config.PTTRankLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []RankItem{}
	for rows.Next() {
		var name string
		var ptt int64
		if err := rows.Scan(&name, &ptt); err != nil {
			return err
		}
		items = append(items, RankItem{Name: name, Value: float64(ptt) / 100})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	out := filepath.Join(p.ctx.Config.TmpPath, "rank", "ptt.jpg")
	if err := renderRank(out, p.ctx.Config.ResourcesPath, p.ctx.Config.GameName+" POTENTIAL Rankings", "PTT", items); err != nil {
		return err
	}
	return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
}

func (p *StatsPlugin) topHash(ctx context.Context, ev napcat.Event) error {
	rows, err := p.ctx.Arcaea.QueryContext(ctx, `SELECT u.name, MAX(b.score) AS score FROM best_score b INNER JOIN user u ON b.user_id=u.user_id GROUP BY u.user_id, u.name ORDER BY score DESC LIMIT ?`, p.ctx.Config.HashRankLimit)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []RankItem{}
	for rows.Next() {
		var name string
		var score int64
		if err := rows.Scan(&name, &score); err != nil {
			return err
		}
		items = append(items, RankItem{Name: name, Value: float64(score)})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	out := filepath.Join(p.ctx.Config.TmpPath, "rank", "hash.jpg")
	if err := renderRank(out, p.ctx.Config.ResourcesPath, p.ctx.Config.GameName+" HASH Rankings", "#", items); err != nil {
		return err
	}
	return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
}

func (p *StatsPlugin) rankHash(ctx context.Context, ev napcat.Event, _ string) error {
	return p.topHash(ctx, ev)
}

func (p *StatsPlugin) hashRank(ctx context.Context, ev napcat.Event, _ string) error {
	return p.myRank(ctx, ev, "hash")
}

func (p *StatsPlugin) pttRank(ctx context.Context, ev napcat.Event, _ string) error {
	return p.myRank(ctx, ev, "ptt")
}

func (p *StatsPlugin) myRank(ctx context.Context, ev napcat.Event, typ string) error {
	var userID int64
	var name string
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if err != nil {
		return err
	}
	if err := p.ctx.Arcaea.QueryRowContext(ctx, "SELECT name FROM user WHERE user_id=?", userID).Scan(&name); err != nil {
		return err
	}
	if typ == "ptt" {
		var rank int64
		_ = p.ctx.Arcaea.QueryRowContext(ctx, "SELECT COUNT(*)+1 FROM user WHERE rating_ptt > (SELECT rating_ptt FROM user WHERE user_id=?)", userID).Scan(&rank)
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("%s 的PTT排名：No.%d", name, rank))
	}
	var rank int64
	_ = p.ctx.Arcaea.QueryRowContext(ctx, `SELECT COUNT(*)+1 FROM (SELECT user_id, MAX(score) s FROM best_score GROUP BY user_id) t WHERE t.s > (SELECT MAX(score) FROM best_score WHERE user_id=?)`, userID).Scan(&rank)
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("%s 的HASH排名：#%d", name, rank))
}

func (p *StatsPlugin) rating(ctx context.Context, ev napcat.Event, args string) error {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "用法：#定数查询 [定数] 或 #定数查询 [起始定数] [结束定数]")
	}
	start, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "定数转换失败，请检查输入！")
	}
	end := start
	isRange := false
	if len(fields) >= 2 {
		end, err = strconv.ParseFloat(fields[1], 64)
		if err != nil {
			return onebot.Reply(ctx, p.ctx.Client, ev, "定数范围转换失败，请检查输入！")
		}
		if start > end {
			start, end = end, start
		}
		isRange = true
	}
	startInt, endInt := int(start*10), int(end*10)
	rows, err := p.ctx.Arcaea.QueryContext(ctx, `
SELECT song_id, name, 'ftr' AS difficulty, rating_ftr FROM chart WHERE rating_ftr BETWEEN ? AND ?
UNION ALL SELECT song_id, name, 'prs' AS difficulty, rating_prs FROM chart WHERE rating_prs BETWEEN ? AND ?
UNION ALL SELECT song_id, name, 'pst' AS difficulty, rating_pst FROM chart WHERE rating_pst BETWEEN ? AND ?
UNION ALL SELECT song_id, name, 'byn' AS difficulty, rating_byn FROM chart WHERE rating_byn BETWEEN ? AND ?
UNION ALL SELECT song_id, name, 'etr' AS difficulty, rating_etr FROM chart WHERE rating_etr BETWEEN ? AND ?`,
		startInt, endInt, startInt, endInt, startInt, endInt, startInt, endInt, startInt, endInt)
	if err != nil {
		return err
	}
	defer rows.Close()
	records := []RatingRecord{}
	for rows.Next() {
		var rec RatingRecord
		if err := rows.Scan(&rec.SongID, &rec.Name, &rec.Difficulty, &rec.Rating); err != nil {
			return err
		}
		if rec.Rating < 0 {
			continue
		}
		records = append(records, rec)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(records) == 0 {
		if isRange {
			return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("未找到定数在 %.1f 到 %.1f 之间的谱面", start, end))
		}
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("未找到定数为 %.1f 的谱面", start))
	}
	out := filepath.Join(p.ctx.Config.TmpPath, "rating", "rating.jpg")
	label := fmt.Sprintf("Level %.1f", start)
	if isRange {
		label = fmt.Sprintf("Level %.1f ~ %.1f", start, end)
	}
	if err := renderRating(out, p.ctx.Config.ResourcesPath, p.ctx.Config.BundleSongsPath, p.ctx.Config.GameName, records, label, isRange); err != nil {
		return err
	}
	return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
}

func (p *StatsPlugin) recent(ctx context.Context, ev napcat.Event, _ string) error {
	var userID int64
	var data recentImageData
	var songID string
	var played int64
	userID, bindingErr := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if bindingErr != nil {
		return bindingErr
	}
	err := p.ctx.Arcaea.QueryRowContext(ctx, `SELECT u.user_id, u.name, u.rating_ptt, COALESCE(u.character_id,0), COALESCE(u.is_char_uncapped,0), COALESCE(u.is_char_uncapped_override,0),
COALESCE(u.song_id,''), COALESCE(u.difficulty,0), COALESCE(u.score,0), COALESCE(u.rating,0),
COALESCE(u.time_played,0), COALESCE(u.perfect_count,0), COALESCE(u.shiny_perfect_count,0), COALESCE(u.near_count,0), COALESCE(u.miss_count,0),
COALESCE(u.clear_type,1), COALESCE(u.health,100)
FROM user u
	WHERE u.user_id=? LIMIT 1`, userID).Scan(
		&userID, &data.PlayerName, &data.RatingPTT, &data.CharacterID, &data.CharUncapped, &data.CharOverride,
		&songID, &data.Difficulty, &data.Score, &data.Rating, &played,
		&data.Pure, &data.ShinyPure, &data.Far, &data.Lost, &data.ClearType, &data.Health)
	if errors.Is(err, sql.ErrNoRows) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "没有最近游玩记录。")
	}
	if err != nil {
		return err
	}
	if songID == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "没有最近游玩记录。")
	}
	data.BotName = p.ctx.Config.BotName
	data.SongID = songID
	data.SongName = songID
	data.Artist = "未知曲师"
	data.JacketDesigner = "未知画师"
	data.ChartDesigner = "未知谱师"
	data.Constant = "？"
	data.BestConstant = "？"
	if played > 0 {
		data.PlayedAt = unixMilliString(played)
	} else {
		data.PlayedAt = "-"
	}
	data.Potential = fmt.Sprintf("%.2f", float64(data.RatingPTT)/100)
	data.HashRank = "?"
	if r := simpleHashRankForUser(ctx, p.ctx.Arcaea, userID); r > 0 {
		data.HashRank = strconv.FormatInt(r, 10)
	}
	if song, ok := p.byID[songID]; ok {
		data.SongName = utils.SongTitle(song)
		data.Artist = song.Artist
		for _, diff := range song.Difficulties {
			if diff.RatingClass == data.Difficulty {
				data.Constant = fmt.Sprintf("%d", diff.Rating)
				if diff.RatingPlus {
					data.Constant += "+"
				}
				data.BestConstant = data.Constant
				data.ChartDesigner = diff.ChartDesigner
				break
			}
		}
	}
	data.Grade = gradeName(data.Score)
	data.Illustration = utils.FindJacket(p.ctx.Config.BundleSongsPath, songID)
	out := filepath.Join(p.ctx.Config.TmpPath, "recent", "recent.jpg")
	if err := renderRecent(out, p.ctx.Config.ResourcesPath, p.ctx.Config.BundleCharPath, data); err != nil {
		return err
	}
	return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
}

func (p *StatsPlugin) course(ctx context.Context, ev napcat.Event, args string) error {
	courseID := strings.TrimSpace(args)
	if courseID == "" {
		courses, err := p.queryCourses(ctx)
		if err != nil || len(courses) == 0 {
			return onebot.Reply(ctx, p.ctx.Client, ev, "未查询到任何课题数据")
		}
		out := filepath.Join(p.ctx.Config.TmpPath, "course", "course_mapping.jpg")
		if err := renderCourseMapping(out, p.ctx.Config.ResourcesPath, p.ctx.Config.GameName, courses); err != nil {
			return err
		}
		return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
	}
	rows, err := p.queryCourseRank(ctx, courseID)
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "课题排行榜暂时不可用或数据库表不存在。")
	}
	if len(rows) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("未查询到课题「%s」有效数据", courseID))
	}
	out := filepath.Join(p.ctx.Config.TmpPath, "course", "course.jpg")
	courseName := "Course " + courseID
	if name := p.courseName(ctx, courseID); name != "" {
		courseName = name
	}
	if err := renderCourseRank(out, p.ctx.Config.ResourcesPath, p.ctx.Config.GameName, courseName, rows); err != nil {
		return err
	}
	return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
}

func unixMilliString(v int64) string {
	return time.UnixMilli(v).Format("2006-01-02 15:04:05")
}

func gradeName(score int64) string {
	switch {
	case score >= 9900000:
		return "EX+"
	case score >= 9800000:
		return "EX"
	case score >= 9500000:
		return "AA"
	case score >= 9200000:
		return "A"
	case score >= 8900000:
		return "B"
	case score >= 8600000:
		return "C"
	default:
		return "D"
	}
}

func simpleHashRankForUser(ctx context.Context, db database.Queryer, userID int64) int64 {
	var rank int64
	if db.QueryRowContext(ctx, `SELECT COUNT(*)+1 FROM user WHERE score > (SELECT score FROM user WHERE user_id=?)`, userID).Scan(&rank) != nil {
		return 0
	}
	return rank
}

func (p *StatsPlugin) queryCourses(ctx context.Context) ([]CourseInfo, error) {
	rows, err := p.ctx.Arcaea.QueryContext(ctx, `SELECT course_id, course_name FROM course ORDER BY course_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var courses []CourseInfo
	for rows.Next() {
		var c CourseInfo
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		courses = append(courses, c)
	}
	return courses, rows.Err()
}

func (p *StatsPlugin) courseName(ctx context.Context, courseID string) string {
	var name string
	_ = p.ctx.Arcaea.QueryRowContext(ctx, `SELECT course_name FROM course WHERE course_id=?`, courseID).Scan(&name)
	return name
}

func (p *StatsPlugin) queryCourseRank(ctx context.Context, courseID string) ([]CourseRankItem, error) {
	rows, err := p.ctx.Arcaea.QueryContext(ctx, `SELECT u.name, uc.high_score, COALESCE(uc.best_clear_type,1)
FROM user_course uc INNER JOIN user u ON uc.user_id=u.user_id
WHERE uc.course_id=? ORDER BY uc.high_score DESC`, courseID)
	if err != nil {
		rows, err = p.ctx.Arcaea.QueryContext(ctx, `SELECT u.name, c.score, 1
FROM course_score c INNER JOIN user u ON c.user_id=u.user_id
WHERE c.course_id=? ORDER BY c.score DESC`, courseID)
		if err != nil {
			return nil, err
		}
	}
	defer rows.Close()
	var out []CourseRankItem
	rank := 1
	for rows.Next() {
		var item CourseRankItem
		item.Rank = rank
		if err := rows.Scan(&item.UserName, &item.Score, &item.ClearType); err != nil {
			return nil, err
		}
		out = append(out, item)
		rank++
	}
	return out, rows.Err()
}

type recentImageData struct {
	PlayerName, BotName, Potential, HashRank                string
	SongID, SongName, Artist, JacketDesigner, ChartDesigner string
	Difficulty                                              int
	Constant, PlayedAt                                      string
	Health                                                  int
	Score                                                   int64
	BestConstant                                            string
	Rating                                                  float64
	Grade                                                   string
	Pure, ShinyPure, Far, Lost                              int64
	ClearType                                               int
	RatingPTT                                               int64
	CharacterID                                             int
	CharUncapped, CharOverride                              bool
	Illustration                                            string
}

func renderRecent(path, resourcesPath, charPath string, d recentImageData) error {
	rp := filepath.Join(resourcesPath, "recent_play")
	bg := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(rp, "bg.png")))
	if bg == nil {
		bg = image.NewRGBA(image.Rect(0, 0, 1440, 900))
		utils.RenderFillRect(bg, 0, 0, 1440, 900, color.RGBA{28, 32, 54, 255})
	} else {
		bg = utils.ResizeRenderImage(bg, 1440, 900)
	}
	utils.RenderPasteAsset(bg, filepath.Join(rp, "res_banner.png"), image.Rect(0, 100, 1440, 800))
	bgW := bg.Bounds().Dx()
	charName := fmt.Sprintf("%d.png", d.CharacterID)
	if d.CharUncapped && !d.CharOverride {
		charName = fmt.Sprintf("%du.png", d.CharacterID)
	}
	if character := utils.LoadRenderImage(filepath.Join(charPath, "1080", charName)); character != nil {
		cw, ch := character.Bounds().Dx(), character.Bounds().Dy()
		utils.RenderPasteImage(bg, character, image.Rect(bgW/2, 80, bgW/2+int(float64(cw)*.6), 80+int(float64(ch)*.6)))
	}
	if top := utils.LoadRenderImage(filepath.Join(rp, "top_bar_bg.png")); top != nil {
		utils.RenderPasteImage(bg, top, image.Rect(0, 0, top.Bounds().Dx(), top.Bounds().Dy()))
		utils.RenderPasteImage(bg, top, image.Rect(bgW-top.Bounds().Dx(), 0, bgW, top.Bounds().Dy()))
		utils.RenderPasteAsset(bg, filepath.Join(resourcesPath, "b30", "course_banner_12.png"), image.Rect(400, 0, 750, top.Bounds().Dy()-20))
	}
	geo35 := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 35)
	geo20 := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 20)
	geo25 := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 25)
	l2_30 := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 30)
	l2_24 := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 24)
	l2_23 := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 23)
	l2_35 := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 35)
	l2_48 := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 48)
	geo60 := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 60)
	utils.RenderDrawShadowText(bg, geo35, 550-len([]rune(d.PlayerName))/2*15, 3, d.PlayerName, color.White, color.RGBA{0, 0, 0, 128})
	utils.RenderPasteAsset(bg, filepath.Join(rp, "usercell_shape_bg.png"), image.Rect(653, -53, 873, 112))
	if avatar := utils.LoadCharacterIcon(charPath, filepath.Join(resourcesPath, "char", "unknown_icon.png"), d.CharacterID, d.CharUncapped, d.CharOverride); avatar != nil {
		utils.RenderPasteImage(bg, avatar, image.Rect(680, -25, 790, 85))
	}
	utils.RenderPasteAsset(bg, filepath.Join(rp, "hash.png"), image.Rect(785, 3, 800, 18))
	utils.RenderDrawTextTop(bg, geo20, 798, 3, d.HashRank, color.White)
	utils.RenderDrawShadowText(bg, geo35, 20, 4, "© NulCorePivot", color.Black, color.RGBA{255, 255, 255, 180})
	utils.RenderDrawTextTop(bg, geo35, 1000, 3, "Generated by "+d.BotName, color.Black)
	utils.RenderDrawShadowText(bg, l2_30, 25, 120, "最近游玩记录", color.White, color.RGBA{0, 0, 0, 180})
	utils.RenderDrawShadowText(bg, l2_30, 25, 150, "画师："+d.JacketDesigner, color.White, color.RGBA{0, 0, 0, 180})
	utils.RenderDrawShadowText(bg, l2_30, 25, 180, "谱师："+d.ChartDesigner, color.White, color.RGBA{0, 0, 0, 180})
	utils.RenderDrawShadowText(bg, l2_48, (bgW-utils.RenderTextWidth(l2_48, d.SongName))/2, 120, d.SongName, color.White, color.RGBA{0, 0, 0, 180})
	utils.RenderDrawShadowText(bg, l2_30, (bgW-utils.RenderTextWidth(l2_30, d.Artist))/2, 180, d.Artist, color.White, color.RGBA{0, 0, 0, 180})
	rating := filepath.Join(resourcesPath, "rating", utils.RatingBoxFile(float64(d.RatingPTT)/100))
	utils.RenderPasteAsset(bg, rating, image.Rect(750, 30, 815, 95))
	utils.RenderDrawPurpleText(bg, l2_23, 750+(65-utils.RenderTextWidth(l2_23, d.Potential))/2, 45, d.Potential)
	utils.RenderPasteAsset(bg, filepath.Join(rp, "back.png"), image.Rect(0, 826, 240, 902))
	utils.RenderDrawTextTop(bg, l2_24, 84, 840, "返回", color.Black)
	utils.RenderPasteAsset(bg, filepath.Join(rp, "mid_button.png"), image.Rect(625, 826, 865, 902))
	utils.RenderDrawTextTop(bg, l2_24, 721, 840, "分享", color.Black)
	utils.RenderPasteAsset(bg, filepath.Join(rp, "retry.png"), image.Rect(1200, 826, 1440, 902))
	utils.RenderDrawTextTop(bg, l2_24, 1313, 840, "重试", color.Black)
	jacket := d.Illustration
	if jacket == "" {
		jacket = filepath.Join(resourcesPath, "common", "random.jpg")
	}
	if img := utils.LoadRenderImage(jacket); img != nil {
		const frameX, frameY, frameSize = 31, 340, 420
		w, h := img.Bounds().Dx(), img.Bounds().Dy()
		if w > 0 && h > 0 {
			scale := math.Min(float64(frameSize)/float64(w), float64(frameSize)/float64(h))
			drawW := int(math.Round(float64(w) * scale))
			drawH := int(math.Round(float64(h) * scale))
			x := frameX + (frameSize-drawW)/2
			y := frameY + (frameSize-drawH)/2
			utils.RenderPasteImage(bg, img, image.Rect(x, y, x+drawW, y+drawH))
		}
	}
	diff := recentDiffText(d.Difficulty)
	utils.RenderPasteAsset(bg, filepath.Join(rp, "max-recall-"+recentDiffAsset(d.Difficulty)+".png"), image.Rect(10, 250, 310, 334))
	utils.RenderDrawStrokeText(bg, l2_35, 50, 265, d.Constant, color.White, color.Black, 2)
	utils.RenderDrawPurpleText(bg, geo25, 5+(300-utils.RenderTextWidth(geo25, diff))/2, 265, diff)
	utils.RenderDrawPurpleText(bg, geo25, 120, 295, "TIME")
	utils.RenderDrawPurpleText(bg, geo25, 180, 295, d.PlayedAt)
	utils.RenderPasteAsset(bg, filepath.Join(resourcesPath, "clear_type", recentClearFile(d.ClearType)), image.Rect(420, 270, 1020, 335))
	hp := d.Health
	if hp < 0 {
		hp = 0
	}
	if hp > 100 {
		hp = 100
	}
	if hp > 0 {
		utils.RenderPasteAsset(bg, filepath.Join(rp, "hp_bar_clear.png"), image.Rect(450, 350+400-hp*4, 482, 750))
	}
	utils.RenderPasteAsset(bg, filepath.Join(rp, "hp_grid.png"), image.Rect(450, 350, 482, 750))
	utils.RenderPasteAsset(bg, filepath.Join(rp, "res_rating.png"), image.Rect(482, 331, 1007, 630))
	score := recentScoreText(d.Score)
	utils.RenderDrawShadowText(bg, geo60, 640-len(score)*7, 365, score, color.White, color.Black)
	utils.RenderDrawShadowText(bg, geo25, 705-len(d.BestConstant)*3, 458, d.BestConstant+"  >", color.White, color.Black)
	ratingText := fmt.Sprintf("%.5f", d.Rating)
	utils.RenderDrawShadowText(bg, geo25, 785-len(ratingText)*3, 458, ratingText, color.White, color.Black)
	utils.RenderPasteAsset(bg, filepath.Join(resourcesPath, "grade", d.Grade+".png"), image.Rect(620, 460, 800, 640))
	utils.RenderPasteAsset(bg, filepath.Join(rp, "pure-count.png"), image.Rect(600, 625, 780, 658))
	utils.RenderDrawShadowText(bg, geo20, 720, 630, strconv.FormatInt(d.Pure, 10), color.White, color.Black)
	utils.RenderDrawShadowText(bg, geo20, 775, 630, fmt.Sprintf("(+%d)", d.ShinyPure), color.White, color.Black)
	utils.RenderDrawShadowText(bg, geo25, 635, 630, "PURE", color.White, color.Black)
	utils.RenderPasteAsset(bg, filepath.Join(rp, "far-count.png"), image.Rect(600, 665, 780, 698))
	utils.RenderDrawShadowText(bg, geo20, 720, 670, strconv.FormatInt(d.Far, 10), color.White, color.Black)
	utils.RenderDrawShadowText(bg, geo25, 640, 670, "FAR", color.White, color.Black)
	utils.RenderPasteAsset(bg, filepath.Join(rp, "lost-count.png"), image.Rect(600, 705, 780, 738))
	utils.RenderDrawShadowText(bg, geo20, 720, 710, strconv.FormatInt(d.Lost, 10), color.White, color.Black)
	utils.RenderDrawShadowText(bg, geo25, 635, 710, "LOST", color.White, color.Black)
	return utils.RenderWriteJPEG(path, bg, 70)
}
func recentDiffText(diff int) string {
	if diff >= 0 && diff <= 4 {
		return []string{"Past", "Present", "Future", "Beyond", "Eternal"}[diff]
	}
	return "Future"
}

func recentDiffAsset(diff int) string {
	switch diff {
	case 0:
		return "past"
	case 1:
		return "present"
	case 2:
		return "future"
	case 3:
		return "beyond"
	case 4:
		return "eternal"
	default:
		return "future"
	}
}
func recentClearFile(v int) string {
	switch v {
	case 0:
		return "track_lost.png"
	case 2:
		return "track_full_recall.png"
	case 3:
		return "track_pure_memory.png"
	case 4:
		return "track_hard.png"
	default:
		return "track_normal.png"
	}
}
func recentScoreText(score int64) string {
	s := strconv.FormatInt(score, 10)
	if len(s) <= 8 {
		s += strings.Repeat("0", 8-len(s))
	}
	var p []string
	for len(s) > 3 {
		p = append([]string{s[len(s)-3:]}, p...)
		s = s[:len(s)-3]
	}
	return strings.Join(append([]string{s}, p...), "'")
}

func renderRank(path, resourcesPath, title, valueLabel string, data []RankItem) error {
	const width = 800
	const headerHeight = 160
	const itemHeight = 90
	const margin = 30
	const footerHeight = 50
	const shadowOffset = 5
	const shadowRadius = 5
	const bgRadius = 24
	const titleBgHeight = 140
	totalHeight := headerHeight + len(data)*itemHeight + margin*2 + footerHeight + shadowOffset*2
	img := image.NewRGBA(image.Rect(0, 0, width, totalHeight))
	utils.RenderFillRect(img, 0, 0, width, totalHeight, color.White)

	for i := 0; i < shadowRadius; i++ {
		r := bgRadius + shadowRadius - i
		utils.RenderDrawRoundedRect(img, margin+shadowOffset-i, margin+shadowOffset-i, width-margin-shadowOffset+i, totalHeight-margin-footerHeight-shadowOffset+i, r, color.RGBA{220, 220, 220, 100})
	}
	mainRect := image.Rect(margin+shadowOffset, margin+shadowOffset, width-margin-shadowOffset, totalHeight-margin-footerHeight-shadowOffset)
	utils.RenderDrawRoundedRect(img, mainRect.Min.X, mainRect.Min.Y, mainRect.Max.X, mainRect.Max.Y, bgRadius, color.White)

	titleFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 50)
	rankFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 40)
	nameFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 34)
	valueFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 32)
	footerFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 18)

	utils.RenderDrawRoundedRect(img, mainRect.Min.X, mainRect.Min.Y, mainRect.Max.X, mainRect.Min.Y+titleBgHeight, bgRadius, color.White)
	utils.RenderDrawTextTop(img, titleFace, (width-utils.RenderTextWidth(titleFace, title))/2, margin+shadowOffset+30, title, color.RGBA{30, 30, 30, 255})
	subtitle := fmt.Sprintf("Top %d · %s", len(data), valueLabel)
	utils.RenderDrawTextTop(img, nameFace, (width-utils.RenderTextWidth(nameFace, subtitle))/2, margin+shadowOffset+90, subtitle, color.RGBA{80, 80, 80, 255})

	maxValue := 1.0
	for _, item := range data {
		if item.Value > maxValue {
			maxValue = item.Value
		}
	}
	for idx, item := range data {
		y := margin + shadowOffset + headerHeight + idx*itemHeight
		bg := color.RGBA{255, 255, 255, 255}
		if idx%2 == 1 {
			bg = color.RGBA{250, 250, 250, 255}
		}
		utils.RenderFillRect(img, margin+shadowOffset, y, width-margin-shadowOffset, y+itemHeight, bg)
		rank := idx + 1
		rankX := margin + shadowOffset + 50
		rankColor := color.RGBA{70, 70, 70, 255}
		switch rank {
		case 1:
			utils.RenderDrawEllipse(img, rankX-25, y+itemHeight/2-25, rankX+25, y+itemHeight/2+25, color.RGBA{255, 240, 200, 255})
			utils.RenderDrawEllipse(img, rankX-23, y+itemHeight/2-23, rankX+23, y+itemHeight/2+23, color.RGBA{255, 184, 28, 255})
			rankColor = color.RGBA{255, 255, 255, 255}
		case 2:
			utils.RenderDrawEllipse(img, rankX-25, y+itemHeight/2-25, rankX+25, y+itemHeight/2+25, color.RGBA{240, 240, 240, 255})
			utils.RenderDrawEllipse(img, rankX-23, y+itemHeight/2-23, rankX+23, y+itemHeight/2+23, color.RGBA{169, 169, 169, 255})
			rankColor = color.RGBA{255, 255, 255, 255}
		case 3:
			utils.RenderDrawEllipse(img, rankX-25, y+itemHeight/2-25, rankX+25, y+itemHeight/2+25, color.RGBA{250, 230, 200, 255})
			utils.RenderDrawEllipse(img, rankX-23, y+itemHeight/2-23, rankX+23, y+itemHeight/2+23, color.RGBA{205, 127, 50, 255})
			rankColor = color.RGBA{255, 255, 255, 255}
		}
		utils.RenderDrawTextCenter(img, rankFace, rankX, y+itemHeight/2, strconv.Itoa(rank), rankColor)
		nameX := margin + shadowOffset + 150
		maxNameWidth := width - 2*(margin+shadowOffset) - 320
		displayName := utils.RenderEllipsizeFace(nameFace, item.Name, maxNameWidth)
		utils.RenderDrawTextLeftMiddle(img, nameFace, nameX, y+itemHeight/2, displayName, color.RGBA{50, 50, 50, 255})
		progressWidth := width - margin - shadowOffset - 200 - nameX
		progressY := y + itemHeight/2 + 20
		utils.RenderDrawRoundedRect(img, nameX, progressY, nameX+progressWidth, progressY+12, 6, color.RGBA{230, 230, 230, 255})
		fillW := int(float64(progressWidth) * item.Value / maxValue)
		utils.RenderDrawRoundedRect(img, nameX, progressY, nameX+fillW, progressY+12, 6, color.RGBA{100, 180, 255, 255})
		valueText := fmt.Sprintf("%.2f", item.Value)
		utils.RenderDrawTextCenter(img, valueFace, width-margin-shadowOffset-80, y+itemHeight/2, valueText, color.RGBA{50, 50, 50, 255})
		if idx < len(data)-1 {
			utils.RenderDrawLine(img, margin+shadowOffset+40, y+itemHeight, width-margin-shadowOffset-40, y+itemHeight, color.RGBA{240, 240, 240, 255})
		}
	}
	footerY := totalHeight - footerHeight - shadowOffset
	utils.RenderDrawLine(img, margin+shadowOffset+40, footerY-20, width-margin-shadowOffset-40, footerY-20, color.RGBA{240, 240, 240, 255})
	copyText := "Copyright © 2025 NulCorePivot"
	utils.RenderDrawTextTop(img, footerFace, (width-utils.RenderTextWidth(footerFace, copyText))/2, footerY+10, copyText, color.RGBA{150, 150, 150, 255})
	return utils.RenderWriteJPEG(path, img, 85)
}

func renderRating(path, resourcesPath, bundleSongsPath, gameName string, records []RatingRecord, ratingLabel string, isRange bool) error {
	const width = 950
	const itemSize = 200
	const titleHeight = 40
	const shadowSize = 10
	const itemHeight = itemSize + titleHeight + shadowSize
	const itemsPerRow = 4
	const headerHeight = 80
	const margin = 20
	const dividerHeight = 60

	grouped := map[int][]RatingRecord{}
	var keys []int
	if isRange {
		seen := map[int]bool{}
		for _, r := range records {
			if !seen[r.Rating] {

				seen[r.Rating] = true
				keys = append(keys, r.Rating)
			}
			grouped[r.Rating] = append(grouped[r.Rating], r)
		}
		sort.Ints(keys)
	} else {
		keys = []int{0}
		grouped[0] = records
	}
	height := headerHeight + margin
	if isRange {
		for idx, key := range keys {
			rows := int(math.Ceil(float64(len(grouped[key])) / itemsPerRow))
			height += rows * (itemHeight + margin)
			if idx < len(keys)-1 {
				height += dividerHeight + margin
			}
		}
		height += margin + 80
	} else {
		rows := int(math.Ceil(float64(len(records)) / itemsPerRow))
		height = headerHeight + (itemHeight+margin)*rows + margin + 30
	}
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	utils.RenderFillRect(img, 0, 0, width, height, color.White)
	for y := 0; y < 10; y++ {
		r := uint8(30 + 100*y*8/headerHeight)
		g := uint8(60 + 40*y*8/headerHeight)
		b := uint8(100 + 100*y*8/headerHeight)
		utils.RenderDrawLine(img, 0, y, width, y, color.RGBA{r, g, b, 255})
	}

	titleFace := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 44)
	rankFace := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 36)
	dividerFace := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 32)
	nameFace := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 32)
	footerFace := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 16)
	utils.RenderDrawTextTop(img, titleFace, margin, (headerHeight-20)/2, gameName+" Constant Sheet", color.Black)
	utils.RenderDrawTextTop(img, rankFace, width-utils.RenderTextWidth(rankFace, ratingLabel)-margin, (headerHeight-20)/2, ratingLabel, color.Black)

	yPos := headerHeight + margin
	for idx, key := range keys {
		items := grouped[key]
		if len(items) == 0 {
			continue
		}
		if isRange {
			segText := fmt.Sprintf(" - %.1f - ", float64(key)/10)
			segW := utils.RenderTextWidth(dividerFace, segText)
			segX := (width - segW) / 2
			utils.RenderFillRect(img, segX-10, yPos, segX+segW+10, yPos+50, color.RGBA{230, 230, 230, 255})
			utils.RenderDrawTextTop(img, dividerFace, segX, yPos+5, segText, color.RGBA{50, 50, 50, 255})
			yPos += dividerHeight
		}
		for itemIdx, rec := range items {
			row := itemIdx / itemsPerRow
			col := itemIdx % itemsPerRow
			x := margin + col*(itemSize+shadowSize+margin)
			itemY := yPos + row*(itemHeight+margin)
			jacket := utils.LoadRenderImage(utils.FindJacket(bundleSongsPath, rec.SongID))
			if jacket == nil {
				jacket = utils.LoadRenderImage(filepath.Join(resourcesPath, "common", "random.jpg"))
			}
			if jacket != nil {
				xdraw.ApproxBiLinear.Scale(img, image.Rect(x, itemY, x+itemSize, itemY+itemSize), jacket, jacket.Bounds(), draw.Over, nil)
			}
			diffColor := utils.RenderDifficultyColor(rec.Difficulty)
			utils.RenderFillRect(img, x+30, itemY+itemSize+5, x+itemSize+shadowSize, itemY+itemSize+shadowSize+5, diffColor)
			utils.RenderFillRect(img, x+itemSize+5, itemY+30, x+itemSize+shadowSize+5, itemY+itemSize+15, diffColor)
			name := utils.RenderTruncateRatingName(rec.Name)
			name = utils.RenderEllipsizeFace(nameFace, name, itemSize+shadowSize-20)
			utils.RenderDrawTextTop(img, nameFace, x+28, itemY+itemSize+shadowSize+(titleHeight-20)/2, name, color.Black)
		}
		rows := int(math.Ceil(float64(len(items)) / itemsPerRow))
		yPos += rows * (itemHeight + margin)
		if isRange && idx < len(keys)-1 {
			yPos += margin
		}
	}
	footer := "Copyright © 2025 NulCorePivot. All rights reserved. - " + time.Now().String()
	utils.RenderDrawTextTop(img, footerFace, margin, height-25, footer, color.RGBA{100, 100, 100, 255})
	return utils.RenderWriteJPEG(path, img, 85)
}

func renderCourseRank(path, resourcesPath, gameName, courseName string, rows []CourseRankItem) error {
	const imageWidth = 800
	section := map[string]int{"title": 110, "header": 60, "row": 45, "footer": 35}
	height := section["title"] + section["header"] + len(rows)*section["row"] + section["footer"]
	if height < 300 {
		height = 300
	}
	img := image.NewRGBA(image.Rect(0, 0, imageWidth, height))
	utils.RenderDrawRoundedRect(img, 0, 0, imageWidth, height, 10, color.White)
	titleFace := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 32)
	headerFace := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 24)
	contentFace := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 16)
	footerFace := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 16)
	mainTitle := gameName + " Course Rank"
	utils.RenderDrawTextTop(img, titleFace, (imageWidth-utils.RenderTextWidth(titleFace, mainTitle))/2, section["title"]/4, mainTitle, color.RGBA{51, 51, 51, 255})
	utils.RenderDrawTextTop(img, contentFace, (imageWidth-utils.RenderTextWidth(contentFace, courseName))/2, section["title"]/4+utils.RenderTextHeight(titleFace)+8, courseName, color.RGBA{51, 51, 51, 255})
	headerY := section["title"]
	utils.RenderDrawRoundedRect(img, 0, headerY, imageWidth, headerY+section["header"], 8, color.RGBA{245, 247, 250, 255})
	headers := []struct {
		text  string
		width int
		x     int
	}{{"Rank", 80, 0}, {"User Name", 250, 80}, {"Highest Score", 220, 330}, {"Clear Type", 250, 550}}
	for _, h := range headers {
		utils.RenderDrawTextTop(img, headerFace, h.x+(h.width-utils.RenderTextWidth(headerFace, h.text))/2, headerY+(section["header"]-utils.RenderTextHeight(headerFace))/2, h.text, color.RGBA{68, 68, 68, 255})
	}
	y := section["title"] + section["header"]
	for idx, row := range rows {
		bg := color.RGBA{250, 251, 252, 255}
		if idx%2 == 1 {
			bg = color.RGBA{255, 255, 255, 255}
		}
		utils.RenderDrawRoundedRect(img, 0, y, imageWidth, y+section["row"], 8, bg)
		rankText := strconv.Itoa(row.Rank)
		if row.Rank <= 3 {
			utils.RenderDrawTinyCup(img, 30, y+3, []color.RGBA{{255, 215, 0, 255}, {192, 192, 192, 255}, {205, 127, 50, 255}}[row.Rank-1])
		} else {
			utils.RenderDrawTextTop(img, contentFace, 40-utils.RenderTextWidth(contentFace, rankText)/2, y+12, rankText, color.RGBA{51, 51, 51, 255})
		}
		utils.RenderDrawTextTop(img, contentFace, 80+(250-utils.RenderTextWidth(contentFace, row.UserName))/2, y+12, row.UserName, color.RGBA{51, 51, 51, 255})
		score := fmt.Sprintf("%d", row.Score)
		utils.RenderDrawTextTop(img, contentFace, 330+(220-utils.RenderTextWidth(contentFace, score))/2, y+12, score, color.RGBA{45, 125, 255, 255})
		clearPath := filepath.Join(resourcesPath, "clear_type", utils.CourseClearTypeFile(row.ClearType))
		utils.RenderPasteAssetFitWidth(img, clearPath, 550+(250-120)/2, y+(section["row"]-30)/2, 120)
		y += section["row"]
	}
	footer := "Copyright 2025 © NulCorePivot. Generated at " + time.Now().Format("2006-01-02 15:04:05")
	utils.RenderDrawTextTop(img, footerFace, (imageWidth-utils.RenderTextWidth(footerFace, footer))/2, height-section["footer"]+8, footer, color.RGBA{134, 144, 156, 255})
	return utils.RenderWriteJPEG(path, img, 85)
}

func renderCourseMapping(path, resourcesPath, gameName string, courses []CourseInfo) error {
	const imageWidth = 800
	section := map[string]int{"title": 80, "header": 60, "row": 45, "footer": 35}
	height := section["title"] + section["header"] + len(courses)*section["row"] + section["footer"]
	if height < 300 {
		height = 300
	}
	img := image.NewRGBA(image.Rect(0, 0, imageWidth, height))
	utils.RenderDrawRoundedRect(img, 0, 0, imageWidth, height, 10, color.White)
	titleFace := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 32)
	headerFace := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 24)
	contentFace := utils.LoadRenderFont(resourcesPath, "L2-Regular.ttf", 16)
	footerFace := utils.LoadRenderFont(resourcesPath, "GeosansLight.ttf", 16)
	title := gameName + " Course ID"
	utils.RenderDrawTextTop(img, titleFace, (imageWidth-utils.RenderTextWidth(titleFace, title))/2, section["title"]/3, title, color.RGBA{51, 51, 51, 255})
	headerY := section["title"]
	utils.RenderDrawRoundedRect(img, 0, headerY, imageWidth, headerY+section["header"], 8, color.RGBA{245, 247, 250, 255})
	utils.RenderDrawTextTop(img, headerFace, (200-utils.RenderTextWidth(headerFace, "Course ID"))/2, headerY+(section["header"]-utils.RenderTextHeight(headerFace))/2, "Course ID", color.RGBA{68, 68, 68, 255})
	utils.RenderDrawTextTop(img, headerFace, 200+(600-utils.RenderTextWidth(headerFace, "Course Name"))/2, headerY+(section["header"]-utils.RenderTextHeight(headerFace))/2, "Course Name", color.RGBA{68, 68, 68, 255})
	y := section["title"] + section["header"]
	for idx, c := range courses {
		bg := color.RGBA{250, 251, 252, 255}
		if idx%2 == 1 {
			bg = color.RGBA{255, 255, 255, 255}
		}
		utils.RenderDrawRoundedRect(img, 0, y, imageWidth, y+section["row"], 8, bg)
		utils.RenderDrawTextTop(img, contentFace, (200-utils.RenderTextWidth(contentFace, c.ID))/2, y+12, c.ID, color.RGBA{51, 51, 51, 255})
		utils.RenderDrawTextTop(img, contentFace, 200+(600-utils.RenderTextWidth(contentFace, c.Name))/2, y+12, c.Name, color.RGBA{51, 51, 51, 255})
		y += section["row"]
	}
	footer := "Copyright 2025 © NulCorePivot. Generated at " + time.Now().Format("2006-01-02 15:04:05")
	utils.RenderDrawTextTop(img, footerFace, (imageWidth-utils.RenderTextWidth(footerFace, footer))/2, height-section["footer"]+8, footer, color.RGBA{134, 144, 156, 255})
	return utils.RenderWriteJPEG(path, img, 85)
}

func init() {
	utils.Add("stats", func(ctx utils.Context) *StatsPlugin {
		p := &StatsPlugin{ctx: ctx}
		p.Init("stats")
		p.songs, p.byID = utils.LoadSongs(ctx.Config.SonglistPath)
		p.Command("#排行", nil, p.rank, "查看玩家PTT排行")
		p.Command("#排行2", nil, p.rankHash, "查看玩家Hash排行")
		p.Command("#Hash", nil, p.hashRank, "查看世界Hash排名")
		p.Command("#PTT", nil, p.pttRank, "查看世界PTT排名")
		p.Command("#定数查询", nil, p.rating, "查询歌曲定数")
		p.Command("#最近游玩", nil, p.recent, "生成结算页面")
		p.Command("#课题排行", nil, p.course, "查看课题排行榜")
		return p
	})
}
