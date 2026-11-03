package api

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	logspb "go.opentelemetry.io/proto/otlp/logs/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/Ervisio/ErvisioLogs/backend/internal/httpx"
	"github.com/Ervisio/ErvisioLogs/backend/internal/ingest"
)

// OTLPRoutes mounts the OTLP/HTTP logs receiver at /v1/logs (ADR 0010). It is outside /api/v1
// because OTLP exporters append /v1/logs to the configured endpoint.
//
// Status: implemented against the OTLP 1.x specification and not yet validated with a real
// OpenTelemetry exporter or Collector. Do not advertise OTLP support until it is.
func (a *API) OTLPRoutes(r chi.Router) {
	r.With(a.authenticate).Post("/v1/logs", a.otlpLogs)
}

// otlpKV and otlpRecord are the neutral form both encodings decode into.
type otlpKV struct {
	key string
	val any
}

type otlpRecord struct {
	timeNano, observedNano uint64
	severity               int32
	severityText           string
	body                   any
	attrs                  []otlpKV
	traceID, spanID        string
	eventName              string
}

type otlpScope struct {
	name, version string
	records       []otlpRecord
}

type otlpResource struct {
	attrs  []otlpKV
	scopes []otlpScope
}

func (a *API) otlpLogs(w http.ResponseWriter, r *http.Request) {
	t, ok := a.ingestToken(w, r)
	if !ok {
		return
	}
	ct, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if ct != "application/x-protobuf" && ct != "application/json" {
		httpx.WriteProblem(w, r, http.StatusUnsupportedMediaType, "Content-Type must be application/x-protobuf or application/json")
		return
	}
	b, ok := readIngestBody(w, r)
	if !ok {
		return
	}
	var resources []otlpResource
	var err error
	if ct == "application/x-protobuf" {
		resources, err = decodeOTLPProto(b)
	} else {
		resources, err = decodeOTLPJSON(b)
	}
	if err != nil {
		httpx.WriteProblem(w, r, http.StatusBadRequest, "malformed OTLP body: "+err.Error())
		return
	}

	defStream, defEnv := r.Header.Get("X-Ervisio-Stream"), r.Header.Get("X-Ervisio-Environment")
	var in []ingest.InEvent
	for _, res := range resources {
		for _, sc := range res.scopes {
			for _, rec := range sc.records {
				in = append(in, otlpEvent(res, sc, rec))
			}
		}
	}
	if len(in) > ingest.MaxBatchEvents {
		httpx.WriteProblem(w, r, http.StatusRequestEntityTooLarge,
			"at most "+strconv.Itoa(ingest.MaxBatchEvents)+" log records per request: lower the exporter batch size")
		return
	}
	var rejected int64
	var firstErr string
	if len(in) > 0 {
		res, f := a.queueEvents(r, t, in, defStream, defEnv)
		if f != nil {
			f.write(w, r)
			return
		}
		rejected = int64(res.Rejected)
		if len(res.Errors) > 0 {
			firstErr = fmt.Sprintf("record %d: %s", res.Errors[0].Index, res.Errors[0].Reason)
			if len(res.Errors) > 1 {
				firstErr += fmt.Sprintf(" (and %d more)", len(res.Errors)-1)
			}
		}
	}

	var out []byte
	if ct == "application/x-protobuf" {
		out = otlpResponseProto(rejected, firstErr)
	} else {
		out = otlpResponseJSON(rejected, firstErr)
	}
	w.Header().Set("Content-Type", ct)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(out)
}

