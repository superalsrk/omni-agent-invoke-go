// Package omni implements the public Octo Runtime Kernel IPC protocol in Go.
package omni

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"time"
)

const maxLine = 42_008_576
const maxRequest = 33_554_432

type Context map[string]any
type Input map[string]any
type Frame map[string]any

// ImageInput builds the complete protocol metadata without reading files or URLs.
func ImageInput(data []byte, mimeType string) (map[string]any, error) {
	if (mimeType != "image/png" && mimeType != "image/jpeg" && mimeType != "image/webp") || len(data) == 0 || len(data) > 2097152 {
		return nil, failure("INVALID_IMAGE")
	}
	digest := sha256.Sum256(data)
	return map[string]any{"type": "image", "mimeType": mimeType, "data": base64.StdEncoding.EncodeToString(data), "byteSize": len(data), "sha256": hex.EncodeToString(digest[:])}, nil
}

type Error struct {
	Code   string
	Data   any
	Remote bool
}

func (e *Error) Error() string  { return e.Code }
func failure(code string) error { return &Error{Code: code} }
func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return "CONNECTION_CLOSED"
}
func retryable(err error) bool {
	switch code(err) {
	case "CONNECTION_CLOSED", "KERNEL_UNAVAILABLE", "KERNEL_TIMEOUT":
		return true
	}
	return false
}
func id() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b[:])
}
func decode(data []byte, value any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if err := d.Decode(value); err != nil {
		return failure("INVALID_RESPONSE")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return failure("INVALID_RESPONSE")
	}
	return nil
}
func value[T any](raw json.RawMessage) (T, error) {
	var result T
	err := decode(raw, &result)
	return result, err
}
func number(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, err := strconv.ParseInt(string(n), 10, 64)
		if err != nil {
			return -1
		}
		return i
	case int:
		return int64(n)
	case int64:
		return n
	}
	return -1
}

func (r RunRef) Key() map[string]any {
	return map[string]any{"run_id": r.RunID, "invocation_id": r.InvocationID, "agent_session_id": r.AgentSessionID, "session_generation": r.SessionGeneration, "protocol_mode": r.ProtocolMode}
}
func (r RunRef) valid() bool {
	return r.KernelInstanceID != "" && r.RunID != "" && r.InvocationID != "" && r.StreamID != "" && r.AgentSessionID != "" && r.Protocol != "" && r.SessionGeneration >= 0 && r.SessionGeneration <= 9007199254740991 && (r.ProtocolMode == Canonical || r.ProtocolMode == Native)
}

type Snapshot struct {
	Ref       RunRef `json:"ref"`
	OldestSeq int64  `json:"oldest_seq"`
	LatestSeq int64  `json:"latest_seq"`
	Terminal  Frame  `json:"terminal,omitempty"`
}

func sameRef(a, b RunRef) error {
	if a.KernelInstanceID != b.KernelInstanceID {
		return failure("KERNEL_INSTANCE_CHANGED")
	}
	if a != b {
		return failure("RUN_IDENTITY_MISMATCH")
	}
	return nil
}
func (f Frame) TerminalState() string {
	control, _ := f["control"].(map[string]any)
	if f["kind"] == "control" && control["type"] == "state" {
		s, _ := control["state"].(string)
		switch s {
		case "completed", "failed", "cancelled", "lost":
			return s
		}
	}
	return ""
}
func frameIdentity(f Frame, r RunRef) error {
	b, _ := json.Marshal(f)
	var actual RunRef
	if decode(b, &actual) != nil {
		return failure("HOST_FRAME_INVALID")
	}
	actual.KernelInstanceID = r.KernelInstanceID
	if err := sameRef(actual, r); err != nil {
		return err
	}
	if n := number(f["seq"]); n < 1 || n > 9007199254740991 {
		return failure("STREAM_GAP")
	}
	key := ""
	switch f["kind"] {
	case "control":
		key = "control"
	case "canonical":
		if r.ProtocolMode != Canonical {
			return failure("HOST_FRAME_INVALID")
		}
		key = "event"
	case "native":
		if r.ProtocolMode != Native {
			return failure("HOST_FRAME_INVALID")
		}
		key = "payload"
	default:
		return failure("HOST_FRAME_INVALID")
	}
	if _, ok := f[key].(map[string]any); !ok {
		return failure("HOST_FRAME_INVALID")
	}
	return nil
}
func validateSnapshot(s Snapshot) error {
	if !s.Ref.valid() || s.OldestSeq < 1 || s.LatestSeq < 0 || s.LatestSeq > 9007199254740991 || s.OldestSeq > s.LatestSeq+1 {
		return failure("INVALID_RESPONSE")
	}
	if s.Terminal != nil {
		if err := frameIdentity(s.Terminal, s.Ref); err != nil {
			return err
		}
		if s.Terminal.TerminalState() == "" || number(s.Terminal["seq"]) != s.LatestSeq {
			return failure("INVALID_RESPONSE")
		}
	}
	return nil
}

