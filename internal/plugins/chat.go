package plugins

import (
	"arcaeabot/internal/utils"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
	"gopkg.in/yaml.v3"
)

type ChatPlugin struct {
	utils.Base
	ctx    utils.Context
	keys   []string
	prompt string
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func (p *ChatPlugin) handle(ctx context.Context, ev napcat.Event) error {
	raw := strings.TrimSpace(onebot.RawMessage(ev))
	if raw == "" {
		return nil
	}
	kv, err := p.ctx.Store.KV("chat_history")
	if err != nil {
		return err
	}
	userKey := "chat_history_" + fmt.Sprintf("%d", onebot.UserID(ev))
	if strings.EqualFold(raw, "#重置聊天") {
		_, _ = kv.Delete(ctx, userKey)
		return onebot.Reply(ctx, p.ctx.Client, ev, "对话历史已清空！现在开始新的对话。")
	}
	lower := strings.ToLower(raw)
	triggered := false
	for _, key := range p.keys {
		if strings.HasPrefix(lower, key) {
			triggered = true
			break
		}
	}
	if !triggered {
		return nil
	}
	if p.ctx.Config.LLMURL == "" || p.ctx.Config.LLMAPIKey == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "AI配置未完成。")
	}
	if strings.TrimSpace(p.prompt) == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "chat/chat.yaml 未配置 system_prompt。")
	}
	var history []chatMessage
	_, _ = kv.Get(ctx, userKey, &history)
	userContext := p.userContext(ctx, ev)
	messages := append([]chatMessage{}, history...)
	systemPrompt := strings.NewReplacer(
		"{bot_name}", p.ctx.Config.BotName,
		"{game_name}", p.ctx.Config.GameName,
		"{ticket_name}", p.ctx.Config.TicketName,
	).Replace(p.prompt)
	messages = append(messages,
		chatMessage{Role: "system", Content: systemPrompt},
		chatMessage{Role: "user", Content: userContext + "\n" + raw},
	)
	replyText, err := p.call(ctx, messages)
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "AI暂时没有回应，请稍后再试。")
	}
	history = append(history, chatMessage{Role: "user", Content: raw}, chatMessage{Role: "assistant", Content: replyText})
	if len(history) > p.ctx.Config.MaxChatCount {
		history = history[len(history)-p.ctx.Config.MaxChatCount:]
	}
	_ = kv.Set(ctx, userKey, history)
	return onebot.Reply(ctx, p.ctx.Client, ev, replyText)
}

func (p *ChatPlugin) userContext(ctx context.Context, ev napcat.Event) string {
	var name, code sql.NullString
	var ticket, userID, ptt sql.NullInt64
	boundUserID, err := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if err == nil {
		err = p.ctx.Arcaea.QueryRowContext(ctx, "SELECT name, ticket, user_code, user_id, rating_ptt FROM user WHERE user_id=?", boundUserID).Scan(&name, &ticket, &code, &userID, &ptt)
	}
	if err != nil {
		return fmt.Sprintf("用户QQ: %d。", onebot.UserID(ev))
	}
	return fmt.Sprintf("用户QQ: %d。%s用户名: %s，%s: %d，好友码: %s，用户ID: %d，ptt: %.2f。",
		onebot.UserID(ev), p.ctx.Config.GameName, chatNullString(name, "-"), p.ctx.Config.TicketName, ticket.Int64, chatNullString(code, "-"), userID.Int64, float64(ptt.Int64)/100)
}

func chatNullString(v sql.NullString, fallback string) string {
	if v.Valid && v.String != "" {
		return v.String
	}
	return fallback
}

func (p *ChatPlugin) call(ctx context.Context, messages []chatMessage) (string, error) {
	body := map[string]any{
		"model":           p.ctx.Config.LLMID,
		"messages":        messages,
		"temperature":     0.8,
		"top_p":           0.9,
		"max_tokens":      1024,
		"enable_thinking": false,
	}
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.ctx.Config.LLMURL, bytes.NewReader(raw))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", p.ctx.Config.LLMAPIKey)
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("llm status %s", resp.Status)
	}
	var out struct {
		Choices []struct {
			Message chatMessage `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("empty choices")
	}
	return out.Choices[0].Message.Content, nil
}

func init() {
	utils.Add("chat", func(ctx utils.Context) *ChatPlugin {
		p := &ChatPlugin{ctx: ctx}
		p.Init("chat")
		if raw, err := os.ReadFile(filepath.Join(ctx.Config.ResourcesPath, "chat", "chat.yaml")); err == nil {
			var data struct {
				SystemPrompt string `yaml:"system_prompt"`
			}
			if yaml.Unmarshal(raw, &data) == nil {
				p.prompt = strings.TrimSpace(data.SystemPrompt)
			}
		}
		for _, part := range strings.Fields(ctx.Config.BotName) {
			p.keys = append(p.keys, strings.ToLower(part))
		}
		if len(p.keys) == 0 {
			p.keys = []string{"bot"}
		}
		p.GroupFilter(p.handle)
		p.PrivateFilter(p.handle)
		return p
	}, "chat_ai")
}
