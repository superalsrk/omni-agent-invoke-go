package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	omni "github.com/superalsrk/omni-agent-invoke-go"
)

func run() error {
	business := omni.AgentEnv{"schema": "octo.context-envelope/v3", "items": []any{
		map[string]any{"namespace": "octo.execution", "name": "adapter", "payload": map[string]any{"type": "codex"}},
	}}
	client := omni.Attach(os.Getenv("OCTO_KERNEL_ENDPOINT"))
	defer client.Close()
	input, err := omni.NewInput([]any{map[string]any{"type": "text", "text": "This is a Go application."}, map[string]any{"type": "text", "text": "Reply with OK without using tools."}}, business, omni.Canonical)
	if err != nil {
		return err
	}
	stream, err := client.InvokeInput(context.Background(), input)
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
		if err = json.NewEncoder(os.Stdout).Encode(frame); err != nil {
			return err
		}
		if state := frame.TerminalState(); state != "" && state != "completed" {
			return fmt.Errorf("execution ended: %s", state)
		}
	}
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
