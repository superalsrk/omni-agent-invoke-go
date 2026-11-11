package omni

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func testServer(t *testing.T, handler func(net.Conn, map[string]any)) string {
	t.Helper()
	// macOS limits Unix socket paths to 104 bytes.
	directory, err := os.MkdirTemp("/tmp", "omni-go-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(directory) })
	endpoint := filepath.Join(directory, "k.sock")
	listener, err := net.Listen("unix", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			workers.Add(1)
			go func() {
				defer workers.Done()
				defer conn.Close()
				conn.SetDeadline(time.Now().Add(5 * time.Second))
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var request map[string]any
				if json.Unmarshal(line, &request) != nil {
					return
				}
				handler(conn, request)
			}()
		}
	}()
	t.Cleanup(func() { listener.Close(); <-stopped; workers.Wait() })
	return endpoint
}
func respond(conn net.Conn, request map[string]any, result any) {
	data, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": request["id"], "result": result})
	// Split the response to exercise transport framing, not just JSON decoding.
	conn.Write(data[:len(data)/2])
	conn.Write(append(data[len(data)/2:], '\n'))
}
func testRef() RunRef {
	return RunRef{KernelInstanceID: "kernel", RunID: "run", InvocationID: "invoke", AgentSessionID: "session", SessionGeneration: 0, StreamID: "stream", ProtocolMode: Canonical, Protocol: "test/v1"}
}
func testFrame(ref RunRef, seq int) Frame {
	raw, _ := json.Marshal(ref)
	var frame Frame
	decode(raw, &frame)
	delete(frame, "kernel_instance_id")
	frame["seq"] = seq
	frame["kind"] = "control"
	frame["control"] = map[string]any{"type": "state", "state": "completed"}
	return frame
}
func TestSubmitRecoversLostAckWithoutResubmission(t *testing.T) {
	var mu sync.Mutex
	calls := map[string]int{}
	var ref RunRef
	endpoint := testServer(t, func(conn net.Conn, request map[string]any) {
		method := request["method"].(string)
		mu.Lock()
		calls[method]++
		mu.Unlock()
		switch method {
		case "status":
			respond(conn, request, map[string]any{"ipc_version": 2, "kernel_instance_id": "kernel"})
		case "agent_run.invoke":
			input := request["params"].(map[string]any)["input"].(map[string]any)
			r := testRef()
			r.RunID = input["runId"].(string)
			r.InvocationID = input["invocationId"].(string)
			r.AgentSessionID = input["sessionId"].(string)
			mu.Lock()
			ref = r
			mu.Unlock() // Close without ACK after acceptance.
		case "agent_run.get":
			mu.Lock()
			r := ref
			mu.Unlock()
			respond(conn, request, Snapshot{Ref: r, OldestSeq: 1, LatestSeq: 0})
		}
	})
	client := Attach(endpoint)
	defer client.Close()
	input, _ := textInput("codex", "hello", Canonical)
	ref, err := client.Submit(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if ref.RunID != input["runId"] {
		t.Fatal("changed run identity")
	}
	mu.Lock()
	defer mu.Unlock()
	if calls["agent_run.invoke"] != 1 || calls["agent_run.get"] != 1 {
		t.Fatalf("calls: %v", calls)
	}
}
func TestSubscriptionChecksCursorAndIdentity(t *testing.T) {
	for _, scenario := range []string{"terminal", "gap", "identity", "instance"} {
		t.Run(scenario, func(t *testing.T) {
			ref := testRef()
			endpoint := testServer(t, func(conn net.Conn, request map[string]any) {
				respond(conn, request, Snapshot{Ref: ref, OldestSeq: 1, LatestSeq: 1})
				frame := testFrame(ref, 1)
				params := map[string]any{"kernel_instance_id": "kernel", "run_id": ref.RunID, "stream_id": ref.StreamID, "seq": 1, "event": frame}
				switch scenario {
				case "gap":
					params["seq"] = 2
					frame["seq"] = 2
				case "identity":
					frame["invocation_id"] = "other"
				case "instance":
					params["kernel_instance_id"] = "other"
				}
				json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "method": "agent_run.event", "params": params})
			})
			client := Attach(endpoint)
			defer client.Close()
			stream, err := client.Subscribe(context.Background(), ref, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer stream.Close()
			frame, err := stream.Next()
			expected := map[string]string{"gap": "STREAM_GAP", "identity": "RUN_IDENTITY_MISMATCH", "instance": "KERNEL_INSTANCE_CHANGED"}[scenario]
			if expected != "" {
				if err == nil || code(err) != expected {
					t.Fatalf("want %s got %v", expected, err)
				}
				return
			}
			if err != nil || frame.TerminalState() != "completed" {
				t.Fatalf("%v %v", frame, err)
			}
			if _, err = stream.Next(); err != io.EOF {
				t.Fatalf("terminal should end: %v", err)
			}
		})
	}
}
func TestLostCancelAckIsNeverRetried(t *testing.T) {
	calls := make(chan string, 2)
	endpoint := testServer(t, func(_ net.Conn, r map[string]any) { calls <- r["method"].(string) })
	client := Attach(endpoint)
	defer client.Close()
	_, err := client.Cancel(context.Background(), testRef(), "test")
	if code(err) != "OPERATION_OUTCOME_UNKNOWN" {
		t.Fatal(err)
	}
	if <-calls != "agent_run.cancel" {
		t.Fatal("wrong method")
	}
	select {
	case <-calls:
		t.Fatal("side effect retried")
	default:
	}
}
func TestInputAgentEnvAndImage(t *testing.T) {
	ctx := AgentEnv{"schema": "octo.context-envelope/v3", "items": []any{map[string]any{"namespace": "octo.execution", "name": "adapter", "payload": map[string]any{"type": "openclaw", "provider_agent_id": "main"}}}}
	input, err := NewInput([]any{map[string]any{"type": "text", "text": "hello"}}, ctx, Native)
	if err != nil {
		t.Fatal(err)
	}
	if input["protocol_mode"] != string(Native) {
		t.Fatal("mode")
	}
	raw, _ := json.Marshal(input["agentEnv"])
	expected, _ := json.Marshal(ctx)
	if string(raw) != string(expected) {
		t.Fatal("agentEnv changed")
	}
	if _, ok := input["contexts"]; ok {
		t.Fatal("legacy contexts emitted")
	}
	ctx["items"] = []any{}
	if len(input["agentEnv"].(map[string]any)["items"].([]any)) != 1 {
		t.Fatal("aliased environment")
	}
	image, err := ImageInput([]byte("abc"), "image/png")
	if err != nil || image["data"] != "YWJj" || image["sha256"] != "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad" {
		t.Fatalf("%v %v", image, err)
	}
	for _, data := range [][]byte{nil, make([]byte, 2097153)} {
		if _, err := ImageInput(data, "image/png"); err == nil {
			t.Fatal("invalid size accepted")
		}
	}
}