// otlpEvent maps one log record. Resource attributes service.name and host.name fill the
// source columns; the rest of the resource and the scope go under attributes.resource and
// attributes.scope. Dotted keys become nested objects.
func otlpEvent(res otlpResource, sc otlpScope, rec otlpRecord) ingest.InEvent {
	ev := ingest.InEvent{TraceID: rec.traceID, SpanID: rec.spanID}
	switch {
	case rec.timeNano > 0:
		ev.Timestamp = nanoTime(rec.timeNano)
	case rec.observedNano > 0:
		ev.Timestamp = nanoTime(rec.observedNano)
	}
	ev.Level = otlpLevel(rec.severity, rec.severityText)
	ev.Message = clipBytes(bodyText(rec.body), ingest.MaxMessageBytes)

	attrs := map[string]any{}
	resAttrs := map[string]any{}
	for _, kv := range res.attrs {
		switch kv.key {
		case "service.name":
			ev.Service = clipBytes(fmt.Sprint(kv.val), ingest.MaxSourceFieldLen)
		case "host.name":
			ev.Host = clipBytes(fmt.Sprint(kv.val), ingest.MaxSourceFieldLen)
		case "ervisio.stream":
			ev.Stream = fmt.Sprint(kv.val)
		case "ervisio.environment":
			ev.Environment = fmt.Sprint(kv.val)
		default:
			putAttr(resAttrs, kv.key, kv.val)
		}
	}
	event := rec.eventName
	for _, kv := range rec.attrs {
		switch kv.key {
		case "ervisio.stream":
			ev.Stream = fmt.Sprint(kv.val)
		case "ervisio.environment":
			ev.Environment = fmt.Sprint(kv.val)
		case "event.name":
			if event == "" {
				event = fmt.Sprint(kv.val)
			}
		default:
			putAttr(attrs, kv.key, kv.val)
		}
	}
	if len(event) <= ingest.MaxNameLen && eventNameOK(event) {
		ev.Event = event
	} else {
		attrs["event_name"] = event // kept, though it cannot be the indexed event name
	}
	if len(resAttrs) > 0 {
		putAttr(attrs, "resource", resAttrs)
	}
	if sc.name != "" || sc.version != "" {
		scope := map[string]any{}
		if sc.name != "" {
			scope["name"] = sc.name
		}
		if sc.version != "" {
			scope["version"] = sc.version
		}
		putAttr(attrs, "scope", scope)
	}
	if len(attrs) > 0 {
		ev.Attributes, _ = json.Marshal(attrs)
	}
	return ev
}

func nanoTime(n uint64) string {
	if n > math.MaxInt64 {
		n = math.MaxInt64
	}
	return time.Unix(0, int64(n)).UTC().Format(time.RFC3339Nano)
}

// otlpLevel maps an OTLP severity number to a level; with no number it tries the text.
func otlpLevel(n int32, text string) string {
	switch {
	case n >= 1 && n <= 4:
		return "TRACE"
	case n >= 5 && n <= 8:
		return "DEBUG"
	case n >= 9 && n <= 12:
		return "INFO"
	case n >= 13 && n <= 16:
		return "WARN"
	case n >= 17 && n <= 20:
		return "ERROR"
	case n >= 21 && n <= 24:
		return "FATAL"
	}
	if _, t, ok := ingest.SeverityNumber(strings.ToUpper(strings.TrimSpace(text))); ok {
		return t
	}
	return "INFO"
}

func eventNameOK(s string) bool {
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("._:/-", c)) {
			return false
		}
	}
	return true
}

func bodyText(v any) string {
	switch b := v.(type) {
	case nil:
		return ""
	case string:
		return b
	default:
		out, _ := json.Marshal(b)
		return string(out)
	}
}

func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for len(s) > 0 && !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// attrKey makes a key segment storable: characters outside A-Z a-z 0-9 _ - become _.
func attrKey(s string) string {
	if s == "" {
		return "_"
	}
	b := []byte(s)
	for i, c := range b {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			b[i] = '_'
		}
	}
	if len(b) > ingest.MaxAttrKeyLen {
		b = b[:ingest.MaxAttrKeyLen]
	}
	return string(b)
}

