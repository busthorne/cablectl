package gateway

import (
	"context"
	"net/url"
	"os"
	"testing"
	"time"
)

func TestHelloWorld(t *testing.T) {
	ctx := context.Background()

	gatewayURL, err := url.Parse(os.Getenv("GATEWAY_URL"))
	if err != nil {
		t.Fatalf("Failed to parse gateway URL: %v", err)
	}

	k := &Kernel{
		Name:      os.Getenv("GATEWAY_KERNEL"),
		URL:       gatewayURL,
		KeepAlive: 30 * time.Second,
	}
	if k.Name == "" {
		k.Name = "python3"
	}
	if err := NewKernel(ctx, k); err != nil {
		t.Fatalf("Failed to create kernel: %v", err)
	}

	var c *Content

	ch, err := k.Execute(ctx, `x = 42`)
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	for c = range ch {
		continue
	}
	t.Log("round", c.ExecutionCount)
	ch, err = k.Execute(ctx, `print(f'Hello, {x} pirates!')`)
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	for c = range ch {
		if c.Text != "" {
			t.Log(c.Text)
		}
	}
	t.Log("round", c.ExecutionCount)

	if err := k.Shutdown(ctx); err != nil {
		t.Fatalf("Failed to shutdown: %v", err)
	}
	t.Log("Kernel shutdown.")
}
