package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"database/sql"
	"fmt"
	"image"
	"image/color"
	"path/filepath"
	"sort"
	"strings"

	"arcaeabot/internal/napcat"
	"golang.org/x/image/font"
)

type HelpPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *HelpPlugin) help(ctx context.Context, ev napcat.Event, _ string) error {
	c := p.ctx.Config
	groups := []helpCommandGroup{
		{title: "「 指令系统 / 账号与日常 」"},
		{title: "「 指令系统 / 成绩与歌曲 」"},
		{title: "「 指令系统 / 群组与反馈 」"},
		{title: "「 指令系统 / 娱乐与互动 」"},
	}
	category := map[string]int{
		"acct": 0, "check": 0, "eco": 0, "frag": 0, "snatch": 0, "tx": 0,
		"alias": 1, "acv": 1, "b30": 1, "board": 1, "ptt": 1, "song": 1, "stats": 1,
		"admin": 2, "files": 2, "notice": 2, "rep": 2, "rpt": 2,
		"ai": 3, "chat": 3, "fun": 3, "guess": 3, "guy": 3, "help": 3, "rand": 3, "sticker": 3, "tarot": 3,
	}
	if p.ctx.Registry != nil {
		plugins := p.ctx.Registry.Plugins()
		sort.SliceStable(plugins, func(i, j int) bool {
			return strings.ToLower(plugins[i].Name()) < strings.ToLower(plugins[j].Name())
		})
		for _, plugin := range plugins {
			pluginCommands := plugin.Commands()
			hasDescription := false
			for _, command := range pluginCommands {
				if strings.TrimSpace(command.Description) != "" {
					hasDescription = true
					break
				}
			}
			if !hasDescription {
				continue
			}
			group := category[plugin.Name()]
			for _, command := range pluginCommands {
				description := strings.TrimSpace(command.Description)
				if description == "" {
					continue
				}
				name := command.Name
				if len(command.Aliases) > 0 {
					name += "（别名：" + strings.Join(command.Aliases, "、") + "）"
				}
				groups[group].items = append(groups[group].items, name+" - "+description)
			}
		}
	}
	theory := p.theoryRatingText(ctx)
	others := append([]string{"「 注意事项 / Tips 」"}, c.HelpTips...)
	for i, tip := range others {
		others[i] = strings.ReplaceAll(tip, "{theory}", theory)
	}
	out := filepath.Join(c.TmpPath, "help", "help.jpg")
	if err := renderHelp(out, c.ResourcesPath, c.BotName, groups, others); err != nil {
		return err
	}
	return utils.ReplyImg(ctx, p.ctx.Client, ev, out)
}

// theoryRatingText returns the theoretical PTT calculated from the server's
// chart constants: the best 30 charts plus the best 10 charts again as R10.
func (p *HelpPlugin) theoryRatingText(ctx context.Context) string {
	rows, err := p.ctx.Arcaea.QueryContext(ctx, `
SELECT rating FROM (
SELECT rating_pst AS rating FROM chart
UNION ALL SELECT rating_prs AS rating FROM chart
UNION ALL SELECT rating_ftr AS rating FROM chart
UNION ALL SELECT rating_byn AS rating FROM chart
UNION ALL SELECT rating_etr AS rating FROM chart
) t WHERE rating IS NOT NULL ORDER BY rating DESC LIMIT 30`)
	if err != nil {
		return "???"
	}
	defer rows.Close()

	values := make([]float64, 0, 30)
	for rows.Next() {
		var rating sql.NullInt64
		if err := rows.Scan(&rating); err != nil {
			return "???"
		}
		if rating.Valid {
			values = append(values, float64(rating.Int64)*0.1+2)
		}
	}
	if err := rows.Err(); err != nil || len(values) == 0 {
		return "???"
	}

	total := 0.0
	for _, value := range values {
		total += value
	}
	for i := 0; i < len(values) && i < 10; i++ {
		total += values[i]
	}
	return fmt.Sprintf("%.2f", total/40)
}

type helpTextSpan struct {
	text  string
	style int
}

type helpTextRune struct {
	value rune
	style int
}

type helpCommandGroup struct {
	title string
	items []string
}

func helpCommandSpans(item string) []helpTextSpan {
	if !strings.HasPrefix(item, "#") {
		return []helpTextSpan{{text: item}}
	}
	command, description, _ := strings.Cut(item, " - ")
	aliasAt := strings.Index(command, "（别名：")
	spans := make([]helpTextSpan, 0, 3)
	if aliasAt < 0 {
		spans = append(spans, helpTextSpan{text: command, style: 1})
	} else {
		spans = append(spans,
			helpTextSpan{text: command[:aliasAt], style: 1},
			helpTextSpan{text: command[aliasAt:], style: 2},
		)
	}
	if description != "" {
		spans = append(spans, helpTextSpan{text: " - " + description})
	}
	return spans
}

