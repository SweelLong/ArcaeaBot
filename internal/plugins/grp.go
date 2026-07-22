package plugins

import (
	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
	"arcaeabot/internal/utils"
	"context"
	"fmt"
	"gopkg.in/yaml.v3"
	"log/slog"
	"math/rand/v2"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type AdminPlugin struct {
	utils.Base
	ctx utils.Context
}

var atPattern = regexp.MustCompile(`\[CQ:at,qq=(\d+)[^]]*]`)

func (p *AdminPlugin) groupID(ctx context.Context, ev napcat.Event, _ string) error {
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("本群群号：%d", onebot.GroupID(ev)))
}

func (p *AdminPlugin) muteAll(enable bool) utils.Handler {
	return func(ctx context.Context, ev napcat.Event, _ string) error {
		if err := p.requireAdmin(ctx, ev); err != nil {
			return err
		}
		_, err := p.ctx.Client.Call(ctx, "set_group_whole_ban", map[string]any{"group_id": onebot.GroupID(ev), "enable": enable})
		return err
	}
}

func (p *AdminPlugin) mute(ctx context.Context, ev napcat.Event, args string) error {
	if err := p.requireAdmin(ctx, ev); err != nil {
		return err
	}
	target, ok := mentionedQQ(onebot.RawMessage(ev))
	fields := strings.Fields(args)
	if !ok || len(fields) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "格式：#禁言 @某人 [分钟数]")
	}
	minutes, err := strconv.ParseFloat(fields[len(fields)-1], 64)
	if err != nil || minutes < 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "禁言时间必须是非负数字。")
	}
	_, err = p.ctx.Client.Call(ctx, "set_group_ban", map[string]any{
		"group_id": onebot.GroupID(ev), "user_id": target, "duration": int(minutes * 60),
	})
	if err != nil {
		return err
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("已禁言 %.1f 分钟", minutes))
}

func (p *AdminPlugin) unmute(ctx context.Context, ev napcat.Event, _ string) error {
	if err := p.requireAdmin(ctx, ev); err != nil {
		return err
	}
	target, ok := mentionedQQ(onebot.RawMessage(ev))
	if !ok {
		return onebot.Reply(ctx, p.ctx.Client, ev, "格式：#解禁 @某人")
	}
	_, err := p.ctx.Client.Call(ctx, "set_group_ban", map[string]any{"group_id": onebot.GroupID(ev), "user_id": target, "duration": 0})
	return err
}

func (p *AdminPlugin) kick(ctx context.Context, ev napcat.Event, _ string) error {
	if err := p.requireAdmin(ctx, ev); err != nil {
		return err
	}
	target, ok := mentionedQQ(onebot.RawMessage(ev))
	if !ok {
		return onebot.Reply(ctx, p.ctx.Client, ev, "格式：#踢 @某人")
	}
	_, err := p.ctx.Client.Call(ctx, "set_group_kick", map[string]any{"group_id": onebot.GroupID(ev), "user_id": target, "reject_add_request": false})
	return err
}

func (p *AdminPlugin) title(ctx context.Context, ev napcat.Event, args string) error {
	if err := p.requireAdmin(ctx, ev); err != nil {
		return err
	}
	target, ok := mentionedQQ(onebot.RawMessage(ev))
	title := strings.TrimSpace(atPattern.ReplaceAllString(args, ""))
	if !ok || title == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "格式：#头衔 @某人 [头衔]")
	}
	_, err := p.ctx.Client.Call(ctx, "set_group_special_title", map[string]any{"group_id": onebot.GroupID(ev), "user_id": target, "special_title": title})
	return err
}

func (p *AdminPlugin) requireAdmin(ctx context.Context, ev napcat.Event) error {
	if utils.Admin(ctx, p.ctx.Client, ev) {
		return nil
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, "权限不足，仅群管理员可使用该指令。")
}

func mentionedQQ(raw string) (int64, bool) {
	match := atPattern.FindStringSubmatch(raw)
	if len(match) != 2 {
		return 0, false
	}
	value, err := strconv.ParseInt(match[1], 10, 64)
	return value, err == nil
}

type FilesPlugin struct {
	utils.Base
	ctx utils.Context
}

func (p *FilesPlugin) moveGroupFile(ctx context.Context, groupID int64, fileID, currentFolderID, targetFolderID string) error {
	if fileID == "" || currentFolderID == "" || targetFolderID == "" {
		return fmt.Errorf("invalid group file move parameters")
	}
	params := map[string]any{
		"group_id": groupID, "file_id": fileID,
		"current_parent_directory": currentFolderID,
		"target_parent_directory":  targetFolderID,
	}
	slog.Info("calling move_group_file", "params", params)
	data, err := p.ctx.Client.Call(ctx, "move_group_file", params)
	if err != nil {
		return err
	}
	slog.Info("move_group_file succeeded", "group_id", groupID, "file_id", fileID, "response", data)
	return nil
}

