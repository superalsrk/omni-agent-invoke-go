package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	omni "github.com/superalsrk/omni-agent-invoke-go"
)

func run() error {
	business := omni.Context{"schema": "octo.context-envelope/v3", "items": []any{
		map[string]any{"namespace": "octo.content", "name": "inline", "payload": map[string]any{"entries": []any{map[string]any{"text": "This is a Go context example."}}}},
	}}
	client := omni.Attach(os.Getenv("OCTO_KERNEL_ENDPOINT"))
	defer client.Close()
	// Business carries the Octo execution Context. InvokeInput also accepts a Go context for local IO.
	stream, err := client.InvokeWithContext("codex", "Reply with OK without using tools.", business)
	if err != nil {
		return err
	}
	defer stream.Close()
	// InvokeNativeWithContext selects native mode with the same business envelope.
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