// NewInput merges explicit business context with the selected Adapter and ask policy.
func NewInput(adapter, prompt string, mode Mode, ctx Context) (Input, error) {
	if adapter == "" || prompt == "" || (mode != Canonical && mode != Native) {
		return nil, failure("INVALID_INPUT")
	}
	items := []any{map[string]any{"namespace": "octo.execution", "name": "adapter", "payload": map[string]any{"type": adapter}}, map[string]any{"namespace": "octo.security", "name": "approval_policy", "payload": map[string]any{"mode": "ask"}}}
	if ctx != nil {
		raw, err := json.Marshal(ctx)
		if err != nil {
			return nil, failure("INVALID_CONTEXT")
		}
		var normalized map[string]any
		if decode(raw, &normalized) != nil || len(normalized) != 2 || normalized["schema"] != "octo.context-envelope/v3" {
			return nil, failure("INVALID_CONTEXT")
		}
		supplied, ok := normalized["items"].([]any)
		if !ok {
			return nil, failure("INVALID_CONTEXT")
		}
		seen := map[string]bool{}
		for _, raw := range supplied {
			item, ok := raw.(map[string]any)
			if !ok {
				return nil, failure("INVALID_CONTEXT")
			}
			ns, nsOK := item["namespace"].(string)
			name, nameOK := item["name"].(string)
			payload, payloadOK := item["payload"].(map[string]any)
			if !nsOK || !nameOK || ns == "" || name == "" || !payloadOK {
				return nil, failure("INVALID_CONTEXT")
			}
			key := ns + "/" + name
			if seen[key] {
				return nil, failure("INVALID_CONTEXT")
			}
			seen[key] = true
			if key == "octo.execution/adapter" && payload["type"] != adapter {
				return nil, failure("CONTEXT_ADAPTER_MISMATCH")
			}
			replaced := false
			for i, v := range items {
				old := v.(map[string]any)
				if old["namespace"] == ns && old["name"] == name {
					items[i] = item
					replaced = true
					break
				}
			}
			if !replaced {
				items = append(items, item)
			}
		}
	}
	envelope := map[string]any{"schema": "octo.context-envelope/v3", "items": items}
	raw, _ := json.Marshal(envelope)
	if len(items) > 32 || len(raw) > 65536 {
		return nil, failure("INVALID_CONTEXT")
	}
	return Input{"runId": id(), "invocationId": id(), "sessionId": id(), "sessionGeneration": 0, "protocol_mode": mode, "inputs": []any{map[string]any{"type": "text", "text": prompt}}, "contexts": envelope}, nil
}

// Client attaches to a host-owned Kernel. Close stops IO without cancelling runs.
type Client struct {
	endpoint string
	slots    chan struct{}
	mu       sync.Mutex
	closed   bool
	sockets  map[net.Conn]bool
}

func Attach(endpoint string) *Client {
	return &Client{endpoint: endpoint, slots: make(chan struct{}, 32), sockets: map[net.Conn]bool{}}
}
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	for conn := range c.sockets {
		conn.Close()
	}
}

type channel struct {
	conn      net.Conn
	reader    *bufio.Reader
	closeOnce sync.Once
	cleanup   func()
}

