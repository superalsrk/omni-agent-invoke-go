package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	omni "github.com/superalsrk/omni-agent-invoke-go"
	"io"
	"os"
	"os/signal"
	"time"
)

func read(path string, v any) error {
	var reader io.Reader = os.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	bytes, err := io.ReadAll(io.LimitReader(reader, 33_554_433))
	if err != nil {
		return err
	}
	if len(bytes) > 33_554_432 {
		return errors.New("JSON input exceeds 32 MiB")
	}
	return json.Unmarshal(bytes, v)
}
func save(path string, ref omni.RunRef) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	return json.NewEncoder(file).Encode(ref)
}
func main() {
	code, err := run()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if code == 0 {
			code = 1
		}
	}
	os.Exit(code)
}
func run() (int, error) {
	if len(os.Args) < 2 || os.Args[1] == "--help" {
		fmt.Println("playboard <status|list|playboard|submit|get|subscribe|cancel|respond|native-respond> [options]\nUse --endpoint or OCTO_KERNEL_ENDPOINT. --mode canonical|native; --context supplies an Octo Context envelope.")
		return 0, nil
	}
	command := os.Args[1]
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	endpoint := flags.String("endpoint", os.Getenv("OCTO_KERNEL_ENDPOINT"), "existing Kernel Unix socket")
	prompt := flags.String("prompt", "", "text prompt")
	adapter := flags.String("adapter", "codex", "Adapter type, e.g. codex, claude-code, openclaw, dsh")
	mode := flags.String("mode", "canonical", "canonical or native")
	request := flags.String("request", "-", "full input JSON file")
	contextFile := flags.String("context", "", "Context envelope JSON file with --prompt")
	receipt := flags.String("receipt", "", "save accepted RunRef to a new file")
	refFile := flags.String("ref", "", "saved RunRef JSON file")
	after := flags.Int64("after", 0, "last consumed sequence")
	reason := flags.String("reason", "playboard_user_cancelled", "cancellation reason")
	responseFile := flags.String("response", "-", "interaction reply JSON file")
	seconds := flags.Int("timeout", 0, "execution deadline in seconds; 0 disables")
	if err := flags.Parse(os.Args[2:]); err != nil {
		return 2, err
	}
	if flags.NArg() != 0 || *endpoint == "" || *seconds < 0 || *seconds > 31536000 {
		return 2, errors.New("invalid arguments or missing Kernel endpoint")
	}
	client := omni.Attach(*endpoint)
	defer client.Close()
	ctx := context.Background()
	emit := func(value any) error { return json.NewEncoder(os.Stdout).Encode(value) }
	switch command {
	case "status":
		v, err := client.Status(ctx)
		if err != nil {
			return 1, err
		}
		return 0, emit(v)
	case "list":
		v, err := client.List(ctx)
		if err != nil {
			return 1, err
		}
		return 0, emit(v)
	}
	var ref omni.RunRef
	switch command {
	case "playboard", "submit":
		var input omni.Input
		var err error
		if *prompt != "" {
			var envelope omni.Context
			if *contextFile != "" {
				if err = read(*contextFile, &envelope); err != nil {
					return 2, err
				}
			}
			input, err = omni.NewInput(*adapter, *prompt, omni.Mode(*mode), envelope)
		} else {
			if *contextFile != "" {
				return 2, errors.New("--context requires --prompt")
			}
			err = read(*request, &input)
		}
		if err != nil {
			return 2, err
		}
		ref, err = client.Submit(ctx, input)
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(os.Stderr, "Accepted receipt:", ref)
		if *receipt != "" {
			if err = save(*receipt, ref); err != nil {
				return 1, err
			}
		}
		if command == "submit" {
			return 0, emit(ref)
		}
	case "get", "subscribe", "cancel", "respond", "native-respond":
		if err := read(*refFile, &ref); err != nil {
			return 2, err
		}
		switch command {
		case "get":
			v, err := client.Get(ctx, ref.Key(), ref.KernelInstanceID)
			if err != nil {
				return 1, err
			}
			return 0, emit(v)
		case "cancel":
			v, err := client.Cancel(ctx, ref, *reason)
			if err != nil {
				return 1, err
			}
			return 0, emit(map[string]bool{"accepted": v})
		case "respond", "native-respond":
			var reply any
			if err := read(*responseFile, &reply); err != nil {
				return 2, err
			}
			var err error
			if command == "respond" {
				err = client.Respond(ctx, ref, reply)
			} else {
				err = client.RespondNative(ctx, ref, reply)
			}
			if err != nil {
				return 1, err
			}
			return 0, emit(map[string]any{})
		}
	default:
		return 2, errors.New("unknown command")
	}
	stream, err := client.Subscribe(ctx, ref, *after)
	if err != nil {
		return 1, err
	}
	defer stream.Close()
	observe, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()
	if *seconds > 0 {
		var cancel context.CancelFunc
		observe, cancel = context.WithTimeout(observe, time.Duration(*seconds)*time.Second)
		defer cancel()
	}
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-done:
			return
		case <-observe.Done():
		}
		if command == "playboard" {
			accepted, err := client.Cancel(context.Background(), ref, "playboard_interrupted")
			fmt.Fprintln(os.Stderr, "Cancellation accepted:", accepted, "error:", err)
			select {
			case <-done:
				return
			case <-time.After(10 * time.Second):
			}
		}
		stream.Close()
	}()
	exit := func() int {
		if errors.Is(observe.Err(), context.DeadlineExceeded) {
			return 124
		}
		if observe.Err() != nil {
			return 130
		}
		return 0
	}
	for {
		frame, err := stream.Next()
		if err == io.EOF {
			return exit(), nil
		}
		if err != nil {
			return max(1, exit()), err
		}
		if err = emit(frame); err != nil {
			return 1, err
		}
		if state := frame.TerminalState(); state != "" {
			if code := exit(); code != 0 {
				return code, nil
			}
			if state != "completed" {
				return 1, errors.New("execution ended: " + state)
			}
			return 0, nil
		}
	}
}
