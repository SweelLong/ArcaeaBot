package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type RptPlugin struct {
	utils.Base
	ctx utils.Context
}

type ReportSegment struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

type ReportEntry struct {
	UserID    string          `json:"user_id"`
	Segments  []ReportSegment `json:"segments"`
	Timestamp string          `json:"timestamp"`
}

type ReportData map[string][]ReportEntry

func (p *RptPlugin) bug(ctx context.Context, ev napcat.Event, args string) error {
	parts := strings.SplitN(strings.TrimSpace(args), " ", 2)
	if len(parts) == 0 || parts[0] == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, p.help())
	}
	reportType := parts[0]
	if len(parts) >= 2 && strings.TrimSpace(parts[1]) != "" {
		return p.submit(ctx, ev, reportType, strings.TrimSpace(parts[1]))
	}
	if len(imageURLs(ev)) > 0 {
		return p.submit(ctx, ev, reportType, "")
	}
	if !utils.Admin(ctx, p.ctx.Client, ev) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "只有管理员可以查看BUG反馈报告。\n请输入 #反馈 查看使用帮助！")
	}
	reports, err := p.load(ctx)
	if err != nil {
		return err
	}
	if reportType == "全部" {
		return p.sendReports(ctx, ev, reports)
	}
	if !p.validType(reportType) {
		return onebot.Reply(ctx, p.ctx.Client, ev, p.help())
	}
	return p.sendReports(ctx, ev, ReportData{reportType: reports[reportType]})
}

func (p *RptPlugin) submit(ctx context.Context, ev napcat.Event, reportType string, content string) error {
	if !p.validType(reportType) {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("无效的报告类型或内容为空。\n支持的类型：%s", strings.Join(p.ctx.Config.ReportTypes, "/")))
	}
	reports, err := p.load(ctx)
	if err != nil {
		return err
	}
	images := imageURLs(ev)
	segments := make([]ReportSegment, 0, 1+len(images))
	if content != "" {
		segments = append(segments, ReportSegment{Type: "text", Content: content})
	}
	for _, imageURL := range images {
		segments = append(segments, ReportSegment{Type: "image", Content: imageURL})
	}
	reports[reportType] = append(reports[reportType], ReportEntry{
		UserID:    strconv.FormatInt(onebot.UserID(ev), 10),
		Segments:  segments,
		Timestamp: time.Now().Format("2006-01-02 15:04:05"),
	})
	if err := p.save(ctx, reports); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, "BUG报告提交成功！类型："+reportType)
}

func (p *RptPlugin) debug(ctx context.Context, ev napcat.Event, args string) error {
	if !utils.Admin(ctx, p.ctx.Client, ev) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "权限不足:\n只有指定群管理员可以删除报告。")
	}
	parts := strings.Fields(args)
	if len(parts) != 2 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "参数错误！使用方法：#删除反馈 [类型] [编号]")
	}
	reportType := parts[0]
	idx, err := strconv.Atoi(parts[1])
	if err != nil || idx < 1 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "编号必须是数字！")
	}
	if !p.validType(reportType) {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("无效的报告类型。支持的类型: %s", strings.Join(p.ctx.Config.ReportTypes, "/")))
	}
	reports, err := p.load(ctx)
	if err != nil {
		return err
	}
	list := reports[reportType]
	if idx > len(list) {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("删除失败！【%s】类型中没有第 %d 号报告。", reportType, idx))
	}
	reports[reportType] = append(list[:idx-1], list[idx:]...)
	if err := p.save(ctx, reports); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("已成功删除【%s】类型的第 %d 号报告。", reportType, idx))
}

func (p *RptPlugin) sendReports(ctx context.Context, ev napcat.Event, reports ReportData) error {
	var nodes []any
	for _, reportType := range p.reportTypesWithAll(reports) {
		list := reports[reportType]
		for i, report := range list {
			content := []onebot.Segment{
				onebot.Text(fmt.Sprintf("【%s】反馈 #%d\nQQ：%s\n时间：%s\n", reportType, i+1, report.UserID, report.Timestamp)),
			}
			for _, seg := range report.Segments {
				if seg.Type == "image" {
					content = append(content, onebot.Image(seg.Content))
				} else {
					content = append(content, onebot.Text(seg.Content+"\n"))
				}
			}
			nodes = append(nodes, map[string]any{
				"type": "node",
				"data": map[string]any{
					"name":    "反馈用户 " + report.UserID,
					"uin":     report.UserID,
					"content": content,
				},
			})
		}
	}
	if len(nodes) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "暂无反馈报告。")
	}
	return onebot.SendForward(ctx, p.ctx.Client, ev, nodes)
}

func (p *RptPlugin) load(ctx context.Context) (ReportData, error) {
	kv, err := p.ctx.Store.KV("report")
	if err != nil {
		return nil, err
	}
	reports := ReportData{}
	_, _ = kv.Get(ctx, "reports", &reports)
	for _, reportType := range p.ctx.Config.ReportTypes {
		if reports[reportType] == nil {
			reports[reportType] = []ReportEntry{}
		}
	}
	return reports, nil
}

func (p *RptPlugin) save(ctx context.Context, reports ReportData) error {
	kv, err := p.ctx.Store.KV("report")
	if err != nil {
		return err
	}
	return kv.Set(ctx, "reports", reports)
}

func (p *RptPlugin) help() string {
	return fmt.Sprintf("使用方法：\n#反馈 [类型(全部/%s)] - 查看报告\n#反馈 [类型(%s)] [内容] - 提交报告\n#删除反馈 [类型] [编号] - 删除报告", strings.Join(p.ctx.Config.ReportTypes, "/"), strings.Join(p.ctx.Config.ReportTypes, "/"))
}

func (p *RptPlugin) validType(reportType string) bool {
	for _, item := range p.ctx.Config.ReportTypes {
		if item == reportType {
			return true
		}
	}
	return false
}

func (p *RptPlugin) reportTypesWithAll(reports ReportData) []string {
	var out []string
	seen := map[string]bool{}
	for _, reportType := range p.ctx.Config.ReportTypes {
		if _, ok := reports[reportType]; ok {
			out = append(out, reportType)
			seen[reportType] = true
		}
	}
	for reportType := range reports {
		if !seen[reportType] {
			out = append(out, reportType)
		}
	}
	return out
}

func imageURLs(ev napcat.Event) []string {
	var urls []string
	if url := utils.FirstImgURL(ev); url != "" {
		urls = append(urls, url)
	}
	if segs, ok := ev["message"].([]any); ok {
		for _, item := range segs {
			seg, _ := item.(map[string]any)
			if seg["type"] != "image" {
				continue
			}
			data, _ := seg["data"].(map[string]any)
			if url, _ := data["url"].(string); url != "" && !containsString(urls, url) {
				urls = append(urls, url)
			}
		}
	}
	return urls
}

func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}

func init() {
	utils.Add("rpt", func(ctx utils.Context) *RptPlugin {
		p := &RptPlugin{ctx: ctx}
		p.Init("rpt")
		p.Command("#反馈", nil, p.bug, "提交问题或建议")
		p.Command("#删除反馈", nil, p.debug, "管理员删除反馈")
		return p
	}, "report")
}
