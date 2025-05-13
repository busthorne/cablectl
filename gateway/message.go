package gateway

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Message struct {
	Header       *Header         `json:"header"`
	ParentHeader *Header         `json:"parent_header"`
	Channel      string          `json:"channel"`
	ID           string          `json:"msg_id"`
	Type         string          `json:"msg_type"`
	Content      json.RawMessage `json:"content"`
	Metadata     map[string]any  `json:"metadata"`
	Buffers      []any           `json:"buffers"`
}

type Header struct {
	ID       string    `json:"msg_id,omitempty"`
	Type     string    `json:"msg_type,omitempty"`
	Username string    `json:"username,omitempty"`
	Session  string    `json:"session,omitempty"`
	Version  string    `json:"version,omitempty"`
	Date     time.Time `json:"date,omitempty"`
}

func (m *Message) Unmarshal(v any) error {
	return json.Unmarshal(m.Content, v)
}

func (m Message) String() string {
	var s strings.Builder
	fmt.Fprintf(&s, "[%s] %s:\n", m.Channel, m.Type)
	enc := json.NewEncoder(&s)
	enc.SetIndent("", "  ")
	enc.Encode(m.Content)
	return s.String()
}
