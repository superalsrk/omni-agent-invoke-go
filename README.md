# Omni Agent Invoke Go: Codex, Claude Code, OpenClaw and DeepSeek Harness

**Invoke Codex, Claude Code (CC), OpenClaw, DeepSeek Harness (DSH), and other supported agent harnesses from Go. Powered by [Octo Runtime](https://github.com/Mininglamp-OSS/octo-runtime).**

A small, independent Go client and runnable **playboard** for a separately installed Octo Runtime Kernel. One local execution API for streaming, explicit AgentEnv, native output, cancellation and concurrent harness calls.

## Octo Runtime Is the Foundation

**[Octo Runtime](https://github.com/Mininglamp-OSS/octo-runtime) turns a collection of agent harnesses into reusable local infrastructure.** Its supervised Kernel, execution worker and Provider adapters give applications a shared execution contract while keeping Provider-specific capabilities accessible.

That is what makes this library useful: your Go application can work with multiple established harnesses through one service boundary. Canonical events provide a common format; native payloads retain Provider-specific details. Your application owns its business data and user experience, while Octo Runtime provides the execution foundation.

Explore and star **[Octo Runtime](https://github.com/Mininglamp-OSS/octo-runtime)**, read its [documentation](https://github.com/Mininglamp-OSS/octo-runtime/blob/main/docs/en/README.md), and discover its [Adapter ecosystem](https://github.com/Mininglamp-OSS/octo-runtime/blob/main/packages/README.md).

## Standalone Service, Independent Client

```text
Go application / playboard
           |
           | JSON-RPC 2.0 + NDJSON over a Unix socket
           v
Independently installed Octo Runtime Kernel
           |
           v
Codex / Claude Code / OpenClaw / DeepSeek Harness / ...
```

This library has **no dependency on upstream Runtime source or SDK packages**. It implements public Kernel IPC and ships its own generated protocol identity declarations. It does not start Runtime or install/authenticate Providers. The separate Runtime service requires Node.js; the Go client uses its own language toolchain.

This is an independent community project, not an official Octo Runtime SDK. Version 0.1 targets macOS/Linux Unix sockets.

## Install and Run

Requirements: Go 1.25+, plus the independently installed Runtime and an authenticated Provider.

```sh
# Install and start the foundation service independently.
npm install -g @mininglamp-oss/octo-runtime
octo-runtime executor-kernel start

git clone https://github.com/superalsrk/omni-agent-invoke-go.git
cd omni-agent-invoke-go
mkdir -p .tmp
go build -o .tmp/playboard ./cmd/playboard

# Use your actual endpoint for custom Runtime Homes/profiles.
export OCTO_KERNEL_ENDPOINT="$HOME/.octo-runtime/sockets/octo-runtime-executor-kernel.sock"
.tmp/playboard status
.tmp/playboard list
.tmp/playboard playboard --adapter codex --mode canonical \
  --prompt 'Reply with OK without using tools.' --receipt .tmp/first-run.json
```

Already running a Kernel? Skip installation/startup and point the client at its existing socket. Authenticate each Provider separately; Runtime does not sign in for you.

## Go API

```go
import omni "github.com/superalsrk/omni-agent-invoke-go"

client := omni.Attach(os.Getenv("OCTO_KERNEL_ENDPOINT"))
defer client.Close()
stream, err := client.Invoke("codex", "Say hello.")
if err != nil { return err }
defer stream.Close()
for {
    frame, err := stream.Next()
    if err == io.EOF { break }
    if err != nil { return err }
    fmt.Println(frame)
}
```

See [examples](examples/README.md) for complete runnable programs, including imports and terminal-state checks.

Use `Invoke(adapter, prompt)` for canonical output or `InvokeNative(adapter, prompt)` for native output. Both yield complete HostFrames; observe the terminal control frame.

For full input, use:

```go
input, err := omni.NewInput(
    []any{map[string]any{"type":"text","text":"Application instructions and history"},
          map[string]any{"type":"text","text":prompt}},
    omni.AgentEnv{"schema":"octo.context-envelope/v3","items":[]any{}},
    omni.Canonical,
)
if err != nil { return err }
stream, err := client.InvokeInput(context.Background(), input)
```

Only `inputs` and `agentEnv` are required by Runtime. Both pass through without added content or merged defaults. AgentEnv carries execution settings; business prompts and history belong in inputs. The old Context type, context methods and `contexts` wire field are removed. See [examples](examples/README.md) for images and explicit Adapter configuration.

## DeepSeek Harness and Other Providers

Use Adapter ID **`dsh`** for **DeepSeek Harness (DSH)**:

```sh
.tmp/playboard playboard --adapter dsh --mode canonical --prompt 'Reply with OK.'
.tmp/playboard playboard --adapter claude-code --mode canonical --prompt 'Reply with OK.'
.tmp/playboard playboard --request examples/openclaw-request.json
```

DSH must be installed/configured and registered by the independently running Runtime. See the [upstream DSH Adapter guide](https://github.com/Mininglamp-OSS/octo-runtime/blob/main/packages/octo-runtime-adapter-dsh/README.md). Native mode is available only when the selected Adapter advertises it. OpenClaw uses an agentEnv item selecting an existing Gateway Agent, such as `provider_agent_id: "main"`.

Supporting a shared execution API does not imply identical Provider capabilities. Check `list` and the upstream guide. Live verification of Codex is not a claim that every Provider was tested.

## Concurrent Harness Invocation

```sh
go run ./examples/concurrent codex claude-code openclaw dsh
```

Only Codex available locally? Replace the Adapter arguments with `codex codex` to exercise two independent concurrent invocations. Examples await all tasks and report individual failures.

## Playboard and Explicit Controls

```sh
.tmp/playboard playboard --adapter codex --mode native --prompt 'Say hello.' --agent-env examples/agent-env.json
.tmp/playboard submit --adapter codex --prompt 'Say hello.' --receipt .tmp/submitted-run.json
.tmp/playboard get --ref .tmp/submitted-run.json
.tmp/playboard subscribe --ref .tmp/submitted-run.json --after 0
.tmp/playboard cancel --ref .tmp/submitted-run.json --reason user_cancelled
.tmp/playboard respond --ref .tmp/submitted-run.json --response answer.json
.tmp/playboard native-respond --ref .tmp/submitted-run.json --response native-answer.json
```

Stdout is NDJSON. Save the full RunRef for recovery. Receipts use new files and are not overwritten. `--timeout` is an execution deadline in seconds; playboard requests cancellation and observes a brief tail. Closing observation or the client never stops the Kernel or automatically cancels an accepted run.

Lost acceptance ACKs recover only by querying the original identity. Controls are never automatically replayed after an unknown outcome. See [protocol design](docs/protocol.md) for identity validation, framing, recovery and lifecycle limits.

## Local Verification

```sh
go test -race ./...
go vet ./...
node scripts/live-smoke.mjs
```

The explicit live smoke uses an existing Kernel and authenticated Codex to verify canonical/native calls, receipt lookup and replay. It does not install or stop Runtime. Evidence is recorded under `docs/live-smoke.json` without raw Provider output. No GitHub workflows or CI gates are added. Platform and live-Provider limits are recorded in [verification](docs/verification.md).

## Ecosystem and License

[Octo Runtime](https://github.com/Mininglamp-OSS/octo-runtime) is the independent foundation service. Other client libraries: [Rust](https://github.com/superalsrk/omni-agent-invoke-rs), [TypeScript](https://github.com/superalsrk/omni-agent-invoke-ts), [Python](https://github.com/superalsrk/omni-agent-invoke-py), [Go](https://github.com/superalsrk/omni-agent-invoke-go).

[MIT License](LICENSE). Protocol types are generated independently; no upstream implementation is vendored. The projects and Provider vendors are separate, with no implied endorsement.
