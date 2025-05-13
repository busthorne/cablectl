package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/acarl005/stripansi"
	"github.com/busthorne/cablectl/gateway/api"
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
	Client    *api.Client
	URL       *url.URL

	in     chan string
	out    chan *Content
	conn   *websocket.Conn
	cancel context.CancelFunc
}

// New attaches a websocket connection to a new, or existing, kernel.
func NewKernel(ctx context.Context, k *Kernel) error {
	switch {
	case k.Name == "":
		return fmt.Errorf("kernel name is required")
	case k.Client == nil:
		if k.URL.String() == "" {
			return fmt.Errorf("kernel gateway url is required")
		}
		gw, err := api.NewClient(k.URL.String())
		if err != nil {
			return fmt.Errorf("failed to create gateway client: %w", err)
		}
		k.Client = gw
	}

	if k.ID == uuid.Nil {
		resp, err := k.Client.PostApiKernels(ctx, api.PostApiKernelsJSONRequestBody{
			Name: &k.Name,
		})
		if err != nil {
			return fmt.Errorf("failed to create kernel: %w", err)
		}
		var kl api.Kernel
		if err := json.NewDecoder(resp.Body).Decode(&kl); err != nil {
			return fmt.Errorf("failed to unmarshal kernel: %w", err)
		}
		defer resp.Body.Close()

		k.ID = kl.Id
		if state := kl.ExecutionState; state != nil {
			k.Status = *state
		}
	}

	dialer := websocket.Dialer{}
	// TODO: URL without schema
	ws := fmt.Sprintf("ws://%s/api/kernels/%s/channels",
		k.URL.Host,
		k.ID)
	conn, _, err := dialer.Dial(ws, http.Header{})
	if err != nil {
		return fmt.Errorf("failed to dial kernel: %w", err)
	}
	k.conn = conn
	ctx, k.cancel = context.WithCancel(ctx)

	if k.in == nil {
		k.in = make(chan string, 1)
	}
	if k.out == nil {
		k.out = make(chan *Content, 1)
	}

	go k.read(ctx)

	if k.KeepAlive == 0 {
		return nil
	}
	// keepalive
	go func() {
		ticker := time.NewTicker(k.KeepAlive)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if k.conn == nil {
					return
				}
				err := k.conn.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					k.out <- &Content{
						Error: &Error{err: err},
					}
					k.Close()
					return
				}
			}
		}
	}()
	return nil
}

// Listen returns a combined stdout/stderr stream.
func (k *Kernel) Listen() <-chan *Content {
	return k.out
}

// Interrupt stops the current kernel execution, & makes way for a new one.
func (k *Kernel) Interrupt(ctx context.Context) error {
	resp, err := k.Client.PostKernelsKernelIdInterrupt(ctx, k.ID)
	if err != nil {
		return fmt.Errorf("failed to interrupt: %w", err)
	}
	return resp.Body.Close()
}

// Shutdown kills the kernel, & releases the resources associated with it.
func (k *Kernel) Shutdown(ctx context.Context) error {
	resp, err := k.Client.DeleteApiKernelsKernelId(ctx, k.ID)
	if err != nil {
		return fmt.Errorf("failed to shutdown: %w", err)
	}
	defer resp.Body.Close()
	k.ID = uuid.Nil
	return k.Close()
}

// Execute
func (k *Kernel) Execute(ctx context.Context, code string) (chan *Content, error) {
	ch := make(chan *Content, 1)

	id, err := k.submit(ctx, code)
	if err != nil {
		return nil, err
	}
	go func() {
		for c := range k.out {
			if c.Message == id {
				ch <- c
				if c.Error != nil || c.Status != "" {
					close(ch)
					return
				}
			}
		}
	}()
	return ch, nil
}

func (k *Kernel) Close() (err error) {
	if k.conn == nil {
		return
	}
	err = k.conn.Close()
	k.conn = nil
	close(k.in)
	close(k.out)
	k.cancel()
	return
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
