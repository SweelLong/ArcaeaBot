package utils

import (
	"context"
	"regexp"
	"strings"

	"arcaeabot/internal/napcat"
	"arcaeabot/internal/onebot"
)

func Admin(ctx context.Context, client *napcat.Client, ev napcat.Event) bool {
	if !onebot.IsGroupMessage(ev) {
		return false
	}
	if sender := ev.Map("sender"); sender != nil {
		if role, _ := sender["role"].(string); role == "owner" || role == "admin" {
			return true
		}
	}
	data, err := client.Call(ctx, "get_group_member_info", map[string]any{
		"group_id": onebot.GroupID(ev),
		"user_id":  onebot.UserID(ev),
	})
	if err != nil {
		return false
	}
	role, _ := data["role"].(string)
	return role == "owner" || role == "admin"
}

func ReplyImg(ctx context.Context, client *napcat.Client, ev napcat.Event, path string) error {
	file, err := Base64File(path)
	if err != nil {
		return err
	}
	return onebot.Reply(ctx, client, ev, onebot.Image(file))
}

var imgURLPattern = regexp.MustCompile(`url=([^,\]]+)`)

func FirstImgURL(ev napcat.Event) string {
	raw := onebot.RawMessage(ev)
	if match := imgURLPattern.FindStringSubmatch(raw); len(match) > 1 {
		return strings.ReplaceAll(match[1], "&amp;", "&")
	}
	if segs, ok := ev["message"].([]any); ok {
		for _, item := range segs {
			seg, _ := item.(map[string]any)
			if seg["type"] != "image" {
				continue
			}
			data, _ := seg["data"].(map[string]any)
			if url, _ := data["url"].(string); url != "" {
				return url
			}
		}
	}
	return ""
}