func (ch *channel) close() { ch.closeOnce.Do(func() { ch.conn.Close(); ch.cleanup() }) }
func (c *Client) open(ctx context.Context) (*channel, error) {
	select {
	case c.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	conn, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "unix", c.endpoint)
	if err != nil {
		<-c.slots
		return nil, &Error{Code: "KERNEL_UNAVAILABLE", Data: err.Error()}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		conn.Close()
		<-c.slots
		return nil, failure("CLIENT_CLOSED")
	}
	c.sockets[conn] = true
	c.mu.Unlock()
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	return &channel{conn: conn, reader: bufio.NewReaderSize(conn, 65536), cleanup: func() { stop(); c.mu.Lock(); delete(c.sockets, conn); c.mu.Unlock(); <-c.slots }}, nil
}
func (ch *channel) line() ([]byte, error) {
	var line []byte
	for {
		part, err := ch.reader.ReadSlice('\n')
		if len(line)+len(part) > maxLine {
			return nil, failure("RESPONSE_TOO_LARGE")
		}
		line = append(line, part...)
		if err == bufio.ErrBufferFull {
			continue
		}
		if err != nil {
			return nil, failure("CONNECTION_CLOSED")
		}
		return line, nil
	}
}
func envelope(method string, params any) (string, []byte, error) {
	requestID := id()
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": requestID, "method": method, "params": params})
	if err != nil {
		return "", nil, err
	}
	line = append(line, '\n')
	if len(line) > maxRequest {
		return "", nil, failure("INVOKE_REQUEST_TOO_LARGE")
	}
	return requestID, line, nil
}
func answer(line []byte, requestID string) (json.RawMessage, error) {
	var response struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      string          `json:"id"`
		Result  json.RawMessage `json:"result"`
		Error   json.RawMessage `json:"error"`
	}
	if decode(line, &response) != nil || response.JSONRPC != "2.0" || response.ID != requestID || (len(response.Result) > 0) == (len(response.Error) > 0) {
		return nil, failure("INVALID_RESPONSE")
	}
	if len(response.Error) > 0 {
		var e struct {
			Data map[string]any `json:"data"`
		}
		if decode(response.Error, &e) != nil {
			return nil, failure("INVALID_RESPONSE")
		}
		stable, ok := e.Data["code"].(string)
		if !ok || stable == "" {
			return nil, failure("INVALID_RESPONSE")
		}
		return nil, &Error{Code: stable, Data: e.Data, Remote: true}
	}
	return response.Result, nil
}
func (c *Client) rpc(ctx context.Context, method string, params any, sideEffect bool) (json.RawMessage, error) {
	requestID, line, err := envelope(method, params)
	if err != nil {
		return nil, err
	}
	duration := 5 * time.Second
	if method == "agent_run.invoke" {
		duration = 30 * time.Second
	}
	call, stop := context.WithTimeout(ctx, duration)
	defer stop()
	ch, err := c.open(call)
	if err != nil {
		return nil, err
	}
	defer ch.close()
	until, _ := call.Deadline()
	ch.conn.SetDeadline(until)
	_, err = io.Copy(ch.conn, bytes.NewReader(line))
	var result json.RawMessage
	if err == nil {
		var raw []byte
		raw, err = ch.line()
		if err == nil {
			result, err = answer(raw, requestID)
		}
	}
	if err != nil && sideEffect {
		var remote *Error
		if !errors.As(err, &remote) || !remote.Remote {
			return nil, &Error{Code: "OPERATION_OUTCOME_UNKNOWN", Data: code(err)}
		}
	}
	return result, err
}
func (c *Client) Status(ctx context.Context) (map[string]any, error) {
	raw, err := c.rpc(ctx, MethodStatus, map[string]any{}, false)
	if err != nil {
		return nil, err
	}
	s, err := value[map[string]any](raw)
	instance, _ := s["kernel_instance_id"].(string)
	if err == nil && (number(s["ipc_version"]) != 1 || instance == "") {
		err = failure("INVALID_RESPONSE")
	}
	return s, err
}
func (c *Client) List(ctx context.Context) ([]map[string]any, error) {
	raw, err := c.rpc(ctx, "adapter.list", map[string]any{}, false)
	if err != nil {
		return nil, err
	}
	return value[[]map[string]any](raw)
}
func (c *Client) Get(ctx context.Context, key map[string]any, instance string) (Snapshot, error) {
	var zero Snapshot
	p := map[string]any{"key": key}
	if instance != "" {
		p["kernel_instance_id"] = instance
	}
	raw, err := c.rpc(ctx, "agent_run.get", p, false)
	if err != nil {
		return zero, err
	}
	s, err := value[Snapshot](raw)
	if err != nil {
		return zero, err
	}
	if err = validateSnapshot(s); err != nil {
		return zero, err
	}
	if instance != "" && s.Ref.KernelInstanceID != instance {
		return zero, failure("KERNEL_INSTANCE_CHANGED")
	}
	actual, _ := json.Marshal(s.Ref.Key())
	expected, _ := json.Marshal(key)
	if !bytes.Equal(actual, expected) {
		return zero, failure("RUN_IDENTITY_MISMATCH")
	}
	return s, nil
}
func (c *Client) Submit(ctx context.Context, input Input) (RunRef, error) {
	var zero RunRef
	raw, err := json.Marshal(input)
	if err != nil {
		return zero, err
	}
	var request Input
	if decode(raw, &request) != nil {
		return zero, failure("INVALID_INPUT")
	}
	for _, key := range []string{"runId", "invocationId", "sessionId"} {
		if _, ok := request[key]; !ok {
			request[key] = id()
		}
	}
	if _, ok := request["sessionGeneration"]; !ok {
		request["sessionGeneration"] = 0
	}
	if _, ok := request["protocol_mode"]; !ok {
		request["protocol_mode"] = Native
	}
	key := map[string]any{"run_id": request["runId"], "invocation_id": request["invocationId"], "agent_session_id": request["sessionId"], "session_generation": request["sessionGeneration"], "protocol_mode": request["protocol_mode"]}
	status, err := c.Status(ctx)
	if err != nil {
		return zero, err
	}
	instance, _ := status["kernel_instance_id"].(string)
	raw, err = c.rpc(ctx, "agent_run.invoke", map[string]any{"input": request, "kernel_instance_id": instance}, true)
	if err == nil {
		var ack struct {
			Instance string `json:"kernel_instance_id"`
			Receipt  RunRef `json:"receipt"`
		}
		err = decode(raw, &ack)
		ack.Receipt.KernelInstanceID = ack.Instance
		if err == nil && !ack.Receipt.valid() {
			err = failure("INVALID_RESPONSE")
		}
		if err == nil {
			a, _ := json.Marshal(ack.Receipt.Key())
			b, _ := json.Marshal(key)
			if ack.Instance != instance || !bytes.Equal(a, b) {
				err = failure("RUN_IDENTITY_MISMATCH")
			} else {
				return ack.Receipt, nil
			}
		}
	}
	var remote *Error
	if errors.As(err, &remote) && remote.Remote {
		return zero, err
	}
	if code(err) != "OPERATION_OUTCOME_UNKNOWN" && code(err) != "INVALID_RESPONSE" && code(err) != "RUN_IDENTITY_MISMATCH" {
		return zero, err
	}
	recovery, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	for _, delay := range []time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond} {
		select {
		case <-time.After(delay):
		case <-recovery.Done():
			return zero, &Error{Code: "RUN_OUTCOME_UNKNOWN", Data: map[string]any{"key": key, "kernel_instance_id": instance}}
		}
		s, e := c.Get(recovery, key, instance)
		if e == nil {
			return s.Ref, nil
		}
		if code(e) == "KERNEL_INSTANCE_CHANGED" || code(e) == "RUN_IDENTITY_MISMATCH" {
			return zero, e
		}
	}
	return zero, &Error{Code: "RUN_OUTCOME_UNKNOWN", Data: map[string]any{"key": key, "kernel_instance_id": instance}}
}
func (c *Client) Cancel(ctx context.Context, ref RunRef, reason string) (bool, error) {
	raw, err := c.rpc(ctx, "agent_run.cancel", map[string]any{"ref": ref, "reason": reason}, true)
	if err != nil {
		return false, err
	}
	var result struct {
		Accepted *bool `json:"accepted"`
	}
	if decode(raw, &result) != nil || result.Accepted == nil {
		return false, failure("OPERATION_OUTCOME_UNKNOWN")
	}
	return *result.Accepted, nil
}
func (c *Client) Respond(ctx context.Context, ref RunRef, response any) error {
	_, err := c.rpc(ctx, "agent_run.respond", map[string]any{"ref": ref, "response": response}, true)
	return err
}
func (c *Client) RespondNative(ctx context.Context, ref RunRef, response any) error {
	_, err := c.rpc(ctx, "agent_run.native.respond", map[string]any{"ref": ref, "response": response}, true)
	return err
}

