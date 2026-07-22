package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"strings"
	"time"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type BoardPlugin struct {
	utils.Base
	ctx utils.Context
}

type boardEntry struct {
	UserID    int64  `json:"user_id"`
	Name      string `json:"name"`
	Content   string `json:"content"`
	CreatedAt int64  `json:"created_at"`
}

func (p *BoardPlugin) leave(ctx context.Context, ev napcat.Event, args string) error {
	content := strings.TrimSpace(args)
	if content == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "用法：#留言 [内容]")
	}
	kv, err := p.ctx.Store.KV("message_board")
	if err != nil {
		return err
	}
	var entries []boardEntry
	_, _ = kv.Get(ctx, "entries", &entries)
	entries = append(entries, boardEntry{
		UserID: onebot.UserID(ev), Name: senderName(ev), Content: content, CreatedAt: time.Now().Unix(),
	})
	if len(entries) > 500 {
		entries = entries[len(entries)-500:]
	}
	if err := kv.Set(ctx, "entries", entries); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, "留言成功！")
}

func (p *BoardPlugin) list(ctx context.Context, ev napcat.Event, _ string) error {
	kv, err := p.ctx.Store.KV("message_board")
	if err != nil {
		return err
	}
	var entries []boardEntry
	_, _ = kv.Get(ctx, "entries", &entries)
	var legacy string
	_, _ = kv.Get(ctx, "legacy_text", &legacy)
	lines := []string{"全部留言："}
	if strings.TrimSpace(legacy) != "" {
		lines = append(lines, strings.TrimSpace(legacy))
	}
	start := max(0, len(entries)-50)
	for _, entry := range entries[start:] {
		name := entry.Name
		if name == "" {
			name = fmt.Sprintf("QQ%d", entry.UserID)
		}
		lines = append(lines, fmt.Sprintf("%s：%s", name, entry.Content))
	}
	if len(lines) == 1 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "暂无留言")
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, strings.Join(lines, "\n"))
}

func senderName(ev napcat.Event) string {
	if sender := ev.Map("sender"); sender != nil {
		if card, _ := sender["card"].(string); strings.TrimSpace(card) != "" {
			return card
		}
		if nickname, _ := sender["nickname"].(string); strings.TrimSpace(nickname) != "" {
			return nickname
		}
	}
	return ""
}

func init() {
	utils.Add("board", func(ctx utils.Context) *BoardPlugin {
		p := &BoardPlugin{ctx: ctx}
		p.Init("board")
		p.Command("#留言", nil, p.leave, "给其他玩家留言")
		p.Command("#查看留言", nil, p.list, "查看留言板")
		return p
	}, "message_board")
}
