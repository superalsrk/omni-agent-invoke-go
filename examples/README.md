# Go Examples

Run these commands from the repository root after following the root README's build/install instructions. Execution examples use an already-running [Octo Runtime](https://github.com/Mininglamp-OSS/octo-runtime) and an authenticated Codex Provider.

```sh
export OCTO_KERNEL_ENDPOINT="$HOME/.octo-runtime/sockets/octo-runtime-executor-kernel.sock"
go run ./examples/with_agent_env
go run ./examples/inputs --dry-run
go run ./examples/inputs --image /path/to/image.png --dry-run
go run ./examples/inputs --image /path/to/image.png
go run ./examples/inputs --native
go run ./examples/concurrent codex codex
```

`--dry-run` prints the constructed Input without connecting to Runtime. Omit it to submit. `--native` selects the explicit native protocol mode; canonical is the default. For live image calls use a PNG, JPEG or WebP supported by your Provider/model.

## Import AgentEnv and Construct Input

```go
import omni "github.com/superalsrk/omni-agent-invoke-go"

// AgentEnv is distinct from context.Context, which controls local IO.
var business omni.AgentEnv
```

AgentEnv carries execution settings. It is not the prompt, message history or a local cancellation token. See the complete [AgentEnv example](with_agent_env/main.go) and [Input example](inputs/main.go) for executable construction, error handling, full-frame consumption and terminal-state checks. Both text convenience methods have explicit native variants; full input methods accept both modes.

Text Input starts with an ordered text part. To make a multimodal Input, append an image part to the same `inputs` array:

```go
input, err := omni.NewInput([]any{map[string]any{"type":"text","text":"Describe this image."}}, omni.AgentEnv{"schema":"octo.context-envelope/v3","items":[]any{}}, omni.Canonical)
if err != nil { return err }
image, err := omni.ImageInput(imageBytes, "image/png")
if err != nil { return err }
input["inputs"] = append(input["inputs"].([]any), image)
```

The full Input example imports AgentEnv, places business text in inputs and execution limits in agentEnv, constructs text, and optionally appends an image. The helpers accept explicit bytes and MIME type and compute `data` (base64), `byteSize` and lowercase `sha256`. They validate format names and the 1-byte to 2-MiB size range; decoding image contents remains the Provider's responsibility. They never fetch an image URL or silently replace images with text.

## Example Index

| Example | Purpose |
| --- | --- |
| [AgentEnv](with_agent_env/main.go) | Import the public AgentEnv type and invoke with complete inputs and execution settings |
| [Inputs](inputs/main.go) | Text-only, text + image, canonical/native and offline construction |
| [Concurrent](concurrent/main.go) | Await independent harness invocations and report individual failures |
| [AgentEnv JSON](agent-env.json) | Pass execution settings using playboard `--agent-env` |
| [OpenClaw request](openclaw-request.json) | Select an existing Gateway Agent with `provider_agent_id` |
| [Protocol design](../docs/protocol.md) | Framing, identity, modes, recovery and lifecycle |

Use Adapter IDs `codex`, `claude-code`, `openclaw` and `dsh` to select Codex, Claude Code, OpenClaw and DeepSeek Harness respectively. Each must be installed/configured for your Runtime; capabilities depend on the selected Adapter. The concurrent example provides the necessary OpenClaw AgentEnv for Gateway Agent `main`; change it for your deployment.

All examples preserve complete HostFrames. A control frame with `completed`, `failed`, `cancelled` or `lost` determines the execution outcome. Breaking a loop or closing observation does not cancel an accepted run. Persist a receipt with the playboard or lower-level submit API when recovery is required.
