package plugins

import (
	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
	"arcaeabot/internal/utils"
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type AliasPlugin struct {
	utils.Base
	ctx   utils.Context
	songs []utils.Song
	byID  map[string]utils.Song
}

func (p *AliasPlugin) usage(ctx context.Context, ev napcat.Event, _ string) error {
	return onebot.Reply(ctx, p.ctx.Client, ev, strings.Join([]string{
		"歌曲别名系统：",
		"#别名 信息 [歌曲ID/歌曲别名] - 查看歌曲已有别名",
		"#别名 添加 [歌曲ID] [新别名] - 添加歌曲别名",
		"#别名 删除 [歌曲ID] [旧别名] - 删除歌曲别名(仅群管理员)",
		"#曲目 等歌曲查询指令会自动读取这里维护的别名。",
	}, "\n"))
}

func (p *AliasPlugin) info(ctx context.Context, ev napcat.Event, args string) error {
	query := strings.TrimSpace(args)
	if query == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "使用方法：\n#别名 信息 [歌曲ID/歌曲别名]")
	}
	ids := utils.FindSongsByAlias(ctx, p.ctx.Store, p.songs, p.byID, query)
	if len(ids) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "未找到匹配的歌曲。")
	}
	if len(ids) > 1 {
		if len(ids) > 10 {
			ids = ids[:10]
		}
		return onebot.Reply(ctx, p.ctx.Client, ev, "找到多个匹配结果：\n"+strings.Join(ids, "\n"))
	}
	kv, err := p.ctx.Store.KV("alias_data")
	if err != nil {
		return err
	}
	var aliases []utils.AliasEntry
	_, _ = kv.Get(ctx, ids[0], &aliases)
	lines := []string{"歌曲ID: " + ids[0], "别名:"}
	if len(aliases) == 0 {
		lines = append(lines, "暂无别名")
	} else {
		for _, item := range aliases {
			lines = append(lines, "- "+item.Alias+" ("+item.Source+")")
		}
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, strings.Join(lines, "\n"))
}

func (p *AliasPlugin) add(ctx context.Context, ev napcat.Event, args string) error {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "参数格式错误！正确格式：#别名 添加 [歌曲ID] [新别名]")
	}
	songID, newAlias := parts[0], parts[1]
	if _, ok := p.byID[songID]; !ok && !utils.ExistsRow(ctx, p.ctx.Arcaea, "SELECT 1 FROM chart WHERE song_id=?", songID) {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("歌曲ID '%s' 不存在！", songID))
	}
	kv, err := p.ctx.Store.KV("alias_data")
	if err != nil {
		return err
	}
	var aliases []utils.AliasEntry
	_, _ = kv.Get(ctx, songID, &aliases)
	for _, item := range aliases {
		if item.Alias == newAlias {
			return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("歌曲 '%s' 已存在别名 '%s'！", songID, newAlias))
		}
	}
	aliases = append(aliases, utils.AliasEntry{Alias: newAlias, Source: fmt.Sprintf("%d", onebot.UserID(ev))})
	if err := kv.Set(ctx, songID, aliases); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("成功为歌曲 '%s' 添加别名 '%s'！", songID, newAlias))
}

func (p *AliasPlugin) del(ctx context.Context, ev napcat.Event, args string) error {
	if !onebot.IsGroupMessage(ev) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "删除别名功能仅可在群聊中使用！")
	}
	if !utils.Admin(ctx, p.ctx.Client, ev) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "权限不足！删除别名功能仅限管理员使用。")
	}
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "参数格式错误！正确格式：#别名 删除 [歌曲ID] [旧别名]")
	}
	kv, err := p.ctx.Store.KV("alias_data")
	if err != nil {
		return err
	}
	var aliases []utils.AliasEntry
	_, _ = kv.Get(ctx, parts[0], &aliases)
	next := aliases[:0]
	removed := false
	for _, item := range aliases {
		if item.Alias == parts[1] {
			removed = true
			continue
		}
		next = append(next, item)
	}
	if !removed {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("歌曲 '%s' 不存在别名 '%s'！", parts[0], parts[1]))
	}
	if err := kv.Set(ctx, parts[0], next); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("成功删除歌曲 '%s' 的别名 '%s'！", parts[0], parts[1]))
}

type SongPlugin struct {
	utils.Base
	ctx   utils.Context
	songs []utils.Song
	byID  map[string]utils.Song
}

