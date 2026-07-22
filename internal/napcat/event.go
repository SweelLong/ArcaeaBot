package napcat

import (
	"encoding/json"
	"strconv"
)

type Event map[string]any

func (e Event) String(key string) string {
	if v, ok := e[key].(string); ok {
		return v
	}
	return ""
}

func (e Event) Int(key string) int64 {
	switch v := e[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		id, _ := v.Int64()
		return id
	case string:
		id, _ := strconv.ParseInt(v, 10, 64)
		return id
	default:
		return 0
	}
}

func (e Event) Map(key string) map[string]any {
	if v, ok := e[key].(map[string]any); ok {
		return v
	}
	return nil
}

func (e Event) RawJSON() []byte {
	b, _ := json.Marshal(e)
	return b
}
