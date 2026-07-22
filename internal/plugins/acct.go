package plugins

import (
	"arcaeabot/internal/utils"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

type AcctPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *AcctPlugin) verifyIdentity(ctx context.Context, ev napcat.Event) bool {
	userID := onebot.UserID(ev)
	if p.isWhitelistedMember(userID, func(groupID int64) error {
		_, err := p.ctx.Client.Call(ctx, "get_group_member_info", map[string]any{
			"group_id": groupID,
			"user_id":  userID,
		})
		return err
	}) {
		return true
	}
	_ = onebot.Reply(ctx, p.ctx.Client, ev, "身份验证失败...\n请确保获得游玩许可！")
	return false
}

func (p *AcctPlugin) isWhitelistedMember(userID int64, isMember func(int64) error) bool {
	if userID == 0 || len(p.ctx.Config.GroupWhitelist) == 0 {
		return false
	}
	for _, groupID := range p.ctx.Config.GroupWhitelist {
		if groupID == 0 {
			continue
		}
		if err := isMember(groupID); err == nil {
			return true
		}
	}
	return false
}

func (p *AcctPlugin) qqInWhitelist(ctx context.Context, qq int64) bool {
	return p.isWhitelistedMember(qq, func(groupID int64) error {
		_, err := p.ctx.Client.Call(ctx, "get_group_member_info", map[string]any{
			"group_id": groupID,
			"user_id":  qq,
		})
		return err
	})
}

func (p *AcctPlugin) secretReply(ctx context.Context, ev napcat.Event, text string) error {
	if onebot.IsGroupMessage(ev) {
		return onebot.SendGroup(ctx, p.ctx.Client, onebot.GroupID(ev), text)
	}
	return onebot.SendPrivate(ctx, p.ctx.Client, onebot.UserID(ev), text)
}

func (p *AcctPlugin) register(ctx context.Context, ev napcat.Event, args string) error {
	if !p.verifyIdentity(ctx, ev) {
		return nil
	}
	_, _ = p.ctx.Client.Call(ctx, "delete_msg", map[string]any{"message_id": ev.Int("message_id")})
	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return p.secretReply(ctx, ev, "请输入您的用户名和密码！")
	}
	username, password := parts[0], parts[1]
	if !isASCII(username) {
		return p.secretReply(ctx, ev, "用户名包含了特殊字符！")
	}
	db := p.ctx.Arcaea
	qq := onebot.UserID(ev)
	if _, err := utils.UserID(ctx, db, qq); err == nil {
		return p.secretReply(ctx, ev, "您已经绑定过账号了，不能重复注册哦~")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if utils.ExistsRow(ctx, db, "SELECT 1 FROM user WHERE name=?", username) {
		return p.secretReply(ctx, ev, fmt.Sprintf("啊哦，用户名 %s 已被注册惹！", username))
	}
	userCode, err := p.newUserCode(ctx)
	if err != nil {
		return p.secretReply(ctx, ev, "注册名额已满，请稍后再试吧！")
	}
	userID := int64(2000001)
	_ = db.QueryRowContext(ctx, "SELECT COALESCE(MAX(user_id), 2000000) + 1 FROM user").Scan(&userID)
	hash := sha256.Sum256([]byte(password))
	now := time.Now().UnixMilli()
	result, err := db.ExecContext(ctx, `INSERT OR IGNORE INTO user(user_id, name, email, password, join_date, user_code, rating_ptt,
character_id, is_skill_sealed, is_char_uncapped, is_char_uncapped_override, is_hide_rating, favorite_character,
max_stamina_notification_enabled, current_map, ticket, prog_boost)
VALUES(?, ?, ?, ?, ?, ?, 0, 0, 0, 0, 0, 0, -1, 0, '', ?, 0)`,
		userID, username, utils.QQEmail(qq), hex.EncodeToString(hash[:]), now, userCode, p.ctx.Config.DefaultTicket)
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "注册失败，请稍后再试！")
	}
	if utils.Affected(result) == 0 {
		return p.secretReply(ctx, ev, "您已经绑定过账号了，不能重复注册哦~")
	}
	_, _ = db.ExecContext(ctx, "INSERT OR IGNORE INTO user_char VALUES(?,?,?,?,?,?,0)", userID, 0, 1, 0, 0, 0)
	_, _ = db.ExecContext(ctx, "INSERT OR IGNORE INTO user_char VALUES(?,?,?,?,?,?,0)", userID, 1, 1, 0, 0, 0)
	return p.secretReply(ctx, ev, fmt.Sprintf("注册成功！请查看个人信息~\n- 好友码：%s\n- 用户名：%s\n- 密码：%s\n- QQ：%d\n注：如需修改密码，请私聊我发送「#新密码 你的新密码」。", userCode, username, strings.Repeat("*", len(password)), qq))
}