type Subscription struct {
	client  *Client
	Ref     RunRef
	Cursor  int64
	ctx     context.Context
	channel *channel
	done    bool
	mu      sync.Mutex
}

func (c *Client) Subscribe(ctx context.Context, ref RunRef, after int64) (*Subscription, error) {
	if !ref.valid() || after < 0 || after > 9007199254740991 {
		return nil, failure("INVALID_CURSOR")
	}
	s := &Subscription{client: c, Ref: ref, Cursor: after, ctx: ctx}
	if err := s.connect(); err != nil {
		return nil, err
	}
	return s, nil
}
func (s *Subscription) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.done = true
	if s.channel != nil {
		s.channel.close()
		s.channel = nil
	}
}
func (s *Subscription) connect() error {
	requestID, line, err := envelope("agent_run.subscribe", map[string]any{"ref": s.Ref, "after_seq": s.Cursor})
	if err != nil {
		return err
	}
	ch, err := s.client.open(s.ctx)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		if !success {
			ch.close()
		}
	}()
	ch.conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = io.Copy(ch.conn, bytes.NewReader(line)); err != nil {
		return err
	}
	raw, err := ch.line()
	if err != nil {
		return err
	}
	raw, err = answer(raw, requestID)
	if err != nil {
		return err
	}
	snapshot, err := value[Snapshot](raw)
	if err != nil {
		return err
	}
	if err = validateSnapshot(snapshot); err != nil {
		return err
	}
	if err = sameRef(snapshot.Ref, s.Ref); err != nil {
		return err
	}
	if s.Cursor > snapshot.LatestSeq {
		return failure("INVALID_CURSOR")
	}
	if s.Cursor+1 < snapshot.OldestSeq {
		return failure("STREAM_GAP")
	}
	if snapshot.Terminal != nil && s.Cursor == snapshot.LatestSeq {
		s.Close()
		return nil
	}
	ch.conn.SetDeadline(time.Time{})
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return failure("SUBSCRIPTION_CLOSED")
	}
	s.channel = ch
	success = true
	return nil
}
func (s *Subscription) Next() (Frame, error) {
	var start time.Time
	repairs := 0
	for {
		s.mu.Lock()
		ch, done := s.channel, s.done
		s.mu.Unlock()
		if done {
			return nil, io.EOF
		}
		var raw []byte
		var err error
		if ch == nil {
			err = s.connect()
			if err == nil {
				continue
			}
		} else {
			raw, err = ch.line()
		}
		if err != nil {
			if start.IsZero() {
				start = time.Now()
			}
			if ch != nil {
				ch.close()
				s.mu.Lock()
				s.channel = nil
				s.mu.Unlock()
			}
			if s.ctx.Err() != nil {
				return nil, s.ctx.Err()
			}
			if !retryable(err) || repairs >= 3 || time.Since(start) > 10*time.Second {
				return nil, err
			}
			time.Sleep([]time.Duration{100 * time.Millisecond, 250 * time.Millisecond, 500 * time.Millisecond}[repairs])
			repairs++
			continue
		}
		var n struct {
			JSONRPC string          `json:"jsonrpc"`
			Method  string          `json:"method"`
			ID      json.RawMessage `json:"id"`
			Params  struct {
				Instance string `json:"kernel_instance_id"`
				Run      string `json:"run_id"`
				Stream   string `json:"stream_id"`
				Seq      int64  `json:"seq"`
				Event    Frame  `json:"event"`
				Code     string `json:"code"`
			} `json:"params"`
		}
		if decode(raw, &n) != nil || n.JSONRPC != "2.0" || len(n.ID) > 0 {
			return nil, failure("HOST_FRAME_INVALID")
		}
		if n.Params.Instance != s.Ref.KernelInstanceID {
			return nil, failure("KERNEL_INSTANCE_CHANGED")
		}
		if n.Method == "agent_run.stream_error" {
			return nil, failure(n.Params.Code)
		}
		if n.Method != "agent_run.event" || n.Params.Run != s.Ref.RunID || n.Params.Stream != s.Ref.StreamID {
			return nil, failure("RUN_IDENTITY_MISMATCH")
		}
		if err = frameIdentity(n.Params.Event, s.Ref); err != nil {
			return nil, err
		}
		if n.Params.Seq != s.Cursor+1 || number(n.Params.Event["seq"]) != n.Params.Seq {
			return nil, failure("STREAM_GAP")
		}
		s.Cursor = n.Params.Seq
		if n.Params.Event.TerminalState() != "" {
			s.Close()
		}
		return n.Params.Event, nil
	}
}
func (c *Client) Invoke(adapter, prompt string) (*Subscription, error) {
	return c.invoke(adapter, prompt, Canonical, nil)
}
func (c *Client) InvokeWithContext(adapter, prompt string, ctx Context) (*Subscription, error) {
	return c.invoke(adapter, prompt, Canonical, ctx)
}
func (c *Client) InvokeNative(adapter, prompt string) (*Subscription, error) {
	return c.invoke(adapter, prompt, Native, nil)
}
func (c *Client) InvokeNativeWithContext(adapter, prompt string, ctx Context) (*Subscription, error) {
	return c.invoke(adapter, prompt, Native, ctx)
}
func (c *Client) invoke(adapter, prompt string, mode Mode, ctx Context) (*Subscription, error) {
	input, err := NewInput(adapter, prompt, mode, ctx)
	if err != nil {
		return nil, err
	}
	return c.InvokeInput(context.Background(), input)
}
func (c *Client) InvokeInput(ctx context.Context, input Input) (*Subscription, error) {
	ref, err := c.Submit(ctx, input)
	if err != nil {
		return nil, err
	}
	return c.Subscribe(ctx, ref, 0)
}