// putAttr stores val at the dotted key, nesting on dots. Deep keys keep their tail flattened
// with _ so the depth limit holds; a key that collides with a scalar is stored flattened.
func putAttr(m map[string]any, key string, val any) {
	parts := strings.Split(key, ".")
	const maxNest = ingest.MaxAttrDepth - 3 // room for resource/scope wrappers and nested values
	if len(parts) > maxNest {
		parts = append(parts[:maxNest-1], strings.Join(parts[maxNest-1:], "_"))
	}
	cur := m
	for i, p := range parts {
		k := attrKey(p)
		if i == len(parts)-1 {
			if existing, ok := cur[k].(map[string]any); ok {
				if vm, ok := val.(map[string]any); ok {
					for kk, vv := range vm {
						existing[kk] = vv
					}
					return
				}
				existing["_value"] = val
				return
			}
			cur[k] = val
			return
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			if _, taken := cur[k]; taken {
				cur[attrKey(strings.Join(parts[i:], "_"))] = val
				return
			}
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
}

// ---------- protobuf ----------
//
// ExportLogsServiceRequest and LogsData share field 1 (repeated ResourceLogs), so the request
// decodes as LogsData; this keeps the gRPC collector packages out of the build.

// otlpResponseProto encodes ExportLogsServiceResponse{partial_success = 1 {
// rejected_log_records = 1, error_message = 2 }}. Full success is the empty message.
func otlpResponseProto(rejected int64, msg string) []byte {
	if rejected == 0 {
		return []byte{}
	}
	var ps []byte
	ps = protowire.AppendTag(ps, 1, protowire.VarintType)
	ps = protowire.AppendVarint(ps, uint64(rejected))
	if msg != "" {
		ps = protowire.AppendTag(ps, 2, protowire.BytesType)
		ps = protowire.AppendString(ps, msg)
	}
	out := protowire.AppendTag(nil, 1, protowire.BytesType)
	return protowire.AppendBytes(out, ps)
}

func otlpResponseJSON(rejected int64, msg string) []byte {
	if rejected == 0 {
		return []byte("{}")
	}
	type partial struct {
		RejectedLogRecords string `json:"rejectedLogRecords"`
		ErrorMessage       string `json:"errorMessage,omitempty"`
	}
	b, _ := json.Marshal(map[string]partial{"partialSuccess": {strconv.FormatInt(rejected, 10), msg}})
	return b
}

func decodeOTLPProto(b []byte) ([]otlpResource, error) {
	var req logspb.LogsData
	if err := proto.Unmarshal(b, &req); err != nil {
		return nil, err
	}
	out := make([]otlpResource, 0, len(req.GetResourceLogs()))
	for _, rl := range req.GetResourceLogs() {
		res := otlpResource{attrs: pbKVs(rl.GetResource().GetAttributes())}
		for _, sl := range rl.GetScopeLogs() {
			sc := otlpScope{name: sl.GetScope().GetName(), version: sl.GetScope().GetVersion()}
			for _, lr := range sl.GetLogRecords() {
				sc.records = append(sc.records, otlpRecord{
					timeNano: lr.GetTimeUnixNano(), observedNano: lr.GetObservedTimeUnixNano(),
					severity: int32(lr.GetSeverityNumber()), severityText: lr.GetSeverityText(),
					body: pbAny(lr.GetBody(), 0), attrs: pbKVs(lr.GetAttributes()),
					traceID: hexID(lr.GetTraceId()), spanID: hexID(lr.GetSpanId()), eventName: lr.GetEventName(),
				})
			}
			res.scopes = append(res.scopes, sc)
		}
		out = append(out, res)
	}
	return out, nil
}

func hexID(b []byte) string {
	for _, c := range b {
		if c != 0 {
			return hex.EncodeToString(b)
		}
	}
	return "" // absent or all zero: no trace context
}

func pbKVs(kvs []*commonpb.KeyValue) []otlpKV {
	out := make([]otlpKV, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, otlpKV{key: kv.GetKey(), val: pbAny(kv.GetValue(), 0)})
	}
	return out
}

func pbAny(v *commonpb.AnyValue, depth int) any {
	if v == nil || depth > ingest.MaxAttrDepth {
		return nil
	}
	switch x := v.GetValue().(type) {
	case *commonpb.AnyValue_StringValue:
		return x.StringValue
	case *commonpb.AnyValue_BoolValue:
		return x.BoolValue
	case *commonpb.AnyValue_IntValue:
		return x.IntValue
	case *commonpb.AnyValue_DoubleValue:
		return finite(x.DoubleValue)
	case *commonpb.AnyValue_BytesValue:
		return base64.StdEncoding.EncodeToString(x.BytesValue)
	case *commonpb.AnyValue_ArrayValue:
		arr := make([]any, 0, len(x.ArrayValue.GetValues()))
		for _, e := range x.ArrayValue.GetValues() {
			arr = append(arr, pbAny(e, depth+1))
		}
		return arr
	case *commonpb.AnyValue_KvlistValue:
		m := map[string]any{}
		for _, kv := range x.KvlistValue.GetValues() {
			m[attrKey(kv.GetKey())] = pbAny(kv.GetValue(), depth+1)
		}
		return m
	}
	return nil
}

// finite keeps NaN and infinities, which JSON cannot hold, as strings.
func finite(f float64) any {
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return strconv.FormatFloat(f, 'g', -1, 64)
	}
	return f
}

// ---------- JSON ----------
//
// OTLP/JSON differs from protojson: trace and span ids are hex, not base64, and enums are
// integers. Unknown fields are ignored, as the specification requires of receivers.