func (p *AcctPlugin) forgot(ctx context.Context, ev napcat.Event, password string) error {
	if !p.verifyIdentity(ctx, ev) {
		return nil
	}
	_, _ = p.ctx.Client.Call(ctx, "delete_msg", map[string]any{"message_id": ev.Int("message_id")})
	if len(password) < 6 {
		return p.secretReply(ctx, ev, "啊哦，密码长度不能少于6位哦！")
	}
	hash := sha256.Sum256([]byte(password))
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if err != nil {
		return err
	}
	res, err := p.ctx.Arcaea.ExecContext(ctx, "UPDATE user SET password=? WHERE user_id=?", hex.EncodeToString(hash[:]), userID)
	if err != nil {
		return err
	}
	if utils.Affected(res) == 0 {
		return sql.ErrNoRows
	}
	return p.secretReply(ctx, ev, "您的密码修改成功！")
}

func (p *AcctPlugin) bind(ctx context.Context, ev napcat.Event, args string) error {
	if !p.verifyIdentity(ctx, ev) {
		return nil
	}
	_, _ = p.ctx.Client.Call(ctx, "delete_msg", map[string]any{"message_id": ev.Int("message_id")})
	db := p.ctx.Arcaea
	qq := onebot.UserID(ev)
	if _, err := utils.UserID(ctx, db, qq); err == nil {
		return p.secretReply(ctx, ev, "你已经绑定过账号了，不能重复绑定哦！")
	} else if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	userCode := strings.TrimSpace(args)
	if userCode == "" {
		return p.secretReply(ctx, ev, "请输入您的好友码~")
	}
	var userID int64
	var email sql.NullString
	if err := db.QueryRowContext(ctx, "SELECT user_id, email FROM user WHERE user_code=?", userCode).Scan(&userID, &email); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return p.secretReply(ctx, ev, "绑定失败：找不到这个好友码！")
		}
		return err
	}
	if boundQQ, bound := utils.EmailQQ(email.String); bound && boundQQ != qq && p.qqInWhitelist(ctx, boundQQ) {
		return p.secretReply(ctx, ev, "该用户已经绑定过了，请联系管理员查看详情。")
	}
	if _, err := db.ExecContext(ctx, "UPDATE user SET email=? WHERE user_id=?", utils.QQEmail(qq), userID); err != nil {
		return err
	}
	return p.secretReply(ctx, ev, "账号绑定成功！")
}

func (p *AcctPlugin) rename(ctx context.Context, ev napcat.Event, username string) error {
	if !p.verifyIdentity(ctx, ev) {
		return nil
	}
	username = strings.TrimSpace(username)
	if username == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "用户名不能为空哦！")
	}
	if !isASCII(username) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "用户名包含了特殊字符！")
	}
	db := p.ctx.Arcaea
	if utils.ExistsRow(ctx, db, "SELECT 1 FROM user WHERE name=?", username) {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("用户名 %s 已被占用惹！", username))
	}
	var ticket int
	var oldName string
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, "SELECT ticket, name FROM user WHERE user_id=?", userID).Scan(&ticket, &oldName); err != nil {
		return err
	}
	if ticket <= p.ctx.Config.RenameCost {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("您的余额不足，需要 %d 个%s才能重命名！", p.ctx.Config.RenameCost, p.ctx.Config.TicketName))
	}
	_, err = db.ExecContext(ctx, "UPDATE user SET name=?, ticket=ticket-? WHERE user_id=?", username, p.ctx.Config.RenameCost, userID)
	if err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("用户名修改成功！\n%s -> %s\n成功支付 %d 个%s。", oldName, username, p.ctx.Config.RenameCost, p.ctx.Config.TicketName))
}

func (p *AcctPlugin) newUserCode(ctx context.Context) (string, error) {
	for range 1000 {
		var b strings.Builder
		for range 9 {
			b.WriteString(strconv.Itoa(rand.IntN(10)))
		}
		code := b.String()
		if !utils.ExistsRow(ctx, p.ctx.Arcaea, "SELECT 1 FROM user WHERE user_code=?", code) {
			return code, nil
		}
	}
	return "", sql.ErrNoRows
}

func isASCII(s string) bool {
	for _, r := range s {
		if r > 127 {
			return false
		}
	}
	return true
}

func init() {
	utils.Add("acct", func(ctx utils.Context) *AcctPlugin {
		p := &AcctPlugin{ctx: ctx}
		p.Init("acct")
		p.Command("#注册", nil, p.register, "注册新的游戏账号")
		p.Command("#绑定", nil, p.bind, "通过好友码绑定游戏账号")
		p.Command("#新密码", nil, p.forgot, "修改已绑定账号的密码")
		p.Command("#改名", nil, p.rename, fmt.Sprintf("消耗%s修改用户名", p.ctx.Config.TicketName))
		return p
	}, "register")
}
