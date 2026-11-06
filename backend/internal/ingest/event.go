// Package ingest validates and normalises incoming events, writes accepted batches to the WAL and
// moves them from the WAL into ClickHouse.
package ingest

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Limits on one event. They protect ClickHouse from unbounded JSON paths and memory.
const (
	MaxBatchEvents    = 1000
	MaxTags           = 20
	MaxTagLen         = 40
	MaxIDLen          = 128
	MaxNameLen        = 128
	MaxMessageBytes   = 32 << 10
	MaxAttrBytes      = 64 << 10
	MaxAttrDepth      = 8
	MaxAttrKeys       = 256
	MaxAttrKeyLen     = 128
	MaxSourceFieldLen = 256
	// SchemaVersion of the normalised event stored in the WAL and in ClickHouse.
	SchemaVersion = 2
)

var (
	tagPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9._:/-]*$`)
	idPattern     = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)
	namePattern   = regexp.MustCompile(`^[A-Za-z0-9._:/-]+$`)
	hexPattern    = regexp.MustCompile(`^[0-9a-fA-F]*$`)
	attrKeyPatten = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)
	streamPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,62}$`)
	envPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}$`)
)

// Severity maps level names to OpenTelemetry severity numbers.
var severity = map[string]uint8{
	"TRACE": 1, "DEBUG": 5, "INFO": 9, "NOTICE": 10, "WARN": 13, "WARNING": 13,
	"ERROR": 17, "FATAL": 21, "CRITICAL": 21,
}

// canonicalLevel folds aliases so search filters see one spelling.
var canonicalLevel = map[string]string{"WARNING": "WARN", "CRITICAL": "FATAL"}

// SeverityNumber returns the number for a level name, or false.
func SeverityNumber(level string) (uint8, string, bool) {
	l := strings.ToUpper(strings.TrimSpace(level))
	n, ok := severity[l]
	if c, alias := canonicalLevel[l]; alias {
		l = c
	}
	return n, l, ok
}

// InEvent is one event as clients send it.
type InEvent struct {
	Tags        []string        `json:"tags"`
	ID          string          `json:"id"`
	Stream      string          `json:"stream"`
	Environment string          `json:"environment"`
	Timestamp   string          `json:"timestamp"`
	Level       string          `json:"level"`
	Event       string          `json:"event"`
	Message     string          `json:"message"`
	Service     string          `json:"service"`
	Host        string          `json:"host"`
	Resource    string          `json:"resource"`
	TraceID     string          `json:"trace_id"`
	SpanID      string          `json:"span_id"`
	Attributes  json.RawMessage `json:"attributes"`
}

// InBatch is the body of POST /ingest/batch.
type InBatch struct {
	Stream      string    `json:"stream"`
	Environment string    `json:"environment"`
	Events      []InEvent `json:"events"`
}

// Event is the normalised form stored in the WAL and in ClickHouse.
type Event struct {
	WorkspaceID    uuid.UUID       `json:"workspace_id"`
	ProjectID      uuid.UUID       `json:"project_id"`
	EnvironmentID  uuid.UUID       `json:"environment_id"`
	StreamID       uuid.UUID       `json:"stream_id"`
	Stream         string          `json:"-"`
	Environment    string          `json:"-"`
	Tags           []string        `json:"tags"`
	EventID        string          `json:"event_id"`
	Timestamp      time.Time       `json:"-"`
	IngestedAt     time.Time       `json:"-"`
	SeverityNumber uint8           `json:"severity_number"`
	SeverityText   string          `json:"severity_text"`
	EventName      string          `json:"event_name"`
	Message        string          `json:"message"`
	SourceService  string          `json:"source_service"`
	SourceHost     string          `json:"source_host"`
	SourceResource string          `json:"source_resource"`
	TraceID        string          `json:"trace_id"`
	SpanID         string          `json:"span_id"`
	Attributes     json.RawMessage `json:"attributes"`
	SchemaVersion  uint16          `json:"schema_version"`
}

// Rejection explains why one event of a batch was refused.
type Rejection struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// Resolver maps stream and environment names to IDs inside the token's project.
type Resolver interface {
	Stream(name string) (uuid.UUID, bool, error)
	Environment(name string) (uuid.UUID, bool, error)
}

// DefaultMaxPastAge applies when Validator.MaxPastAge is not set.
const DefaultMaxPastAge = 7 * 24 * time.Hour

// Validator checks events against the limits and the clock. MaxFutureSkew zero refuses any future
// timestamp; MaxPastAge zero means DefaultMaxPastAge.
type Validator struct {
	MaxFutureSkew time.Duration
	MaxPastAge    time.Duration
	Now           func() time.Time
}

// NewEventID returns a random event ID for events sent without one. Retries of such events
// cannot be deduplicated: clients that retry should send their own IDs.
func NewEventID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return "evt_" + hex.EncodeToString(b[:])
}

// Normalize validates in and returns the stored form. defStream and defEnv come from the batch.
// The returned error text is safe to show to the client.
func (v Validator) Normalize(in InEvent, defStream, defEnv string, res Resolver, ws, project uuid.UUID) (Event, error) {
	now := v.Now().UTC()
	e := Event{WorkspaceID: ws, ProjectID: project, IngestedAt: now, SchemaVersion: SchemaVersion}

	e.EventID = in.ID
	if e.EventID == "" {
		e.EventID = NewEventID()
	} else if len(e.EventID) > MaxIDLen || !idPattern.MatchString(e.EventID) {
		return e, fmt.Errorf("id must be 1 to %d characters from A-Z a-z 0-9 . _ : -", MaxIDLen)
	}

	stream := firstNonEmpty(in.Stream, defStream, "default")
	if !streamPattern.MatchString(stream) {
		return e, fmt.Errorf("stream %q is not a valid stream name", clip(stream))
	}
	id, ok, err := res.Stream(stream)
	if err != nil {
		return e, err
	}
	if !ok {
		return e, fmt.Errorf("stream %q does not exist in this project", stream)
	}
	e.StreamID, e.Stream = id, stream

	if env := firstNonEmpty(in.Environment, defEnv); env != "" {
		if !envPattern.MatchString(env) {
			return e, fmt.Errorf("environment %q is not a valid environment name", clip(env))
		}
		id, ok, err := res.Environment(env)
		if err != nil {
			return e, err
		}
		if !ok {
			return e, fmt.Errorf("environment %q does not exist in this project", env)
		}
		e.EnvironmentID, e.Environment = id, env
	}

	if in.Timestamp == "" {
		e.Timestamp = now
	} else {
		ts, err := time.Parse(time.RFC3339Nano, in.Timestamp)
		if err != nil {
			return e, fmt.Errorf("timestamp must be RFC 3339, for example 2026-11-02T14:30:00Z")
		}
		ts = ts.UTC().Truncate(time.Millisecond)
		if ts.After(now.Add(v.MaxFutureSkew)) {
			return e, fmt.Errorf("timestamp is more than %s in the future", v.MaxFutureSkew)
		}
		maxAge := v.MaxPastAge
		if maxAge <= 0 {
			maxAge = DefaultMaxPastAge
		}
		if ts.Before(now.Add(-maxAge)) {
			return e, fmt.Errorf("timestamp is older than %s", maxAge)
		}
		e.Timestamp = ts
	}

	level := in.Level
	if level == "" {
		level = "INFO"
	}
	n, text, ok := SeverityNumber(level)
	if !ok {
		return e, fmt.Errorf("level must be one of TRACE, DEBUG, INFO, NOTICE, WARN, ERROR, FATAL")
	}
	e.SeverityNumber, e.SeverityText = n, text

	if len(in.Event) > MaxNameLen || (in.Event != "" && !namePattern.MatchString(in.Event)) {
		return e, fmt.Errorf("event must be at most %d characters from A-Z a-z 0-9 . _ : / -", MaxNameLen)
	}
	if err := ValidateTags(in.Tags); err != nil {
		return e, err
	}
	e.Tags = append([]string{}, in.Tags...)
	e.EventName = in.Event
	if len(in.Message) > MaxMessageBytes {
		return e, fmt.Errorf("message is larger than %d bytes", MaxMessageBytes)
	}
	e.Message = in.Message
	for _, f := range []struct {
		name, val string
		dst       *string
	}{{"service", in.Service, &e.SourceService}, {"host", in.Host, &e.SourceHost}, {"resource", in.Resource, &e.SourceResource}} {
		if len(f.val) > MaxSourceFieldLen || !utf8.ValidString(f.val) {
			return e, fmt.Errorf("%s must be valid UTF-8 of at most %d bytes", f.name, MaxSourceFieldLen)
		}
		*f.dst = f.val
	}
	if len(in.TraceID) > 64 || !hexPattern.MatchString(in.TraceID) || len(in.SpanID) > 32 || !hexPattern.MatchString(in.SpanID) {
		return e, fmt.Errorf("trace_id and span_id must be hexadecimal (at most 32 and 16 bytes)")
	}
	e.TraceID, e.SpanID = strings.ToLower(in.TraceID), strings.ToLower(in.SpanID)

	attrs, err := checkAttributes(in.Attributes)
	if err != nil {
		return e, err
	}
	e.Attributes = attrs
	return e, nil
}

// checkAttributes requires a JSON object within the depth, key and size limits, and returns it
// compacted. Keys may not contain dots: ClickHouse reads dots as path separators, and search paths
// must stay unambiguous.
func checkAttributes(raw json.RawMessage) (json.RawMessage, error) {
	if len(bytes.TrimSpace(raw)) == 0 || string(bytes.TrimSpace(raw)) == "null" {
		return json.RawMessage("{}"), nil
	}
	if len(raw) > MaxAttrBytes {
		return nil, fmt.Errorf("attributes are larger than %d bytes", MaxAttrBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, fmt.Errorf("attributes must be a JSON object")
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("attributes must be a JSON object")
	}
	keys := 0
	if err := walkAttrs(obj, 1, &keys); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, raw); err != nil {
		return nil, fmt.Errorf("attributes must be a JSON object")
	}
	return buf.Bytes(), nil
}

func walkAttrs(v any, depth int, keys *int) error {
	if depth > MaxAttrDepth {
		return fmt.Errorf("attributes are nested deeper than %d levels", MaxAttrDepth)
	}
	switch t := v.(type) {
	case map[string]any:
		for k, child := range t {
			*keys++
			if *keys > MaxAttrKeys {
				return fmt.Errorf("attributes have more than %d keys", MaxAttrKeys)
			}
			if len(k) > MaxAttrKeyLen || !attrKeyPatten.MatchString(k) {
				return fmt.Errorf("attribute key %q must be 1 to %d characters from A-Z a-z 0-9 _ -", clip(k), MaxAttrKeyLen)
			}
			if err := walkAttrs(child, depth+1, keys); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range t {
			if err := walkAttrs(child, depth+1, keys); err != nil {
				return err
			}
		}
	}
	return nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func clip(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

// ---------- WAL record ----------

// walEvent is the JSON form of an event inside a WAL record; times are Unix milliseconds.
type walEvent struct {
	Event
	Stream      string `json:"stream"`
	Environment string `json:"environment,omitempty"`
	TS          int64  `json:"ts"`
	Ingested    int64  `json:"ingested"`
}

// EncodeRecord serialises accepted events into one WAL record.
func EncodeRecord(events []Event) ([]byte, error) {
	out := make([]walEvent, len(events))
	for i, e := range events {
		out[i] = walEvent{Event: e, Stream: e.Stream, Environment: e.Environment, TS: e.Timestamp.UnixMilli(), Ingested: e.IngestedAt.UnixMilli()}
	}
	return json.Marshal(out)
}

// DecodeRecord is the inverse of EncodeRecord.
func DecodeRecord(b []byte) ([]Event, error) {
	var in []walEvent
	if err := json.Unmarshal(b, &in); err != nil {
		return nil, err
	}
	out := make([]Event, len(in))
	for i, w := range in {
		e := w.Event
		if e.Tags == nil {
			e.Tags = []string{}
		}
		e.Stream, e.Environment = w.Stream, w.Environment
		e.Timestamp, e.IngestedAt = time.UnixMilli(w.TS).UTC(), time.UnixMilli(w.Ingested).UTC()
		out[i] = e
	}
	return out, nil
}

// ValidateTags checks the shared tag alphabet and limits.
func ValidateTags(tags []string) error {
	if len(tags) > MaxTags {
		return fmt.Errorf("tags must contain at most %d values", MaxTags)
	}
	for _, tag := range tags {
		if len(tag) == 0 || len(tag) > MaxTagLen || !tagPattern.MatchString(tag) {
			return fmt.Errorf("tags must be lowercase letters, digits, dot, underscore, colon, slash or dash, 1 to %d characters", MaxTagLen)
		}
	}
	return nil
}