type jsonLogsReq struct {
	ResourceLogs []struct {
		Resource struct {
			Attributes []jsonKV `json:"attributes"`
		} `json:"resource"`
		ScopeLogs []struct {
			Scope struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"scope"`
			LogRecords []struct {
				TimeUnixNano         jsonUint `json:"timeUnixNano"`
				ObservedTimeUnixNano jsonUint `json:"observedTimeUnixNano"`
				SeverityNumber       int32    `json:"severityNumber"`
				SeverityText         string   `json:"severityText"`
				Body                 *jsonAny `json:"body"`
				Attributes           []jsonKV `json:"attributes"`
				TraceID              string   `json:"traceId"`
				SpanID               string   `json:"spanId"`
				EventName            string   `json:"eventName"`
			} `json:"logRecords"`
		} `json:"scopeLogs"`
	} `json:"resourceLogs"`
}

type jsonKV struct {
	Key   string   `json:"key"`
	Value *jsonAny `json:"value"`
}

type jsonAny struct {
	StringValue *string  `json:"stringValue"`
	BoolValue   *bool    `json:"boolValue"`
	IntValue    *jsonInt `json:"intValue"`
	DoubleValue *float64 `json:"doubleValue"`
	BytesValue  *string  `json:"bytesValue"`
	ArrayValue  *struct {
		Values []*jsonAny `json:"values"`
	} `json:"arrayValue"`
	KvlistValue *struct {
		Values []jsonKV `json:"values"`
	} `json:"kvlistValue"`
}

// jsonUint and jsonInt accept 64-bit integers as JSON numbers or strings.
type jsonUint uint64
type jsonInt int64

func (u *jsonUint) UnmarshalJSON(b []byte) error {
	n, err := strconv.ParseUint(strings.Trim(string(b), `"`), 10, 64)
	if err != nil && string(b) != "null" {
		return fmt.Errorf("invalid unsigned integer %s", clipBytes(string(b), 32))
	}
	*u = jsonUint(n)
	return nil
}

func (i *jsonInt) UnmarshalJSON(b []byte) error {
	n, err := strconv.ParseInt(strings.Trim(string(b), `"`), 10, 64)
	if err != nil && string(b) != "null" {
		return fmt.Errorf("invalid integer %s", clipBytes(string(b), 32))
	}
	*i = jsonInt(n)
	return nil
}

func decodeOTLPJSON(b []byte) ([]otlpResource, error) {
	var req jsonLogsReq
	if err := json.Unmarshal(b, &req); err != nil {
		return nil, err
	}
	out := make([]otlpResource, 0, len(req.ResourceLogs))
	for _, rl := range req.ResourceLogs {
		res := otlpResource{attrs: jsonKVs(rl.Resource.Attributes)}
		for _, sl := range rl.ScopeLogs {
			sc := otlpScope{name: sl.Scope.Name, version: sl.Scope.Version}
			for _, lr := range sl.LogRecords {
				sc.records = append(sc.records, otlpRecord{
					timeNano: uint64(lr.TimeUnixNano), observedNano: uint64(lr.ObservedTimeUnixNano),
					severity: lr.SeverityNumber, severityText: lr.SeverityText,
					body: jsonAnyVal(lr.Body, 0), attrs: jsonKVs(lr.Attributes),
					traceID: zeroHex(lr.TraceID), spanID: zeroHex(lr.SpanID), eventName: lr.EventName,
				})
			}
			res.scopes = append(res.scopes, sc)
		}
		out = append(out, res)
	}
	return out, nil
}

func zeroHex(s string) string {
	if strings.Trim(s, "0") == "" {
		return ""
	}
	return s // Normalize rejects anything that is not hex
}

func jsonKVs(kvs []jsonKV) []otlpKV {
	out := make([]otlpKV, 0, len(kvs))
	for _, kv := range kvs {
		out = append(out, otlpKV{key: kv.Key, val: jsonAnyVal(kv.Value, 0)})
	}
	return out
}

func jsonAnyVal(v *jsonAny, depth int) any {
	if v == nil || depth > ingest.MaxAttrDepth {
		return nil
	}
	switch {
	case v.StringValue != nil:
		return *v.StringValue
	case v.BoolValue != nil:
		return *v.BoolValue
	case v.IntValue != nil:
		return int64(*v.IntValue)
	case v.DoubleValue != nil:
		return finite(*v.DoubleValue)
	case v.BytesValue != nil:
		return *v.BytesValue // already base64 in OTLP/JSON
	case v.ArrayValue != nil:
		arr := make([]any, 0, len(v.ArrayValue.Values))
		for _, e := range v.ArrayValue.Values {
			arr = append(arr, jsonAnyVal(e, depth+1))
		}
		return arr
	case v.KvlistValue != nil:
		m := map[string]any{}
		for _, kv := range v.KvlistValue.Values {
			m[attrKey(kv.Key)] = jsonAnyVal(kv.Value, depth+1)
		}
		return m
	}
	return nil
}
