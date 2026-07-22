package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync"

	"arcaeabot/internal/database"
	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type GuessPlugin struct {
	utils.Base
	ctx utils.Context
	mu  sync.Mutex
}

type guessSongState struct {
	Active  bool     `json:"active"`
	Answer  string   `json:"answer"`
	Masked  string   `json:"masked"`
	Guessed []string `json:"guessed"`
}

func (p *GuessPlugin) handle(ctx context.Context, ev napcat.Event) error {
	text := strings.TrimSpace(onebot.RawMessage(ev))
	if text == "" {
		return nil
	}
	if !strings.HasPrefix(text, "#猜歌") && !strings.HasPrefix(text, "#取消猜歌") &&
		!strings.HasPrefix(text, "#开") && !strings.HasPrefix(text, "#连开") &&
		!strings.HasPrefix(text, "#连连开") && !strings.HasPrefix(text, "#猜 ") &&
		!strings.HasPrefix(text, "#添加歌曲") {
		return nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	kv, err := p.ctx.Store.KV("guess_song")
	if err != nil {
		return err
	}
	groupKey := "state_" + strconv.FormatInt(onebot.GroupID(ev), 10)
	var state guessSongState
	_, _ = kv.Get(ctx, groupKey, &state)
	switch {
	case strings.HasPrefix(text, "#猜歌"):
		return p.start(ctx, ev, kv, groupKey, strings.TrimSpace(strings.TrimPrefix(text, "#猜歌")))
	case text == "#取消猜歌":
		if !state.Active {
			return onebot.Reply(ctx, p.ctx.Client, ev, "当前没有进行中的猜歌。")
		}
		_ = kv.Set(ctx, groupKey, guessSongState{})
		return onebot.Reply(ctx, p.ctx.Client, ev, "已取消猜歌。")
	case strings.HasPrefix(text, "#添加歌曲"):
		if !utils.Admin(ctx, p.ctx.Client, ev) {
			return onebot.Reply(ctx, p.ctx.Client, ev, "仅群管理员可以添加猜歌题目。")
		}
		return p.addSong(ctx, ev, kv, strings.TrimSpace(strings.TrimPrefix(text, "#添加歌曲")))
	}
	if !state.Active {
		return nil
	}
	if strings.HasPrefix(text, "#连连开") {
		if !utils.Admin(ctx, p.ctx.Client, ev) {
			return onebot.Reply(ctx, p.ctx.Client, ev, "仅群管理员可以使用 #连连开。")
		}
		return p.reveal(ctx, ev, kv, groupKey, state, strings.TrimSpace(strings.TrimPrefix(text, "#连连开")), false)
	}
	if strings.HasPrefix(text, "#连开") {
		return p.reveal(ctx, ev, kv, groupKey, state, strings.TrimSpace(strings.TrimPrefix(text, "#连开")), false)
	}
	if strings.HasPrefix(text, "#开") {
		return p.reveal(ctx, ev, kv, groupKey, state, strings.TrimSpace(strings.TrimPrefix(text, "#开")), true)
	}
	if strings.HasPrefix(text, "#猜 ") {
		return p.guess(ctx, ev, kv, groupKey, state, strings.TrimSpace(strings.TrimPrefix(text, "#猜")))
	}
	return nil
}

func (p *GuessPlugin) start(ctx context.Context, ev napcat.Event, kv *database.KV, groupKey, setID string) error {
	if setID == "" {
		setID = strconv.Itoa(rand.IntN(7) + 1)
	}
	answer, err := p.songSet(ctx, kv, setID)
	if err != nil || strings.TrimSpace(answer) == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "找不到对应的猜歌题库。")
	}
	maskedRunes := []rune(answer)
	for i, char := range maskedRunes {
		if char != ' ' && char != '\n' && char != '\r' && char != '\t' {
			maskedRunes[i] = '*'
		}
	}
	state := guessSongState{Active: true, Answer: answer, Masked: string(maskedRunes)}
	if err := kv.Set(ctx, groupKey, state); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, state.Masked)
}

func (p *GuessPlugin) songSet(ctx context.Context, kv *database.KV, setID string) (string, error) {
	var answer string
	_, err := kv.Get(ctx, "song_set_"+setID, &answer)
	return answer, err
}

func (p *GuessPlugin) reveal(ctx context.Context, ev napcat.Event, kv *database.KV, groupKey string, state guessSongState, value string, single bool) error {
	if value == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "请输入要开的字符。")
	}
	requested := []rune(strings.ToLower(value))
	if single {
		requested = requested[:1]
	}
	answer, masked := []rune(state.Answer), []rune(state.Masked)
	for i, char := range answer {
		for _, request := range requested {
			if []rune(strings.ToLower(string(char)))[0] == request && masked[i] == '*' {
				masked[i] = char
			}
		}
	}
	state.Masked = string(masked)
	state.Guessed = append(state.Guessed, value)
	return p.saveAndReply(ctx, ev, kv, groupKey, state, "已开")
}

func (p *GuessPlugin) guess(ctx context.Context, ev napcat.Event, kv *database.KV, groupKey string, state guessSongState, value string) error {
	if value == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "请输入猜测的曲名。")
	}
	answerLines := strings.Split(state.Answer, "\n")
	maskedLines := strings.Split(state.Masked, "\n")
	for i, line := range answerLines {
		if strings.EqualFold(strings.TrimSpace(line), value) && i < len(maskedLines) {
			maskedLines[i] = line
		}
	}
	state.Masked = strings.Join(maskedLines, "\n")
	state.Guessed = append(state.Guessed, value)
	return p.saveAndReply(ctx, ev, kv, groupKey, state, "已猜")
}

func (p *GuessPlugin) saveAndReply(ctx context.Context, ev napcat.Event, kv *database.KV, groupKey string, state guessSongState, action string) error {
	if strings.EqualFold(state.Masked, state.Answer) {
		_ = kv.Set(ctx, groupKey, guessSongState{})
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("%s：%s\n\n%s\n\n恭喜全部猜对！", action, strings.Join(state.Guessed, " "), state.Answer))
	}
	if err := kv.Set(ctx, groupKey, state); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("%s：%s\n\n%s", action, strings.Join(state.Guessed, " "), state.Masked))
}

func (p *GuessPlugin) addSong(ctx context.Context, ev napcat.Event, kv *database.KV, song string) error {
	if song == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "用法：#添加歌曲 [曲名]")
	}
	current, _ := p.songSet(ctx, kv, "8")
	lines := strings.Split(strings.TrimSpace(current), "\n")
	for _, line := range lines {
		if strings.EqualFold(strings.TrimSpace(line), song) {
			return onebot.Reply(ctx, p.ctx.Client, ev, "该曲名已经收录。")
		}
	}
	lines = append(lines, song)
	value := strings.Join(lines, "\n")
	if err := kv.Set(ctx, "song_set_8", value); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, "添加成功！\n\n已收录曲名：\n"+value)
}

func init() {
	utils.Add("guess", func(ctx utils.Context) *GuessPlugin {
		p := &GuessPlugin{ctx: ctx}
		p.Init("guess")
		p.GroupFilter(p.handle)
		return p
	}, "guess_song")
}