func (p *SongPlugin) info(ctx context.Context, ev napcat.Event, args string) error {
	query := strings.TrimSpace(args)
	if query == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "请输入要查询的曲目，例如：#曲目 song_id")
	}
	ids := utils.FindSongsByAlias(ctx, p.ctx.Store, p.songs, p.byID, query)
	if len(ids) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "未查询到相应的档案信息 - "+query)
	}
	if len(ids) > 1 {
		lines := []string{fmt.Sprintf("通过别名查询到 %d 个匹配结果：", len(ids))}
		for i, id := range ids {
			if i >= 10 {
				break
			}
			title := id
			if song, ok := p.byID[id]; ok {
				title = utils.SongTitle(song)
			}
			lines = append(lines, fmt.Sprintf("%s - %s", id, title))
		}
		return onebot.Reply(ctx, p.ctx.Client, ev, strings.Join(lines, "\n"))
	}
	song, ok := p.byID[ids[0]]
	if !ok {
		return onebot.Reply(ctx, p.ctx.Client, ev, "未查询到相应的档案信息 - "+query)
	}
	text := p.format(ctx, song)
	if jacket := utils.FindJacket(p.ctx.Config.BundleSongsPath, song.ID); jacket != "" {
		if file, err := utils.Base64File(jacket); err == nil {
			return onebot.Reply(ctx, p.ctx.Client, ev, []onebot.Segment{onebot.Image(file), onebot.Text(text)})
		}
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, text)
}

func (p *SongPlugin) format(ctx context.Context, song utils.Song) string {
	ratings := map[string]sql.NullInt64{}
	row := p.ctx.Arcaea.QueryRowContext(ctx, "SELECT rating_pst, rating_prs, rating_ftr, rating_byn, rating_etr FROM chart WHERE song_id=?", song.ID)
	var pst, prs, ftr, byn, etr sql.NullInt64
	_ = row.Scan(&pst, &prs, &ftr, &byn, &etr)
	ratings["PST"], ratings["PRS"], ratings["FTR"], ratings["BYD"], ratings["ETR"] = pst, prs, ftr, byn, etr

	var levels []string
	var designers []string
	for _, diff := range song.Difficulties {
		name := utils.DifficultyName(diff.RatingClass)
		level := fmt.Sprintf("%s %d", name, diff.Rating)
		if diff.RatingPlus {
			level += "+"
		}
		levels = append(levels, level)
		if diff.ChartDesigner != "" {
			designers = append(designers, fmt.Sprintf("%s (%s)", name, diff.ChartDesigner))
		}
	}
	var ratingParts []string
	for _, name := range []string{"PST", "PRS", "FTR", "BYD", "ETR"} {
		if ratings[name].Valid && ratings[name].Int64 >= 0 {
			ratingParts = append(ratingParts, fmt.Sprintf("%s %.1f", name, float64(ratings[name].Int64)/10))
		}
	}
	side := map[int]string{0: "光侧", 1: "暗侧", 2: "消光侧"}[song.Side]
	if side == "" {
		side = "-"
	}
	return fmt.Sprintf("曲目：%s\n曲师：%s\nBPM：%v\n更新版本：%s（%s）\n-\n标级：%s\n定数：%s\n谱面：%s",
		utils.SongTitle(song), song.Artist, song.BPMBase, emptyDash(song.Version), side,
		emptyDash(strings.Join(levels, " / ")), emptyDash(strings.Join(ratingParts, " / ")), emptyDash(strings.Join(designers, " / ")))
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func init() {
	utils.Add("alias", func(ctx utils.Context) *AliasPlugin {
		p := &AliasPlugin{ctx: ctx}
		p.Init("alias")
		p.songs, p.byID = utils.LoadSongs(ctx.Config.SonglistPath)
		p.Command("#别名 信息", nil, p.info, "查询别名")
		p.Command("#别名 添加", nil, p.add, "添加别名")
		p.Command("#别名 删除", nil, p.del, "删除别名")
		p.Command("#别名", nil, p.usage, "查看别名指令")
		return p
	})
	utils.Add("song", func(ctx utils.Context) *SongPlugin {
		p := &SongPlugin{ctx: ctx}
		p.Init("song")
		p.songs, p.byID = utils.LoadSongs(ctx.Config.SonglistPath)
		p.Command("#曲目", nil, p.info, "查询歌曲信息")
		return p
	}, "song_info")
}
