// This package provides an interface to Jupyter Enterprise Gateway.
//
// The gateway will be cleverly provisioned with sandboxed kernels
// for each "cable," or code execution environtment that is meant to
// persist across multiple LLM inferences.
package cablectl

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/busthorne/cablectl/gateway"
	"github.com/crackcomm/go-jupyter/jupyter"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

func New(ctx context.Context, kernel *Kernel) error {
	switch {
	case kernel.URL.String() == "":
		return fmt.Errorf("kernel gateway url is required")
	case kernel.Name == "":
		return fmt.Errorf("kernel name is required")
	}

	gw, err := gateway.NewClient(kernel.URL.String())
	if err != nil {
		return fmt.Errorf("failed to create gateway client: %w", err)
	}
	if kernel.ID == uuid.Nil {
		resp, err := gw.PostApiKernels(ctx, gateway.PostApiKernelsJSONRequestBody{
			Name: &kernel.Name,
		})
		if err != nil {
			return fmt.Errorf("failed to create kernel: %w", err)
		}
		var k gateway.Kernel
		if err := json.NewDecoder(resp.Body).Decode(&k); err != nil {
			return fmt.Errorf("failed to unmarshal kernel: %w", err)
		}
		defer resp.Body.Close()

		kernel.ID = k.Id
		if state := k.ExecutionState; state != nil {
			kernel.Status = *state
		}
	}

	dialer := websocket.Dialer{}
	// TODO: URL without schema
	ws := fmt.Sprintf("ws://%s/api/kernels/%s/channels",
		kernel.URL.Host,
		kernel.ID)
	conn, _, err := dialer.Dial(ws, http.Header{})
	if err != nil {
		return fmt.Errorf("failed to dial kernel: %w", err)
	}
	kernel.conn = conn

	if kernel.in == nil {
		kernel.in = make(chan string, 1)
	}
	if kernel.out == nil {
		kernel.out = make(chan *Content, 1)
	}
	if kernel.KeepAlive == 0 {
		return nil
	}

	// keepalive
	go func() {
		ticker := time.NewTicker(kernel.KeepAlive)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if kernel.conn == nil {
					return
				}
				err := kernel.conn.WriteMessage(websocket.PingMessage, nil)
				if err != nil {
					kernel.out <- &Content{
						Error: &Error{err: err},
					}
					kernel.Close()
					return
				}
			}
		}
	}()
	go kernel.read(ctx)
	return nil
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

func (k *Kernel) Listen() <-chan *Content {
	return k.out
}

func (k *Kernel) Submit(ctx context.Context, code string) (uuid.UUID, error) {
	if k.Status == "busy" {
		client, err := gateway.NewClient(k.URL.String())
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to create gateway client: %w", err)
		}
		resp, err := client.PostKernelsKernelIdInterrupt(ctx, k.ID)
		if err != nil {
			return uuid.Nil, fmt.Errorf("failed to interrupt kernel: %w", err)
		}
		defer resp.Body.Close()
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

func (k *Kernel) Execute(ctx context.Context, code string) (*Content, error) {
	id, err := k.Submit(ctx, code)
	if err != nil {
		return nil, err
	}

	for c := range k.out {
		if c.Message == id {
			if c.Error != nil {
				return nil, c.Error
			}
			return c, nil
		}
	}
	return nil, fmt.Errorf("bizarre")
}
