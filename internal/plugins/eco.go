package plugins

import (
	"arcaeabot/internal/database"
	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"strconv"
	"strings"
	"time"
)

type CheckPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *CheckPlugin) checkIn(ctx context.Context, ev napcat.Event, _ string) error {
	now := time.Now()
	qq := onebot.UserID(ev)
	today := now.Format("2006-01-02")
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, qq)
	if err != nil {
		return err
	}
	tx, err := p.ctx.Arcaea.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	expiredBefore := now.UnixMilli()
	cleanupQueries := []string{
		"DELETE FROM user_present WHERE present_id IN (SELECT present_id FROM present WHERE expire_ts <= ?)",
		"DELETE FROM present_item WHERE present_id IN (SELECT present_id FROM present WHERE expire_ts <= ?)",
	}
	for _, query := range cleanupQueries {
		if _, err := tx.ExecContext(ctx, query, expiredBefore); err != nil {
			return fmt.Errorf("清理过期礼物关联记录失败: %w", err)
		}
	}
	result, err := tx.ExecContext(ctx, "DELETE FROM present WHERE expire_ts <= ?", expiredBefore)
	if err != nil {
		return fmt.Errorf("清理过期礼物记录失败: %w", err)
	}
	cleanedCount := utils.Affected(result)
	presentID := fmt.Sprintf("check_in_%d_%s", qq, today)
	if utils.ExistsRow(ctx, tx, "SELECT 1 FROM user_present WHERE user_id=? AND present_id=?", userID, presentID) ||
		utils.ExistsRow(ctx, tx, "SELECT 1 FROM present WHERE present_id=?", presentID) {
		if err := tx.Commit(); err != nil {
			return err
		}
		logExpiredPresentCleanup(cleanedCount)
		return onebot.Reply(ctx, p.ctx.Client, ev, "今天已经签到过啦！")
	}
	expire := now.Add(time.Duration(p.ctx.Config.CheckInExpireHours) * time.Hour).UnixMilli()
	description := fmt.Sprintf("QQ %d 在 %s 获得的每日签到奖励", qq, today)
	if _, err := tx.ExecContext(ctx, "INSERT INTO present(present_id, description, expire_ts) VALUES (?, ?, ?)", presentID, description, expire); err != nil {
		if database.IsDuplicate(err) {
			return onebot.Reply(ctx, p.ctx.Client, ev, "今天已经签到过啦！")
		}
		return err
	}
	for _, item := range p.ctx.Config.CheckInRewards {
		if _, err := tx.ExecContext(ctx, "INSERT INTO present_item(present_id, item_id, type, amount) VALUES (?, ?, ?, ?)", presentID, item.ItemID, item.Type, item.Amount); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO user_present(user_id, present_id) VALUES (?, ?)", userID, presentID); err != nil {
		if database.IsDuplicate(err) {
			return onebot.Reply(ctx, p.ctx.Client, ev, "今天已经签到过啦！")
		}
		return err
	}
	if err := tx.Commit(); err != nil {
		if database.IsDuplicate(err) {
			return onebot.Reply(ctx, p.ctx.Client, ev, "今天已经签到过啦！")
		}
		return err
	}
	logExpiredPresentCleanup(cleanedCount)
	rewards := make([]string, 0, len(p.ctx.Config.CheckInRewards))
	for _, item := range p.ctx.Config.CheckInRewards {
		rewards = append(rewards, fmt.Sprintf("%d 个%s", item.Amount, item.Name))
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, "🎉 签到成功！\n获得 "+strings.Join(rewards, "、")+"，重新登录游戏即可领取")
}

func logExpiredPresentCleanup(count int64) {
	if count > 0 {
		slog.Info("expired presents cleaned", "count", count)
	}
}

type FragPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *FragPlugin) frag(ctx context.Context, ev napcat.Event, args string) error {
	num, err := strconv.Atoi(strings.TrimSpace(args))
	if err != nil || num <= 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "请输入兑换"+p.ctx.Config.FragName+"的数量")
	}
	db := p.ctx.Arcaea
	var ticket int
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if err != nil {
		return err
	}
	if err := db.QueryRowContext(ctx, "SELECT ticket FROM user WHERE user_id=?", userID).Scan(&ticket); err != nil {
		return err
	}
	if ticket < num {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("余额不足！\n(%d/%d)", num, ticket))
	}
	presentID := fmt.Sprintf("fragment_%d_%d", userID, time.Now().UnixMilli())
	expire := time.Now().Add(time.Duration(p.ctx.Config.FragmentExpireDays) * 24 * time.Hour).UnixMilli()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.ExecContext(ctx, "INSERT INTO present(present_id, expire_ts, description) VALUES (?, ?, ?)", presentID, expire, fmt.Sprintf("QQ %d 兑换的%s奖励", onebot.UserID(ev), p.ctx.Config.FragName))
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO present_item(present_id, item_id, type, amount) VALUES (?, 'fragment', 'fragment', ?)", presentID, num)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "INSERT INTO user_present(user_id, present_id) VALUES (?, ?)", userID, presentID)
	}
	if err == nil {
		_, err = tx.ExecContext(ctx, "UPDATE user SET ticket=ticket-? WHERE user_id=?", num, userID)
	}
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "兑换失败："+err.Error())
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("兑换成功！%s将以礼物形式发放~\n(%d %s -> %d %s)", p.ctx.Config.FragName, num, p.ctx.Config.TicketName, num, p.ctx.Config.FragName))
}

type TxPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *TxPlugin) transfer(ctx context.Context, ev napcat.Event, args string) error {
	parts := strings.Fields(args)
	if len(parts) != 2 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "请提供转账目标和金额，格式：#转账 <QQ号> <金额>")
	}
	amount, err := strconv.Atoi(parts[1])
	if err != nil || amount <= 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "请输入合理的额度！")
	}
	targetQQ := strings.Trim(parts[0], " ")
	targetQQ = strings.TrimPrefix(targetQQ, "[CQ:at,qq=")
	targetQQ = strings.TrimSuffix(targetQQ, "]")
	targetID, err := strconv.ParseInt(targetQQ, 10, 64)
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "请输入正确的用户ID或使用@功能")
	}
	db := p.ctx.Arcaea
	originUserID, originErr := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	targetUserID, targetErr := utils.UserID(ctx, p.ctx.Arcaea, targetID)
	if originErr != nil {
		return originErr
	}
	if targetErr != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "找不到收款方！")
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var originTicket int
	if err := tx.QueryRowContext(ctx, "SELECT ticket FROM user WHERE user_id=?", originUserID).Scan(&originTicket); err != nil {
		return err
	}
	if originTicket < amount {
		return onebot.Reply(ctx, p.ctx.Client, ev, "余额不足！")
	}
	var targetName string
	if err := tx.QueryRowContext(ctx, "SELECT name FROM user WHERE user_id=?", targetUserID).Scan(&targetName); err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "找不到收款方！")
	}
	if _, err := tx.ExecContext(ctx, "UPDATE user SET ticket=ticket+? WHERE user_id=?", amount, targetUserID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "UPDATE user SET ticket=ticket-? WHERE user_id=?", amount, originUserID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("转账成功，%d个%s已转给%s！\n你的%s：%d -> %d", amount, p.ctx.Config.TicketName, targetName, p.ctx.Config.TicketName, originTicket, originTicket-amount))
}

type SnatchPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *SnatchPlugin) snatch(ctx context.Context, ev napcat.Event, args string) error {
	if strings.TrimSpace(args) != "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("#争夺 是全量玩法，不支持指定额度，会自动使用你全部%s参与。", p.ctx.Config.TicketName))
	}
	var userID int64
	var ticket int
	userID, err := utils.UserID(ctx, p.ctx.Arcaea, onebot.UserID(ev))
	if err != nil {
		return err
	}
	tx, err := p.ctx.Arcaea.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := tx.QueryRowContext(ctx, "SELECT ticket FROM user WHERE user_id=? FOR UPDATE", userID).Scan(&ticket); err != nil {
		return err
	}
	stake := ticket
	if ticket <= p.ctx.Config.SnatchEnableTicket {
		return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("诶诶，你才这点%s，想...想干嘛？至少要超过 %d 才能开启争夺战。", p.ctx.Config.TicketName, p.ctx.Config.SnatchEnableTicket))
	}
	kv, err := p.ctx.Store.KV("snatch_data")
	if err != nil {
		return err
	}
	var userMMR, botMMR int
	_, _ = kv.Get(ctx, strconv.FormatInt(onebot.UserID(ev), 10), &userMMR)
	floor := p.ctx.Config.SnatchEnableTicket
	initialBank := max(floor*20, floor+stake)
	if ok, _ := kv.Get(ctx, strconv.FormatInt(p.ctx.Config.QQ, 10), &botMMR); !ok || botMMR < floor {
		botMMR = initialBank
	}
	bonus := 0.0
	if userMMR < 0 {
		bonus = math.Min(0.35, math.Abs(float64(userMMR))*0.0001)
	}
	roll := rand.IntN(100)
	delta := 0
	desc := ""
	if roll < 2 {
		delta = int(float64(stake) * (1.0 + rand.Float64()*0.5) * (1 + bonus))
		desc = "太可恶了！你赢走了一笔巨额"
	} else if roll < 10 {
		delta = int(float64(stake) * (0.5 + rand.Float64()*0.3) * (1 + bonus))
		desc = "哎呀！你赢了不少"
	} else if roll < 35 {
		delta = int(float64(stake) * (0.1 + rand.Float64()*0.25) * (1 + bonus))
		desc = "哎哟，你赢了一些"
	} else if roll < 45 {
		delta = int(float64(stake) * (rand.Float64()*0.1 - 0.05))
		desc = "势均力敌"
	} else if roll < 75 {
		delta = -int(float64(stake) * (0.15 + rand.Float64()*0.2))
		desc = "哎呀，稍显劣势"
	} else {
		delta = -int(float64(stake) * (0.35 + rand.Float64()*0.25))
		desc = "啊哈哈，你输得太彻底了啦"
	}
	if delta > botMMR-floor {
		delta = max(0, botMMR-floor)
	}
	if -delta > ticket {
		delta = -ticket
	}
	newTicket := ticket + delta
	if _, err := tx.ExecContext(ctx, "UPDATE user SET ticket=? WHERE user_id=?", newTicket, userID); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	userMMR += delta
	botMMR -= delta
	_ = kv.Set(ctx, strconv.FormatInt(onebot.UserID(ev), 10), userMMR)
	_ = kv.Set(ctx, strconv.FormatInt(p.ctx.Config.QQ, 10), botMMR)
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("%s！\n本次全量参与：%d %s\n亏损补偿加成：+%.1f%%\n你的%s: %d -> %d\n%s的%s池: %d -> %d", desc, stake, p.ctx.Config.TicketName, bonus*100, p.ctx.Config.TicketName, ticket, newTicket, p.ctx.Config.BotName, p.ctx.Config.TicketName, botMMR+delta, botMMR))
}

func init() {
	utils.Add("check", func(ctx utils.Context) *CheckPlugin {
		p := &CheckPlugin{ctx: ctx}
		p.Init("check")
		p.Command("#签到", nil, p.checkIn, "领取每日签到奖励")
		return p
	}, "check_in")
	utils.Add("frag", func(ctx utils.Context) *FragPlugin {
		p := &FragPlugin{ctx: ctx}
		p.Init("frag")
		p.Command("#"+p.ctx.Config.FragName, nil, p.frag, fmt.Sprintf("使用%s兑换%s", p.ctx.Config.TicketName, p.ctx.Config.FragName))
		return p
	}, "fragment")
	utils.Add("tx", func(ctx utils.Context) *TxPlugin {
		p := &TxPlugin{ctx: ctx}
		p.Init("tx")
		p.Command("#转账", nil, p.transfer, fmt.Sprintf("转移%s", p.ctx.Config.TicketName))
		return p
	}, "transfer")
	utils.Add("snatch", func(ctx utils.Context) *SnatchPlugin {
		p := &SnatchPlugin{ctx: ctx}
		p.Init("snatch")
		p.Command("#抢劫", nil, p.snatch, fmt.Sprintf("看看谁抢%s更厉害", p.ctx.Config.TicketName))
		return p
	})
}