func TestCompleteInputPassesThrough(t *testing.T) {
	for _, mode := range []Mode{Canonical, Native} {
		t.Run(string(mode), func(t *testing.T) {
			image, _ := ImageInput([]byte("abc"), "image/png")
			parts := []any{map[string]any{"type": "text", "text": "Application instructions and history"}, image, map[string]any{"type": "text", "text": "Current question"}}
			env := AgentEnv{"schema": "octo.context-envelope/v3", "items": []any{
				map[string]any{"namespace": "private.host", "name": "config", "payload": map[string]any{"nested": []any{1, true}}},
				map[string]any{"namespace": "octo.execution", "name": "adapter", "payload": map[string]any{"type": "openclaw", "provider_agent_id": "main"}},
			}}
			input, err := NewInput(parts, env, mode)
			if err != nil {
				t.Fatal(err)
			}
			input["runMetadata"] = map[string]any{"business": "caller-owned"}
			expected, _ := json.Marshal(input)
			captured := make(chan []byte, 1)
			endpoint := testServer(t, func(conn net.Conn, request map[string]any) {
				if request["method"] == "status" {
					respond(conn, request, map[string]any{"ipc_version": 2, "kernel_instance_id": "kernel"})
					return
				}
				sent := request["params"].(map[string]any)["input"]
				raw, _ := json.Marshal(sent)
				captured <- raw
				json.NewEncoder(conn).Encode(map[string]any{"jsonrpc": "2.0", "id": request["id"], "error": map[string]any{"code": -32000, "message": "inspect request", "data": map[string]any{"code": "TEST_STOP"}}})
			})
			client := Attach(endpoint)
			defer client.Close()
			if _, err = client.Submit(context.Background(), input); code(err) != "TEST_STOP" {
				t.Fatal(err)
			}
			if actual := <-captured; string(actual) != string(expected) {
				t.Fatalf("request changed: %s", actual)
			}
		})
	}
}

func TestMalformedResponseRejected(t *testing.T) {
	for _, line := range []string{`{"jsonrpc":"2.0","id":"wrong","result":{}}`, `{"jsonrpc":"2.0","id":"id","result":{},"error":{}}`, `{"jsonrpc":"2.0","id":"id","result":{}} {}`} {
		if _, err := answer([]byte(line), "id"); err == nil {
			t.Fatal("malformed response accepted")
		}
	}
}
