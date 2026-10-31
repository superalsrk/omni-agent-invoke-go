# Protocol Design

## Service Boundary

Octo Runtime is an independently installed local service. Applications install and start it with `npm install -g @mininglamp-oss/octo-runtime` and `octo-runtime executor-kernel start`. These clients attach to the actual Kernel endpoint. They do not import Runtime source, link its SDK, start a worker, or own its process.

The implementation targets Kernel IPC v1, Run contract v7 and Context envelope `octo.context-envelope/v3`. See the [public Kernel IPC reference](https://github.com/Mininglamp-OSS/octo-runtime/blob/main/docs/en/reference/kernel-ipc.md) and [Context catalog](https://github.com/Mininglamp-OSS/octo-runtime/blob/main/docs/en/reference/contexts.md).

## Generated and Handwritten Code

Identity models, protocol mode enums and method names are generated from the small, independent [client contract](protocol-contract.json). [Generation provenance](protocol-provenance.json) records the contract revision and SHA-256. These declarations are a deliberately limited subset, not a claim of implementing every upstream schema validator.

The standalone generator runs in a temporary workspace outside the four library repositories. Only generated artifacts and the contract record are committed. No generator, temporary directory, schema download, upstream checkout or Runtime package is needed to build or run these clients. Changing a protocol requires reviewing the contract, regenerating, formatting, and rerunning local checks. A digest proves a shared generation input; it is not proof of full wire compatibility.

Connection handling, submission, recovery, cancellation and streaming are independently implemented in each language. There is no vendored upstream client implementation. Project code is MIT-licensed.

## Transport and Framing

The supported transport is a Unix domain socket on macOS/Linux. Each connection sends one UTF-8 JSON-RPC 2.0 request terminated by LF. IDs are unique nonempty strings. Unary calls close after the response; subscriptions keep reading notifications on that connection.

One message may span multiple reads, and one read may contain multiple messages. Read complete LF-delimited lines before parsing JSON. Requests are capped at 32 MiB and response lines at 42,008,576 bytes. Streaming is pulled by the consumer; connection budgets are bounded. These are client resource limits, not an application sandbox.

```json
{"jsonrpc":"2.0","id":"status-1","method":"status","params":{}}
```

A response must match the request ID and contain exactly one of `result` or `error`. Business errors are identified by the stable string `error.data.code`, retaining unknown remote codes rather than matching human-readable messages.

## RPC Methods

| Method | Params | Result |
| --- | --- | --- |
| `status` | `{}` | Kernel instance, worker state and readiness |
| `adapter.list` | `{}` | Registered Adapter metadata |
| `agent_run.invoke` | `{input, kernel_instance_id?}` | `{kernel_instance_id, receipt}` |
| `agent_run.get` | `{key, kernel_instance_id?}` | Original ref, retained range, optional terminal frame |
| `agent_run.subscribe` | `{ref, after_seq}` | Snapshot ACK, then `agent_run.event` notifications |
| `agent_run.cancel` | `{ref, reason?}` | `{accepted}`; acceptance is not completion |
| `agent_run.respond` | `{ref, response}` | Canonical interaction acceptance |
| `agent_run.native.respond` | `{ref, response}` | Provider-native interaction acceptance |

Rust additionally exposes environment overrides through its lower-level submission API. The libraries intentionally do not promise full feature parity with upstream SDKs.

## Identity and Lifecycle

An invocation input uses camelCase `runId`, `invocationId`, `sessionId` and `sessionGeneration`. The lookup key uses `run_id`, `invocation_id`, `agent_session_id`, `session_generation` and `protocol_mode`.

The accepted `RunRef` adds `kernel_instance_id`, `stream_id` and `protocol`. Save the complete receipt. Never reconstruct it from a business session name, substitute another run, or continue across a Kernel restart.

```mermaid
sequenceDiagram
    participant App
    participant Client
    participant Kernel
    App->>Client: InvokeInput + explicit protocol mode
    Client->>Kernel: status
    Kernel-->>Client: kernel_instance_id
    Client->>Kernel: agent_run.invoke (once)
    Kernel-->>Client: acceptance receipt
    Client->>Kernel: agent_run.subscribe(ref, after_seq)
    Kernel-->>App: ordered HostFrames
    Kernel-->>App: terminal control frame
```

New convenience invocations get fresh execution, invocation and session IDs. Reusing a Provider session requires an explicit full input; these examples do not silently share conversation history.

## Canonical and Native Modes

The context-free and context-aware `invoke` methods select **canonical** mode. Their explicitly named native variants select **native** mode. Full-input APIs use `input.protocol_mode` explicitly. Both modes yield complete HostFrames:

- Canonical: `{kind:"canonical", event:{...}, ...identity, seq}`. Read normalized event content from `event`.
- Native: `{kind:"native", messageKind, payload:{...}, ...identity, seq}`. Preserve Provider-specific details in `payload`.
- Control: `{kind:"control", control:{...}, ...identity, seq}`. A state control frame reports `completed`, `failed`, `cancelled` or `lost`.

A Provider payload containing the word "completed" is not an execution terminal. Inspect the control frame. DeepSeek Harness uses Adapter ID `dsh`; use canonical mode unless the installed Adapter advertises native support. Registered metadata does not prove installation, authentication, model availability or equivalent capabilities.

## Context and Inputs

A Context is an execution/background envelope, not a cancellation token:

```json
{
  "schema": "octo.context-envelope/v3",
  "items": [
    {"namespace":"octo.execution","name":"limits","payload":{"run_timeout_ms":60000}},
    {"namespace":"octo.content","name":"inline","payload":{"entries":[{"text":"Background material"}]}}
  ]
}
```

The typed examples show importing `Context` or `ContextEnvelope` from each library. Rust's Context is a JSON value alias; Go's is a map type; Python exposes a TypedDict; TypeScript exposes structural types. Explicit items merge with the selected Adapter and default `ask` approval policy. Duplicate namespace/name pairs and conflicting Adapter types are rejected; OpenClaw's `provider_agent_id` is retained. Context has at most 32 items and 65,536 serialized UTF-8 bytes. Additional Adapter-specific semantics remain the Kernel's responsibility.

`inputs` contains ordered text and image parts. Images use PNG, JPEG or WebP, at most 2 MiB each. An image part contains `type`, `mimeType`, base64 `data`, original `byteSize` and a lowercase SHA-256 digest. The image helpers compute these from caller-supplied bytes. They do not fetch URLs, read files implicitly, or turn images into a text fallback. The selected Provider/model must support images.

## Recovery and Cancellation

If an acceptance ACK is lost or malformed after a possible write, query the original lookup key and observed Kernel instance with bounded attempts. **Never automatically replay invoke.** If acceptance cannot be established, return `RUN_OUTCOME_UNKNOWN` with the original lookup information.

Subscription reconnects use the same full ref and last validated cursor. Validate the snapshot's retained window, instance, run identity, consecutive sequence, and agreement between notification and HostFrame sequence. Gaps and identity changes fail. A consumed terminal cursor completes without replaying terminal output. This is bounded recovery over an in-memory Kernel cache, not durable execution storage.

Lost cancellation or interaction acknowledgements produce `OPERATION_OUTCOME_UNKNOWN`. Do not automatically replay a side effect. A cancellation ACK means only that the request was accepted; observe the terminal state for the outcome.

Closing a client, dropping an iterator or aborting observation releases local IO; it does not cancel accepted execution or stop the service. Retain the receipt and explicitly cancel when required. The playboards use execution deadlines to request cancellation and observe a short tail. The independent clients do not implement a managed invocation registry or automatic owner drain.

## Validation

Run unit/protocol tests locally, then test an already-running Kernel and authenticated Provider. The real-Provider smoke records canonical/native completion and receipt handling without committing raw output or credentials. Live Codex results do not establish support for every harness, Windows, recovery after service restart, or sustained load. No GitHub workflow or CI gate is introduced.
