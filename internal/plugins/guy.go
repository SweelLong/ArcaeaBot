package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type GuyPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *GuyPlugin) random(ctx context.Context, ev napcat.Event, _ string) error {
	files := utils.ImageFiles(filepath.Join(p.ctx.Config.ResourcesPath, "guy"))
	if len(files) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "找不到钙哥表情包惹~")
	}
	selected := files[rand.IntN(len(files))]
	file, err := utils.Base64File(selected)
	if err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, onebot.Sticker(file))
}

func (p *GuyPlugin) manage(ctx context.Context, ev napcat.Event) error {
	msg := strings.TrimSpace(onebot.RawMessage(ev))
	if !strings.HasSuffix(msg, "#添加钙图") && !strings.HasSuffix(msg, "#删除钙图") {
		return nil
	}
	if !utils.Admin(ctx, p.ctx.Client, ev) {
		return nil
	}
	dir := filepath.Join(p.ctx.Config.ResourcesPath, "guy")
	if strings.HasSuffix(msg, "#删除钙图") {
		n := utils.LastInt(msg)
		if n <= 0 {
			return onebot.Reply(ctx, p.ctx.Client, ev, "删除失败，无法获取要删除的图片编号")
		}
		for _, file := range utils.ImageFiles(dir) {
			base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
			if v, _ := strconv.Atoi(base); v == n {
				if err := os.Remove(file); err != nil {
					return err
				}
				return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("钙哥图片 %d 已删除", n))
			}
		}
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("未找到编号为 %d 的钙哥图片", n))
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, "已关闭外部图片下载，请将图片手动放入 data/guy 目录。")
}

func init() {
	utils.Add("guy", func(ctx utils.Context) *GuyPlugin {
		p := &GuyPlugin{ctx: ctx}
		p.Init("guy")
		p.Command("#钙哥", []string{"#钙"}, p.random, "随机发送钙哥图片")
		p.GroupFilter(p.manage)
		return p
	})
}
