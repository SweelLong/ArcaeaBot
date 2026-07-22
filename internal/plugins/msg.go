package plugins

import (
	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
	"arcaeabot/internal/utils"
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

type RandPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *RandPlugin) handle(ctx context.Context, ev napcat.Event) error {
	text := strings.TrimSpace(onebot.RawMessage(ev))
	if text == "" || (text[0] != '?' && !strings.HasPrefix(text, "？")) {
		return nil
	}
	for _, keyword := range []string{"能不能", "有没有", "能"} {
		index := strings.Index(text, keyword)
		if index < 0 {
			continue
		}
		suffix := strings.NewReplacer("?", "", "？", "", "要", "", "吗", "").Replace(text[index+len(keyword):])
		answer := []string{"能", "不能"}[rand.IntN(2)] + suffix
		return onebot.SendGroup(ctx, p.ctx.Client, onebot.GroupID(ev), []onebot.Segment{
			{"type": "reply", "data": map[string]any{"id": ev.Int("message_id")}},
			onebot.Text(answer),
		})
	}
	return nil
}

type ACVPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *ACVPlugin) handle(ctx context.Context, ev napcat.Event) error {
	if !strings.HasPrefix(strings.TrimSpace(onebot.RawMessage(ev)), "#版本") {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://webapi.lowiro.com/webapi/serve/static/bin/arcaea/apk", nil)
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "获取"+p.ctx.Config.GameName+" c版信息失败: "+err.Error())
	}
	client := http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "获取"+p.ctx.Config.GameName+" c版信息失败: "+err.Error())
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return onebot.Reply(ctx, p.ctx.Client, ev, "无法获取"+p.ctx.Config.GameName+" c版的最新下载链接！")
	}
	var data struct {
		Success bool `json:"success"`
		Value   struct {
			Version string `json:"version"`
			URL     string `json:"url"`
		} `json:"value"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "获取"+p.ctx.Config.GameName+" c版信息失败: "+err.Error())
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("爬取成功: %t\n获取到%s最新下载链接: %s\n若无法下载请检查网络连接！", data.Success, data.Value.Version, data.Value.URL))
}

func init() {
	utils.Add("rand", func(ctx utils.Context) *RandPlugin {
		p := &RandPlugin{ctx: ctx}
		p.Init("rand")
		p.GroupFilter(p.handle)
		return p
	}, "random_answer")
	utils.Add("acv", func(ctx utils.Context) *ACVPlugin {
		p := &ACVPlugin{ctx: ctx}
		p.Init("acv")
		p.GroupFilter(p.handle)
		p.PrivateFilter(p.handle)
		return p
	}, "arcaea_china_version")
}
