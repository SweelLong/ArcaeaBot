package napcat

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	conn    *websocket.Conn
	events  chan Event
	pending sync.Map
	seq     atomic.Int64
	selfID  atomic.Int64
	done    chan struct{}
}

type response struct {
	Status  string          `json:"status"`
	Retcode int             `json:"retcode"`
	Data    json.RawMessage `json:"data"`
	Message string          `json:"message"`
	Wording string          `json:"wording"`
	Echo    string          `json:"echo"`
}

func Connect(ctx context.Context, wsURL, token string) (*Client, error) {
	header := http.Header{}
	if token != "" {
		header.Set("Authorization", "Bearer "+token)
	}
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, header)
	if err != nil {
		return nil, err
	}
	c := &Client{
		conn:   conn,
		events: make(chan Event, 1000),
		done:   make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

func (c *Client) Close() error {
	select {
	case <-c.done:
	default:
		close(c.done)
	}
	return c.conn.Close()
}

func (c *Client) Events() <-chan Event {
	return c.events
}

func (c *Client) SetSelfID(id int64) {
	c.selfID.Store(id)
}

func (c *Client) SelfID() int64 {
	return c.selfID.Load()
}

func (c *Client) Call(ctx context.Context, action string, params map[string]any) (Data, error) {
	echo := fmt.Sprintf("go-%d", c.seq.Add(1))
	req := map[string]any{
		"action": action,
		"params": params,
		"echo":   echo,
	}
	ch := make(chan response, 1)
	c.pending.Store(echo, ch)
	defer c.pending.Delete(echo)

	if err := c.conn.WriteJSON(req); err != nil {
		return nil, err
	}
	timeout, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	select {
	case <-timeout.Done():
		return nil, timeout.Err()
	case resp := <-ch:
		if resp.Status != "ok" || resp.Retcode != 0 {
			return nil, fmt.Errorf("api call %s failed: status=%s retcode=%d message=%q wording=%q data=%s", action, resp.Status, resp.Retcode, resp.Message, resp.Wording, string(resp.Data))
		}
		var data Data
		if len(resp.Data) > 0 && string(resp.Data) != "null" {
			if err := json.Unmarshal(resp.Data, &data); err != nil {
				return nil, err
			}
		}
		return data, nil
	}
}

func (c *Client) readLoop() {
	defer close(c.events)
	for {
		var raw json.RawMessage
		if err := c.conn.ReadJSON(&raw); err != nil {
			return
		}
		var probe struct {
			Echo string `json:"echo"`
		}
		_ = json.Unmarshal(raw, &probe)
		if probe.Echo != "" {
			if v, ok := c.pending.Load(probe.Echo); ok {
				var resp response
				if err := json.Unmarshal(raw, &resp); err == nil {
					v.(chan response) <- resp
				}
				continue
			}
		}
		var ev Event
		if err := json.Unmarshal(raw, &ev); err == nil {
			select {
			case c.events <- ev:
			default:
				<-c.events
				c.events <- ev
			}
		}
	}
}

type Data map[string]any

func (d Data) Int(key string) int64 {
	switch v := d[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		value, _ := v.Int64()
		return value
	case string:
		value, _ := strconv.ParseInt(v, 10, 64)
		return value
	default:
		return 0
	}
}
