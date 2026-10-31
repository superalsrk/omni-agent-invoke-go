package main

import (
	"encoding/json"
	"fmt"
	omni "github.com/superalsrk/omni-agent-invoke-go"
	"io"
	"os"
	"sync"
)

func run(client *omni.Client, adapter string) error {
	var stream *omni.Subscription
	var err error
	if adapter == "openclaw" {
		ctx := omni.Context{"schema": "octo.context-envelope/v3", "items": []any{map[string]any{"namespace": "octo.execution", "name": "adapter", "payload": map[string]any{"type": "openclaw", "provider_agent_id": "main"}}}}
		stream, err = client.InvokeWithContext(adapter, "Reply with OK without using tools.", ctx)
	} else {
		stream, err = client.Invoke(adapter, "Reply with OK without using tools.")
	}
	if err != nil {
		return err
	}
	defer stream.Close()
	for {
		frame, err := stream.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		raw, _ := json.Marshal(map[string]any{"adapter": adapter, "frame": frame})
		fmt.Println(string(raw))
		if state := frame.TerminalState(); state != "" && state != "completed" {
			return fmt.Errorf("%s ended: %s", adapter, state)
		}
	}
}
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run ./examples/concurrent codex claude-code openclaw dsh")
		os.Exit(2)
	}
	client := omni.Attach(os.Getenv("OCTO_KERNEL_ENDPOINT"))
	defer client.Close()
	var workers sync.WaitGroup
	results := make(chan error, len(os.Args)-1)
	for _, adapter := range os.Args[1:] {
		workers.Go(func() { results <- run(client, adapter) })
	}
	workers.Wait()
	close(results)
	failed := false
	for err := range results {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			failed = true
		}
	}
	if failed {
		os.Exit(1)
	}
}
