package langfuse

import (
	"time"

	"github.com/google/uuid"
)

type Trace struct {
	Id          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	SessionId   string    `json:"sessionId,omitempty"`
	UserId      string    `json:"userId,omitempty"`
	Environment string    `json:"environment,omitempty"`
	Release     string    `json:"release,omitempty"`
	Version     string    `json:"version,omitempty"`
	Tags        []string  `json:"tags,omitempty"`
	Input       any       `json:"input,omitempty"`
	Metadata    any       `json:"metadata,omitempty"`
	Output      any       `json:"output,omitempty"`
	Public      bool      `json:"public,omitempty"`
	Timestamp   time.Time `json:"timestamp"`

	client *Client `json:"-"`
}

func (t *Trace) EventId() string      { return t.Id.String() }
func (t *Trace) EventType() EventType { return TRACE_CREATE }
func (t *Trace) EventTime() time.Time { return t.Timestamp }

func (t *Trace) Span(s *Span) *Span {
	if s == nil {
		panic("langfuse: Span cannot be nil when calling Trace.Span")
	}
	if t.client == nil {
		panic("langfuse: Trace must be associated with a client before creating a Span")
	}

	s.client = t.client
	s.TraceId = t.Id.String()
	if s.Id == uuid.Nil {
		s.Id = uuid.New()
	}
	if s.StartedAt.IsZero() {
		s.StartedAt = time.Now().UTC()
	}
	if s.Environment == "" && t.Environment != "" {
		s.Environment = t.Environment
	}
	if s.Version == "" && t.Version != "" {
		s.Version = t.Version
	}

	eventCopy := *s
	t.client.batch(&eventCopy)
	return s
}

func (t *Trace) Event(e *Event) *Event {
	if e == nil {
		panic("langfuse: Event cannot be nil when calling Trace.Event")
	}
	if t.client == nil {
		panic("langfuse: Trace must be associated with a client before creating an Event")
	}

	e.client = t.client
	e.TraceId = t.Id.String()
	if e.Id == uuid.Nil {
		e.Id = uuid.New()
	}
	if e.StartTime.IsZero() {
		e.StartTime = time.Now().UTC()
	}
	if e.Environment == "" && t.Environment != "" {
		e.Environment = t.Environment
	}
	if e.Version == "" && t.Version != "" {
		e.Version = t.Version
	}

	eventCopy := *e
	t.client.batch(&eventCopy)
	return e
}

type Span struct {
	Id                  uuid.UUID  `json:"id"`
	TraceId             string     `json:"traceId,omitempty"`
	Name                string     `json:"name,omitempty"`
	StartedAt           time.Time  `json:"startTime"`
	EndedAt             *time.Time `json:"endTime,omitempty"`
	Metadata            any        `json:"metadata,omitempty"`
	Input               any        `json:"input,omitempty"`
	Output              any        `json:"output,omitempty"`
	Level               string     `json:"level,omitempty"`
	StatusMessage       string     `json:"statusMessage,omitempty"`
	ParentObservationId string     `json:"parentObservationId,omitempty"`
	Version             string     `json:"version,omitempty"`
	Environment         string     `json:"environment,omitempty"`

	client *Client `json:"-"`
}

func (s *Span) EventId() string { return s.Id.String() }
func (s *Span) EventType() EventType {
	if s.EndedAt == nil {
		return SPAN_CREATE
	}
	return SPAN_UPDATE // If EndedAt is set, it's an update type
}
func (s *Span) EventTime() time.Time { return s.StartedAt }

func (s *Span) End() {
	if s.EndedAt == nil {
		now := time.Now().UTC()
		s.EndedAt = &now
	}
}

func (s *Span) Generation(g *Generation) *Generation {
	if g == nil {
		panic("langfuse: Generation cannot be nil when calling Span.Generation")
	}
	if s.client == nil {
		panic("langfuse: Span must be associated with a client before creating a Generation")
	}

	g.client = s.client
	g.TraceId = s.TraceId
	g.ParentObservationId = s.Id.String()
	if g.Id == uuid.Nil {
		g.Id = uuid.New()
	}
	if g.StartedAt.IsZero() {
		g.StartedAt = time.Now().UTC()
	}
	if g.Environment == "" && s.Environment != "" {
		g.Environment = s.Environment
	}
	if g.Version == "" && s.Version != "" {
		g.Version = s.Version
	}

	eventCopy := *g
	s.client.batch(&eventCopy)
	return g
}

