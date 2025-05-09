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
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// New attaches a websocket connection to a new, or existing, kernel.
func New(ctx context.Context, k *Kernel) error {
	switch {
	case k.Name == "":
		return fmt.Errorf("kernel name is required")
	case k.Client == nil:
		if k.URL.String() == "" {
			return fmt.Errorf("kernel gateway url is required")
		}
		gw, err := gateway.NewClient(k.URL.String())
		if err != nil {
			return fmt.Errorf("failed to create gateway client: %w", err)
		}
		k.Client = gw
	}

	if k.ID == uuid.Nil {
		resp, err := k.Client.PostApiKernels(ctx, gateway.PostApiKernelsJSONRequestBody{
			Name: &k.Name,
		})
		if err != nil {
			return fmt.Errorf("failed to create kernel: %w", err)
		}
		var kl gateway.Kernel
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