func (p *FilesPlugin) list(ctx context.Context, ev napcat.Event, args string) error {
	if !utils.Admin(ctx, p.ctx.Client, ev) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "仅群管理员可以查看群文件列表。")
	}
	count := 50
	if value, err := strconv.Atoi(strings.TrimSpace(args)); err == nil && value > 0 {
		count = min(value, 200)
	}
	data, err := p.ctx.Client.Call(ctx, "get_group_root_files", map[string]any{"group_id": onebot.GroupID(ev), "file_count": count})
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "获取群文件失败。")
	}
	files, _ := data["files"].([]any)
	if len(files) == 0 {
		return onebot.Reply(ctx, p.ctx.Client, ev, "暂无文件。")
	}
	lines := []string{"群文件列表："}
	for _, item := range files {
		file, _ := item.(map[string]any)
		lines = append(lines, fmt.Sprintf("%s (%.1fKB, id:%s)", mapStringAny(file, "file_name", "name"), float64(mapIntAny(file, "file_size", "size"))/1024, mapStringAny(file, "file_id", "id")))
	}
	message := strings.Join(lines, "\n")
	if len(message) > 2000 {
		message = message[:1997] + "..."
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, message)
}

func (p *FilesPlugin) move(ctx context.Context, ev napcat.Event, args string) error {
	if !utils.Admin(ctx, p.ctx.Client, ev) {
		return onebot.Reply(ctx, p.ctx.Client, ev, "仅群管理员可以移动群文件。")
	}
	folderName := strings.TrimSpace(args)
	if folderName == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "用法：#移动文件 [目标文件夹名称]")
	}
	data, err := p.ctx.Client.Call(ctx, "get_group_root_files", map[string]any{"group_id": onebot.GroupID(ev), "file_count": 200})
	if err != nil {
		return onebot.Reply(ctx, p.ctx.Client, ev, "获取群文件失败。")
	}
	var targetID string
	folders, _ := data["folders"].([]any)
	for _, item := range folders {
		folder, _ := item.(map[string]any)
		if mapStringAny(folder, "folder_name", "name") == folderName {
			targetID = mapStringAny(folder, "folder_id", "id")
			break
		}
	}
	if targetID == "" {
		return onebot.Reply(ctx, p.ctx.Client, ev, "找不到文件夹："+folderName)
	}
	moved := 0
	files, _ := data["files"].([]any)
	for _, item := range files {
		file, _ := item.(map[string]any)
		if err := p.moveGroupFile(ctx, onebot.GroupID(ev), mapStringAny(file, "file_id", "id"), "/", targetID); err == nil {
			moved++
		}
	}
	return onebot.Reply(ctx, p.ctx.Client, ev, fmt.Sprintf("移动成功，共移动 %d 个文件。", moved))
}

func mapString(data map[string]any, key string) string {
	value, _ := data[key].(string)
	return value
}

func mapStringAny(data map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := mapString(data, key); value != "" {
			return value
		}
		if value, ok := data[key].(float64); ok {
			return strconv.FormatInt(int64(value), 10)
		}
		if value, ok := data[key].(int64); ok {
			return strconv.FormatInt(value, 10)
		}
		if value, ok := data[key].(int); ok {
			return strconv.Itoa(value)
		}
	}
	return ""
}

func mapInt(data map[string]any, key string) int64 {
	switch value := data[key].(type) {
	case float64:
		return int64(value)
	case int64:
		return value
	case int:
		return int64(value)
	default:
		return 0
	}
}

func mapIntAny(data map[string]any, keys ...string) int64 {
	for _, key := range keys {
		if value := mapInt(data, key); value != 0 {
			return value
		}
	}
	return 0
}

type NoticePlugin struct {
	utils.Base
	ctx     utils.Context
	replies []string
}

func (p *NoticePlugin) handle(ctx context.Context, ev napcat.Event) error {
	if ev.String("post_type") != "notice" {
		return nil
	}
	switch ev.String("notice_type") {
	case "notify":
		return p.poke(ctx, ev)
	case "group_increase":
		return p.groupIncrease(ctx, ev)
	case "group_decrease":
		return p.groupDecrease(ctx, ev)
	default:
		return nil
	}
}

