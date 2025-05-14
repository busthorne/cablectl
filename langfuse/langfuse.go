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
	// "github.com/google/uuid" // No longer directly used in this file by client methods
)

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

type Ingestible interface {
	EventId() string
	EventType() EventType
	EventTime() time.Time
}

type Client struct {
	API *api.Client

	ingestibles []Ingestible
	mu          sync.Mutex
	// successfullyCreatedSpanIDs map[string]bool // Removed as per user changes
	// release, environment, etc. also removed from Client struct as per user changes
}

type ClientOptions struct {
	Host       string
	PrivateKey string
	PublicKey  string

	HTTPClient *http.Client
}

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

	basicAuth := func(ctx context.Context, req *http.Request) error {
		b := []byte(opts.PublicKey + ":" + opts.PrivateKey)
		h := "Basic " + base64.StdEncoding.EncodeToString(b)
		req.Header.Set("Authorization", h)
		return nil
	}
	// User changed apiClient back to api
	api, err := api.NewClient(opts.Host,
		api.WithBaseURL(opts.Host),
		api.WithRequestEditorFn(basicAuth))
	if err != nil {
		return nil, fmt.Errorf("langfuse: %w", err)
	}

	client := &Client{
		API:         api,
		ingestibles: make([]Ingestible, 0), // Initialize ingestibles
		mu:          sync.Mutex{},          // Initialize mutex
	}
	return client, nil
}

// New lowercase batch method to add a single event to the buffer
func (c *Client) batch(event Ingestible) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ingestibles = append(c.ingestibles, event)
}

// Uppercase Batch method as reinstated by user
func (c *Client) Batch(ctx context.Context, events []Ingestible) (*api.IngestionResponse, error) {
	bodies := make([]map[string]any, len(events))
	for i := range events {
		eventType := events[i].EventType()
		// If it's a Span, client needs to decide if it's CREATE or UPDATE.
		// This logic was removed by user from Client struct (successfullyCreatedSpanIDs).
		// Reverting to use the EventType() directly from the span, which is based on EndedAt.
		// For a more robust CREATE vs UPDATE, state would need to be tracked here or passed.
		// Given current constraints, Span.EventType() is what we have.

		bodies[i] = map[string]any{
			"id":        events[i].EventId(),
			"type":      eventType, // Using event's own determination
			"timestamp": events[i].EventTime(),
			"body":      events[i],
		}
	}
	batchPayload := map[string]any{"batch": bodies} // Changed from batch to batchPayload for clarity
	var b bytes.Buffer
	if err := json.NewEncoder(&b).Encode(batchPayload); err != nil {
		return nil, fmt.Errorf("langfuse: batch encode: %w", err)
	}

	resp, err := c.API.IngestionBatchWithBody(ctx, "application/json", &b)
	if err != nil {
		return nil, fmt.Errorf("langfuse: batch ingest: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case 200, 201: // Added 201 for created
		// Potentially decode success response if needed, but often not for 200/201 batch
		return nil, nil
	case 207:
		ing := &api.IngestionResponse{}
		if err := json.NewDecoder(resp.Body).Decode(ing); err != nil {
			return nil, fmt.Errorf("langfuse: batch ingest decode (207): %w", err)
		}
		// The IngestionResponse itself can be part of the error or handled by caller
		// For now, returning it along with a generic error message.
		return ing, fmt.Errorf("langfuse: %d events failed during batch ingestion", len(ing.Errors))
	default:
		return nil, fmt.Errorf("langfuse: batch ingest failed with status: %s", resp.Status)
	}
}

// New Flush method
func (c *Client) Flush(ctx context.Context) error {
	c.mu.Lock()
	if len(c.ingestibles) == 0 {
		c.mu.Unlock()
		return nil
	}

	eventsToFlush := make([]Ingestible, len(c.ingestibles))
	copy(eventsToFlush, c.ingestibles)
	c.ingestibles = make([]Ingestible, 0) // Clear the original slice

	c.mu.Unlock() // Unlock before calling Batch, as Batch might be long-running

	// Call the uppercase Batch method
	ingestionResponse, err := c.Batch(ctx, eventsToFlush)

	if err != nil {
		// If Batch itself returns an error, we need to decide if we re-queue eventsToFlush.
		// The current Batch implementation doesn't distinguish between recoverable/non-recoverable errors for re-queuing.
		// If ingestionResponse is not nil, it means it was a 207, and Batch already returned an error for it.
		if ingestionResponse != nil && len(ingestionResponse.Errors) > 0 {
			// This is a 207 error, already has details. We need to re-queue only the failed items.
			failedEventIDs := make(map[string]bool)
			for _, anError := range ingestionResponse.Errors {
				if anError.Id != "" {
					failedEventIDs[anError.Id] = true
				}
			}

			reQueuedEvents := make([]Ingestible, 0)
			for _, event := range eventsToFlush {
				if failedEventIDs[event.EventId()] {
					reQueuedEvents = append(reQueuedEvents, event)
				}
			}
			if len(reQueuedEvents) > 0 {
				c.mu.Lock()
				c.ingestibles = append(reQueuedEvents, c.ingestibles...) // Prepend to preserve order of new items
				c.mu.Unlock()
			}
			return err // Return the original error from Batch for 207
		}
		// For other errors from Batch (e.g. network, non-207 status), re-queue all events that were attempted.
		c.mu.Lock()
		c.ingestibles = append(eventsToFlush, c.ingestibles...) // Prepend to preserve order
		c.mu.Unlock()
		return err // Return the error from Batch
	}

	// If err is nil, it means Batch was successful (200/201)
	return nil
}
