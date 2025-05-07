package cablectl

import (
	"context"
	"net/url"
	"testing"
	"time"
)

const (
	testGateway = "http://192.168.65.4:8888"
)

func TestHelloWorld(t *testing.T) {
	ctx := context.Background()

	gwu, err := url.Parse(testGateway)
	if err != nil {
		t.Fatalf("Failed to parse gateway URL: %v", err)
	}

	k := &Kernel{
		Name:      "python",
		URL:       gwu,
		KeepAlive: 30 * time.Second,
	}
	if err := New(ctx, k); err != nil {
		t.Fatalf("Failed to create kernel: %v", err)
	}

	c, err := k.Execute(ctx, `x = 42`)
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	t.Log("round", c.ExecutionCount)
	c, err = k.Execute(ctx, `print(f'Hello, {x} pirates!')`)
	if err != nil {
		t.Fatalf("Failed to execute: %v", err)
	}
	t.Log("round", c.ExecutionCount)
	t.Log(c.Text)
}
