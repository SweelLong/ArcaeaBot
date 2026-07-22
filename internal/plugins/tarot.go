package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type TarotPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *TarotPlugin) draw(ctx context.Context, ev napcat.Event, _ string) error {
	cards := p.ctx.Config.TarotCards
	if len(cards) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "塔罗牌配置为空，请联系管理员。")
	}
	number := rand.IntN(len(cards)) + 1
	message := []onebot.Segment{onebot.At(onebot.UserID(ev))}
	path := filepath.Join(p.ctx.Config.ResourcesPath, "tarot", fmt.Sprintf("%d.png", number))
	if file, err := utils.Base64File(path); err == nil {
		message = append(message, onebot.Image(file))
	}
	card := cards[number-1]
	result := "你抽中了" + card.Name
	switch card.Effect {
	case "mute":
		duration := card.Duration
		if duration <= 0 {
			duration = 60
		}
		result += fmt.Sprintf("，禁言%d秒", duration)
		_, _ = p.ctx.Client.Call(ctx, "set_group_ban", map[string]any{
			"group_id": onebot.GroupID(ev), "user_id": onebot.UserID(ev), "duration": duration,
		})
	case "sanity_random":
		if rand.IntN(2) == 0 {
			result += "\n+25 理智"
		} else {
			result += "\n-25 理智"
		}
	case "sanity_full":
		result += "\n理智回满"
	case "sanity_zero":
		result += "\n理智清零"
	}
	message = append(message, onebot.Text("\n"+result))
	return onebot.Reply(ctx, p.ctx.Client, ev, message)
}

func init() {
	utils.Add("tarot", func(ctx utils.Context) *TarotPlugin {
		p := &TarotPlugin{ctx: ctx}
		p.Init("tarot")
		p.Command("#抽塔罗牌", []string{"#塔罗"}, p.draw, "随机抽取一张塔罗牌")
		return p
	})
}