func (s *Span) Span(childSpan *Span) *Span {
	if childSpan == nil {
		panic("langfuse: child Span cannot be nil when calling Span.Span")
	}
	if s.client == nil {
		panic("langfuse: Span must be associated with a client before creating a child Span")
	}

	childSpan.client = s.client
	childSpan.TraceId = s.TraceId
	childSpan.ParentObservationId = s.Id.String()
	if childSpan.Id == uuid.Nil {
		childSpan.Id = uuid.New()
	}
	if childSpan.StartedAt.IsZero() {
		childSpan.StartedAt = time.Now().UTC()
	}
	if childSpan.Environment == "" && s.Environment != "" {
		childSpan.Environment = s.Environment
	}
	if childSpan.Version == "" && s.Version != "" {
		childSpan.Version = s.Version
	}

	eventCopy := *childSpan
	s.client.batch(&eventCopy)
	return childSpan
}

func (s *Span) Event(e *Event) *Event {
	if e == nil {
		panic("langfuse: Event cannot be nil when calling Span.Event")
	}
	if s.client == nil {
		panic("langfuse: Span must be associated with a client before creating an Event")
	}

	e.client = s.client
	e.TraceId = s.TraceId
	e.ParentObservationId = s.Id.String()
	if e.Id == uuid.Nil {
		e.Id = uuid.New()
	}
	if e.StartTime.IsZero() {
		e.StartTime = time.Now().UTC()
	}
	if e.Environment == "" && s.Environment != "" {
		e.Environment = s.Environment
	}
	if e.Version == "" && s.Version != "" {
		e.Version = s.Version
	}

	eventCopy := *e
	s.client.batch(&eventCopy)
	return e
}

type Generation struct {
	Id                  uuid.UUID          `json:"id"`
	TraceId             string             `json:"traceId,omitempty"`
	Name                string             `json:"name,omitempty"`
	StartedAt           time.Time          `json:"startTime"`
	EndedAt             *time.Time         `json:"endTime,omitempty"`
	CompletionAt        *time.Time         `json:"completionStartTime,omitempty"`
	Model               string             `json:"model,omitempty"`
	ModelParameters     map[string]any     `json:"modelParameters,omitempty"`
	Usage               any                `json:"usage,omitempty"`
	UsageDetails        any                `json:"usageDetails,omitempty"`
	CostDetails         map[string]float64 `json:"costDetails,omitempty"`
	PromptName          string             `json:"promptName,omitempty"`
	PromptVersion       *int               `json:"promptVersion,omitempty"`
	Metadata            any                `json:"metadata,omitempty"`
	Input               any                `json:"input,omitempty"`
	Output              any                `json:"output,omitempty"`
	Level               string             `json:"level,omitempty"`
	StatusMessage       string             `json:"statusMessage,omitempty"`
	ParentObservationId string             `json:"parentObservationId,omitempty"`
	Version             string             `json:"version,omitempty"`
	Environment         string             `json:"environment,omitempty"`

	client *Client `json:"-"`
}

func (g *Generation) EventId() string      { return g.Id.String() }
func (g *Generation) EventType() EventType { return GENERATION_CREATE }
func (g *Generation) EventTime() time.Time { return g.StartedAt }

func (g *Generation) End() {
	now := time.Now().UTC()
	if g.EndedAt == nil {
		g.EndedAt = &now
	}
	if g.CompletionAt == nil {
		g.CompletionAt = &g.StartedAt
	}
}

type Event struct {
	Id                  uuid.UUID `json:"id"`
	TraceId             string    `json:"traceId,omitempty"`
	Name                string    `json:"name,omitempty"`
	StartTime           time.Time `json:"startTime"`
	Metadata            any       `json:"metadata,omitempty"`
	Input               any       `json:"input,omitempty"`
	Output              any       `json:"output,omitempty"`
	Level               string    `json:"level,omitempty"`
	StatusMessage       string    `json:"statusMessage,omitempty"`
	ParentObservationId string    `json:"parentObservationId,omitempty"`
	Version             string    `json:"version,omitempty"`
	Environment         string    `json:"environment,omitempty"`

	client *Client `json:"-"`
}

func (e *Event) EventId() string      { return e.Id.String() }
func (e *Event) EventType() EventType { return EVENT_CREATE }
func (e *Event) EventTime() time.Time { return e.StartTime }
