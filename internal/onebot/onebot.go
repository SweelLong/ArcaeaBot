package onebot

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"arcaeabot/internal/napcat"
)

type Segment map[string]any

func IsGroupMessage(event napcat.Event) bool {
	return event.String("post_type") == "message" && event.String("message_type") == "group"
}

func IsPrivateMessage(event napcat.Event) bool {
	return event.String("post_type") == "message" && event.String("message_type") == "private"
}

func IsNotice(event napcat.Event, notice string) bool {
	return event.String("post_type") == "notice" && event.String("notice_type") == notice
}

func UserID(event napcat.Event) int64 {
	if sender := event.Map("sender"); sender != nil {
		if id := anyInt(sender["user_id"]); id != 0 {
			return id
		}
	}
	return event.Int("user_id")
}

func GroupID(event napcat.Event) int64 {
	return event.Int("group_id")
}

func RawMessage(event napcat.Event) string {
	if raw := event.String("raw_message"); raw != "" {
		return raw
	}
	if msg, ok := event["message"].(string); ok {
		return msg
	}
	if segs, ok := event["message"].([]any); ok {
		var b strings.Builder
		for _, item := range segs {
			seg, ok := item.(map[string]any)
			if !ok || seg["type"] != "text" {
				continue
			}
			data, _ := seg["data"].(map[string]any)
			if text, _ := data["text"].(string); text != "" {
				b.WriteString(text)
			}
		}
		return b.String()
	}
	return ""
}

func Text(text string) Segment {
	return Segment{"type": "text", "data": map[string]any{"text": text}}
}

func At(qq int64) Segment {
	return Segment{"type": "at", "data": map[string]any{"qq": strconv.FormatInt(qq, 10)}}
}

func Image(file string) Segment {
	return Segment{"type": "image", "data": map[string]any{"file": file}}
}

func Sticker(file string) Segment {
	return Segment{"type": "image", "data": map[string]any{"file": file, "sub_type": 1}}
}

func SendGroup(ctx context.Context, client *napcat.Client, groupID int64, message any) error {
	_, err := client.Call(ctx, "send_group_msg", map[string]any{"group_id": groupID, "message": message})
	return err
}

func SendPrivate(ctx context.Context, client *napcat.Client, userID int64, message any) error {
	_, err := client.Call(ctx, "send_private_msg", map[string]any{"user_id": userID, "message": message})
	return err
}

func SendForward(ctx context.Context, client *napcat.Client, event napcat.Event, nodes []any) error {
	if IsGroupMessage(event) {
		_, err := client.Call(ctx, "send_group_forward_msg", map[string]any{
			"group_id": GroupID(event), "messages": nodes,
		})
		return err
	}
	if IsPrivateMessage(event) {
		_, err := client.Call(ctx, "send_private_forward_msg", map[string]any{
			"user_id": UserID(event), "messages": nodes,
		})
		return err
	}
	return fmt.Errorf("event is not a message")
}

func Reply(ctx context.Context, client *napcat.Client, event napcat.Event, message any) error {
	if IsGroupMessage(event) {
		return SendGroup(ctx, client, GroupID(event), message)
	}
	if IsPrivateMessage(event) {
		return SendPrivate(ctx, client, UserID(event), message)
	}
	return fmt.Errorf("event is not a message")
}

func anyInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	default:
		return 0
	}
}
