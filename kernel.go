package cablectl

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/acarl005/stripansi"
	"github.com/busthorne/cablectl/gateway"
	"github.com/crackcomm/go-jupyter/jupyter"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Kernel struct {
	ID        uuid.UUID
	Name      string
	Session   string
	User      string
	Status    string
	KeepAlive time.Duration
	Client    *gateway.Client
	URL       *url.URL

	in     chan string
	out    chan *Content
	conn   *websocket.Conn
	cancel context.CancelFunc
}

func (k *Kernel) read(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			var m Message
			if err := k.conn.ReadJSON(&m); err != nil {
				return fmt.Errorf("failed to read message: %w", err)
			}
			switch m.Type {
			case "status":
				var status jupyter.StatusMessage
				if err := m.Unmarshal(&status); err != nil {
					return fmt.Errorf("failed to unmarshal status: %w", err)
				}
				k.Status = string(status.ExecutionState)
			case "stream", "display_data", "execute_reply":
				var c Content
				if err := m.Unmarshal(&c); err != nil {
					return fmt.Errorf("failed to unmarshal stream: %w", err)
				}
				if h := m.ParentHeader; h != nil {
					c.Message = uuid.MustParse(h.ID)
				}
				k.out <- &c
			}
		}
	}
}

func (k *Kernel) submit(ctx context.Context, code string) (uuid.UUID, error) {
	if k.Status == "busy" {
		if err := k.Interrupt(ctx); err != nil {
			return uuid.Nil, fmt.Errorf("busy kernel: %w", err)
		}
	}

	c := map[string]any{
		"code":             code,
		"silent":           false,
		"store_history":    true,
		"user_expressions": map[string]any{},
		"allow_stdin":      false,
	}
	b, err := json.Marshal(c)
	if err != nil {
		return uuid.Nil, fmt.Errorf("failed to marshal execute request: %w", err)
	}
	id := uuid.New()
	return id, k.conn.WriteJSON(&Message{
		Header: &Header{
			Type:     "execute_request",
			ID:       id.String(),
			Username: k.User,
			Session:  k.Session,
			Version:  "5.0",
		},
		ParentHeader: &Header{},
		Channel:      "shell",
		Content:      b,
		Metadata:     map[string]any{},
		Buffers:      []any{},
	})
}

type Content struct {
	Message uuid.UUID `json:"-"`

	// Actual content
	Channel string `json:"channel,omitempty"`
	Code    string `json:"code,omitempty"` // stream data
	Text    string `json:"text,omitempty"` // stream data
	Data    *Data  `json:"data,omitempty"`

	// Result
	Status         string    `json:"status"`
	ExecutionCount int       `json:"execution_count"`
	Timestamp      time.Time `json:"date"`

	// Metadata
	Metadata  map[string]any `json:"metadata"`
	Transient map[string]any `json:"transient"`
	Error     *Error         `json:"-"`
}

// String64 is a base64 encoded string.
type String64 string

func (s String64) Bytes() ([]byte, error) {
	return base64.StdEncoding.DecodeString(string(s))
}

type Data struct {
	Plaintext string `json:"text/plain"`
	Markdown  string `json:"text/markdown"`
	Latex     string `json:"text/latex"`
	JS        string `json:"application/javascript"`
	JSON      string `json:"application/json"`
	HTML      string `json:"text/html"`

	PNG String64 `json:"image/png"`
	JPG String64 `json:"image/jpeg"`
	SVG String64 `json:"image/svg+xml"`
}

func (m *Data) Text() (string, bool) {
	switch {
	case m.Plaintext != "":
		return m.Plaintext, true
	case m.Markdown != "":
		return m.Markdown, true
	case m.Latex != "":
		return m.Latex, true
	case m.JS != "":
		return m.JS, true
	case m.JSON != "":
		return m.JSON, true
	case m.HTML != "":
		return m.HTML, true
	}
	return "", false
}

func (m *Data) Multipart() (b []byte, mimeType string, err error) {
	switch {
	case m.PNG != "":
		mimeType = "image/png"
		b, err = m.PNG.Bytes()
	case m.JPG != "":
		mimeType = "image/jpeg"
		b, err = m.JPG.Bytes()
	case m.SVG != "":
		mimeType = "image/svg+xml"
		b, err = m.SVG.Bytes()
	}
	if err != nil || b != nil {
		return
	}
	return nil, "", io.EOF
}

type Error struct {
	Ename     string   `json:"ename"`
	Evalue    string   `json:"evalue"`
	Traceback []string `json:"traceback"`

	err error `json:"-"`
}

func (e Error) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return fmt.Sprintf("%s: %s", e.Ename, e.Evalue)
}

func (e Error) String() string {
	if e.err != nil {
		return fmt.Sprintf("%+v", e.err)
	}
	var s strings.Builder
	s.WriteString(e.Ename)
	s.WriteString(": ")
	s.WriteString(e.Evalue)
	for _, tb := range e.Traceback {
		s.WriteString("\n")
		s.WriteString(stripansi.Strip(tb))
	}
	return s.String()
}
