package cablectl

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/acarl005/stripansi"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Kernel struct {
	URL *url.URL

	ID        uuid.UUID
	Name      string
	Session   string
	User      string
	Status    string
	KeepAlive time.Duration

	in  chan string
	out chan *Content

	conn *websocket.Conn
}

func (k *Kernel) Close() (err error) {
	if k.conn == nil {
		return
	}
	err = k.conn.Close()
	k.conn = nil
	close(k.in)
	close(k.out)
	return
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