func drawHelpCommandItem(img *image.RGBA, face font.Face, x, y, maxWidth, lineHeight int, item string) int {
	var runes []helpTextRune
	for _, span := range helpCommandSpans(item) {
		for _, value := range span.text {
			runes = append(runes, helpTextRune{value: value, style: span.style})
		}
	}
	lines := make([][]helpTextRune, 0, 1)
	current := make([]helpTextRune, 0, len(runes))
	for _, item := range runes {
		candidate := append(current, item)
		var text strings.Builder
		for _, value := range candidate {
			text.WriteRune(value.value)
		}
		if len(current) > 0 && utils.RenderTextWidth(face, text.String()) > maxWidth {
			lines = append(lines, current)
			current = []helpTextRune{item}
		} else {
			current = candidate
		}
	}
	if len(current) > 0 {
		lines = append(lines, current)
	}
	colors := []color.Color{
		color.White,
		color.RGBA{82, 162, 255, 255},
		color.RGBA{255, 92, 108, 255},
	}
	for lineIndex, line := range lines {
		cursor := x
		for start := 0; start < len(line); {
			end := start + 1
			for end < len(line) && line[end].style == line[start].style {
				end++
			}
			var text strings.Builder
			for _, value := range line[start:end] {
				text.WriteRune(value.value)
			}
			part := text.String()
			utils.RenderDrawTextTop(img, face, cursor, y+lineIndex*lineHeight, part, colors[line[start].style])
			cursor += utils.RenderTextWidth(face, part)
			start = end
		}
	}
	return max(1, len(lines))
}

func renderHelp(path, resourcesPath, botName string, groups []helpCommandGroup, others []string) error {
	source := utils.RenderRGBA(utils.LoadRenderImage(filepath.Join(resourcesPath, "help", "help_bg.jpg")))
	if source == nil {
		source = image.NewRGBA(image.Rect(0, 0, 900, 700))
		utils.RenderFillRect(source, 0, 0, 900, 700, color.RGBA{70, 80, 110, 255})
	}
	contentFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 32)
	titleFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 54)
	copyFace := utils.LoadRenderFont(resourcesPath, "DingTalk JinBuTi.ttf", 27)
	lineHeight := 45
	margin := 30
	baseWidth := source.Bounds().Dx() * 2
	maxColumnWidth := baseWidth/4 - margin*2
	if maxColumnWidth < 200 {
		maxColumnWidth = 200
	}
	groupLines := func(group helpCommandGroup) int {
		lines := 1
		for _, item := range group.items {
			lines += max(1, len(utils.RenderWrapRunes(contentFace, item, maxColumnWidth)))
		}
		return lines
	}
	totalLines := 0
	for _, group := range groups {
		totalLines += groupLines(group)
	}
	commandColumns := len(groups)
	if commandColumns == 0 {
		commandColumns = 1
	}
	columns := make([][]helpCommandGroup, commandColumns)
	columnLines := make([]int, commandColumns)
	for i, group := range groups {
		if i >= commandColumns {
			break
		}
		columns[i] = append(columns[i], group)
		columnLines[i] = groupLines(group)
	}
	tips := make([]string, len(others))
	copy(tips, others)
	tipsHeight := 1
	for _, item := range tips {
		tipsHeight += max(1, len(utils.RenderWrapRunes(contentFace, item, maxColumnWidth)))
	}
	maxLines := tipsHeight
	for _, lines := range columnLines {
		if lines+1 > maxLines {
			maxLines = lines + 1
		}
	}
	width := baseWidth
	height := max(source.Bounds().Dy()*2, 100+maxLines*lineHeight+70)
	canvas := utils.ResizeRenderImage(source, width, height)
	title := botName + " 帮助菜单"
	utils.RenderDrawTextTop(canvas, titleFace, (width-utils.RenderTextWidth(titleFace, title))/2, 25, title, color.White)
	columnWidth := width / (commandColumns + 1)
	for i := 1; i <= commandColumns; i++ {
		utils.RenderDrawLine(canvas, i*columnWidth, 100, i*columnWidth, height-75, color.RGBA{255, 255, 255, 128})
	}
	for i, groups := range columns {
		x := i*columnWidth + margin
		y := 115
		for _, group := range groups {
			utils.RenderDrawTextTop(canvas, contentFace, x, y, group.title, color.White)
			y += lineHeight
			for _, item := range group.items {
				y += drawHelpCommandItem(canvas, contentFace, x, y, columnWidth-margin*2, lineHeight, item) * lineHeight
			}
		}
	}
	tipsX := commandColumns*columnWidth + margin
	y := 115
	for _, item := range tips {
		for _, line := range utils.RenderWrapRunes(contentFace, item, columnWidth-margin*2) {
			utils.RenderDrawTextTop(canvas, contentFace, tipsX, y, line, color.White)
			y += lineHeight
		}
	}
	copyright := "Copyright 2026 © NulCorePivot. All rights reserved."
	footerY := height - 55
	utils.RenderDrawTextTop(canvas, copyFace, (width-utils.RenderTextWidth(copyFace, copyright))/2, footerY, copyright, color.White)
	return utils.RenderWriteJPEG(path, canvas, 70)
}

func init() {
	utils.Add("help", func(ctx utils.Context) *HelpPlugin {
		p := &HelpPlugin{ctx: ctx}
		p.Init("help")
		p.Command("#帮助", nil, p.help, "生成帮助菜单")
		return p
	})
}
