package langfuse

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/busthorne/cablectl/langfuse/api"
	"github.com/google/uuid"
)

// EventType corresponds to various events supported by Langfuse.
//
// Events are distinct from observations: for example, a single
// observation may appear as multiple events: first, when it is
// created, and then when it is updated.
type EventType string

const (
	TRACE_CREATE       EventType = "trace-create"
	SCORE_CREATE       EventType = "score-create"
	SPAN_CREATE        EventType = "span-create"
	SPAN_UPDATE        EventType = "span-update"
	GENERATION_CREATE  EventType = "generation-create"
	GENERATION_UPDATE  EventType = "generation-update"
	EVENT_CREATE       EventType = "event-create"
	SDK_LOG            EventType = "sdk-log"
	OBSERVATION_CREATE EventType = "observation-create"
	OBSERVATION_UPDATE EventType = "observation-update"
)

// Ingestible represents an observation, or update thereof, that Langfuse will
// ingest using Batched ingestion API. The schema is a bit awkward; to make it
// more ergonomic, the observation types (Span, Generation, Event) themselves
// will determine whether they are created or updated.
//
// For example, calling End() on a span will set its EndedAt time, and ingest
// an update. Therefore, if defer sequence is client.Flush, span.End, then
// the span will first be ingested as not having endTime, then again as an
// update with endTime set, and finally flushed.
//
// https://api.reference.langfuse.com/#tag/ingestion/POST/api/public/ingestion
type Ingestible interface {
	EventId() string
	EventType() EventType
	EventTime() time.Time
}

// Client provides ergonomic methods for ingesting observations and other APIs.
type Client struct {
	API *api.Client

	ingestibles []Ingestible
	mu          sync.Mutex
}

// ClientOptions are the determining the API line for this package.
//
// The client will inherit the provided HTTP client, or otherwise use
// sane defaults for transport, dialer, and timeouts. If you wish to
// control these variables, you absolutely should provide your own
// client.
type ClientOptions struct {
	Host       string
	PrivateKey string
	PublicKey  string

	HTTPClient *http.Client
}

// New creates a client from code-generated API client implementation.
//
// Note: the `API` field takes care of Basic Auth.
func New(opts *ClientOptions) (*Client, error) {
	const (
		cloudHost = "https://cloud.langfuse.com"
		keepAlive = 30 * time.Second
	)

	if opts.Host == "" {
		opts.Host = cloudHost
	}
	if opts.PublicKey == "" {
		return nil, errors.New("langfuse: public key is required")
	}
	if opts.PrivateKey == "" {
		return nil, errors.New("langfuse: private key is required")
	}

	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{
				ForceAttemptHTTP2:     true,
				Proxy:                 http.ProxyFromEnvironment,
				MaxIdleConns:          100,
				MaxIdleConnsPerHost:   100,
				MaxConnsPerHost:       0,
				DisableKeepAlives:     false,
				DisableCompression:    false,
				IdleConnTimeout:       3 * keepAlive,
				TLSHandshakeTimeout:   keepAlive,
				ExpectContinueTimeout: 1 * time.Second,
				ResponseHeaderTimeout: 30 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   keepAlive,
					KeepAlive: keepAlive,
				}).DialContext,
			},
			Timeout: keepAlive,
		}
	}

	b := []byte(opts.PublicKey + ":" + opts.PrivateKey)
	h := "Basic " + base64.StdEncoding.EncodeToString(b)
	basicAuth := func(ctx context.Context, req *http.Request) error {
		req.Header.Set("Authorization", h)
		return nil
	}
	api, err := api.NewClient(opts.Host,
		api.WithBaseURL(opts.Host),
		api.WithRequestEditorFn(basicAuth))
	if err != nil {
		return nil, fmt.Errorf("langfuse: %w", err)
	}

	client := &Client{
		API:         api,
		ingestibles: make([]Ingestible, 0, 64),
		mu:          sync.Mutex{},
	}
	return client, nil
}

// Ingest adds an ingestible to the client's buffer, after populating it.
func (c *Client) Ingest(event Ingestible) {
	c.Populate(event)
	c.mu.Lock()
	c.ingestibles = append(c.ingestibles, event)
	c.mu.Unlock()
}

// Populate populates the traces and spans with the client.
//
// You will typically use this on spans after unmarshalling them from
// the database metadata column, or for inputs that didn't originate
// from the client directly.
func (c *Client) Populate(event Ingestible) {
	switch e := event.(type) {
	case *Trace:
		e.client = c
	case *Span:
		e.client = c
	}
}

// Batch submits a series of ingestibles to the upstream API.
func (c *Client) Batch(ctx context.Context, events []Ingestible) error {
	bodies := make([]map[string]any, len(events))
	for i := range events {
		bodies[i] = map[string]any{
			"id":        events[i].EventId(),
			"type":      events[i].EventType(),
			"timestamp": events[i].EventTime(),
			"body":      events[i],
		}
	}

	var b bytes.Buffer
	err := json.NewEncoder(&b).Encode(map[string]any{"batch": bodies})
	if err != nil {
		return fmt.Errorf("langfuse: batch encode: %w", err)
	}

	resp, err := c.API.IngestionBatchWithBody(ctx, "application/json", &b)
	if err != nil {
		return fmt.Errorf("langfuse: batch ingest: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200, 201:
		return nil
	case 207:
		var ing api.IngestionResponse
		if err := json.NewDecoder(resp.Body).Decode(&ing); err != nil {
			return fmt.Errorf("langfuse: batch ingest decode: %w", err)
		}
		if len(ing.Errors) > 0 {
			return &BatchError{Errors: ing.Errors}
		}
		return nil
	default:
		return fmt.Errorf("langfuse: batch ingest failed with status: %s", resp.Status)
	}
}

// Trace populates the provided trace, ingests and returns it.
func (c *Client) Trace(t *Trace) *Trace {
	t.client = c

	if t.Id == "" {
		t.Id = uuid.New().String()
	}
	if t.Timestamp.IsZero() {
		t.Timestamp = time.Now().UTC()
	}
	c.Ingest(t)
	return t
}

// Flush will batch all ingestibles remaining in the client's buffer.
//
// You would typically `defer client.Flush()`.
func (c *Client) Flush(ctx context.Context) error {
	if len(c.ingestibles) == 0 {
		return nil
	}

	c.mu.Lock()
	eventsToFlush := make([]Ingestible, len(c.ingestibles))
	copy(eventsToFlush, c.ingestibles)
	c.ingestibles = make([]Ingestible, 0, len(eventsToFlush))
	c.mu.Unlock()

	switch err := c.Batch(ctx, eventsToFlush); {
	case err == nil:
		return nil
	case errors.Is(err, ErrBatchFailed):
		return err
	default:
		c.mu.Lock()
		c.ingestibles = append(eventsToFlush, c.ingestibles...) // preserve order
		c.mu.Unlock()
		return err
	}
}