func (p *NoticePlugin) poke(ctx context.Context, ev napcat.Event) error {
	if ev.String("sub_type") != "poke" {
		return nil
	}
	if target := ev.Int("target_id"); target != 0 && target != p.ctx.Client.SelfID() {
		return nil
	}
	if len(p.replies) == 0 {
		return nil
	}
	msg := p.replies[rand.IntN(len(p.replies))]
	if groupID := onebot.GroupID(ev); groupID != 0 {
		return onebot.SendGroup(ctx, p.ctx.Client, groupID, msg)
	}
	return onebot.SendPrivate(ctx, p.ctx.Client, onebot.UserID(ev), msg)
}

func (p *NoticePlugin) groupIncrease(ctx context.Context, ev napcat.Event) error {
	groupID := onebot.GroupID(ev)
	userID := ev.Int("user_id")
	if groupID == 0 || userID == 0 {
		return nil
	}
	return onebot.SendGroup(ctx, p.ctx.Client, groupID, []onebot.Segment{
		onebot.At(userID),
		onebot.Text(fmt.Sprintf(" 欢迎光临，输入 #帮助 查看 %s 帮助菜单哦！", p.ctx.Config.GameName)),
	})
}

func (p *NoticePlugin) groupDecrease(ctx context.Context, ev napcat.Event) error {
	groupID := onebot.GroupID(ev)
	userID := ev.Int("user_id")
	if groupID == 0 || userID == 0 {
		return nil
	}
	displayName := strconv.FormatInt(userID, 10)
	var gameUserID int64
	var username string
	if p.ctx.Arcaea.QueryRowContext(ctx, "SELECT user_id, name FROM user WHERE LOWER(TRIM(email))=?", utils.QQEmail(userID)).Scan(&gameUserID, &username) == nil {
		if username != "" {
			displayName = fmt.Sprintf("%s(%d)", username, userID)
		}
		_, _ = p.ctx.Arcaea.ExecContext(ctx, "UPDATE user SET password='' WHERE user_id=?", gameUserID)
	}
	if displayName == strconv.FormatInt(userID, 10) {
		if data, err := p.ctx.Client.Call(ctx, "get_group_member_info", map[string]any{
			"group_id": groupID, "user_id": userID, "no_cache": false,
		}); err == nil {
			name := ""
			if value, ok := data["card"].(string); ok {
				name = value
			}
			if name == "" {
				if value, ok := data["nickname"].(string); ok {
					name = value
				}
			}
			if name != "" {
				displayName = fmt.Sprintf("%s(%d)", name, userID)
			}
		}
	}
	action := "离开了群聊"
	if ev.String("sub_type") == "kick" || ev.String("sub_type") == "kick_me" {
		action = "被移出了群聊"
	}
	return onebot.SendGroup(ctx, p.ctx.Client, groupID, fmt.Sprintf("%s %s。", displayName, action))
}

func init() {
	utils.Add("admin", func(ctx utils.Context) *AdminPlugin {
		p := &AdminPlugin{ctx: ctx}
		p.Init("admin")
		p.Command("#群号", nil, p.groupID, "查看当前群号")
		p.Command("#全体禁言", nil, p.muteAll(true), "管理员开启全体禁言")
		p.Command("#全体解禁", nil, p.muteAll(false), "管理员解除全体禁言")
		p.Command("#禁言", nil, p.mute, "管理员禁言成员")
		p.Command("#解禁", nil, p.unmute, "管理员解除成员禁言")
		p.Command("#踢", nil, p.kick, "管理员踢出成员")
		p.Command("#头衔", nil, p.title, "管理员设置成员头衔")
		return p
	}, "group_admin")
	utils.Add("files", func(ctx utils.Context) *FilesPlugin {
		p := &FilesPlugin{ctx: ctx}
		p.Init("files")
		p.Command("#群文件", nil, p.list, "管理员查看群文件")
		p.Command("#移动文件", nil, p.move, "管理员移动群文件")
		return p
	}, "group_files")
	utils.Add("notice", func(ctx utils.Context) *NoticePlugin {
		p := &NoticePlugin{ctx: ctx, replies: []string{"输入 #帮助 查看帮助菜单哦！"}}
		p.Init("notice")
		if raw, err := os.ReadFile(filepath.Join(ctx.Config.ResourcesPath, "chat", "poke.yaml")); err == nil {
			var data []string
			if yaml.Unmarshal(raw, &data) == nil && len(data) > 0 {
				p.replies = data
			}
		}
		p.GroupFilter(p.handle)
		return p
	})
}
