package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	omni "github.com/superalsrk/omni-agent-invoke-go"
	"io"
	"mime"
	"os"
	"path/filepath"
)

func run() error {
	path := flag.String("image", "", "optional PNG/JPEG/WebP image path")
	native := flag.Bool("native", false, "use native output")
	dry := flag.Bool("dry-run", false, "print constructed input without contacting Kernel")
	flag.Parse()
	ctx := omni.Context{"schema": "octo.context-envelope/v3", "items": []any{
		map[string]any{"namespace": "octo.content", "name": "inline", "payload": map[string]any{"entries": []any{map[string]any{"text": "This is a Go input construction example."}}}},
		map[string]any{"namespace": "octo.execution", "name": "limits", "payload": map[string]any{"run_timeout_ms": 60000}},
	}}
	mode := omni.Canonical
	if *native {
		mode = omni.Native
	}
	input, err := omni.NewInput("codex", "Reply with OK without using tools.", mode, ctx)
	if err != nil {
		return err
	}
	if *path != "" {
		data, err := os.ReadFile(*path)
		if err != nil {
			return err
		}
		part, err := omni.ImageInput(data, mime.TypeByExtension(filepath.Ext(*path)))
		if err != nil {
			return err
		}
		input["inputs"] = append(input["inputs"].([]any), part)
	}
	if *dry {
		return json.NewEncoder(os.Stdout).Encode(input)
	}
	client := omni.Attach(os.Getenv("OCTO_KERNEL_ENDPOINT"))
	defer client.Close()
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
