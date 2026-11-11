# Compatibility and Verification

## Protocol Target

This independent Go implementation targets [Octo Runtime](https://github.com/Mininglamp-OSS/octo-runtime) Kernel IPC v2, Run contract v7 and AgentEnv envelope v3. The generated identity subset is recorded in [the client contract](protocol-contract.json) and [generation provenance](protocol-provenance.json). See [protocol design](protocol.md) for the service boundary and handwritten transport behavior.

Runtime is installed and started independently. No upstream source checkout, SDK package, protocol download or code generator is needed to build or run the library. Matching IPC versions alone does not establish full schema parity; rerun local verification when upgrading Runtime.

## Local Evidence: 2026-10-31

Platform: macOS arm64. Toolchain: Go 1.26.1, Node.js 24.22.2 (verification host only).

- `go test -race ./...`: all five top-level test groups passed, including four stream identity/cursor scenarios; examples compiled.
- `go vet ./...`: passed.
- `go build -o .tmp/playboard ./cmd/playboard`: passed.
- Go module dependencies: standard library only.

The independent Unix socket test peer exercises fragmented responses, lost invoke ACK recovery without resubmission, terminal streams, sequence gaps, changed run/Kernel identities, non-replayed cancellation, Context merging, image metadata and malformed JSON-RPC responses.

## Real Codex and Existing Kernel

[Machine-readable smoke evidence](live-smoke.json) records two completed real Codex invocations, one canonical and one native, against an already-running Kernel. Both verify receipt lookup, exact full-output replay, and completion when resubscribing from the consumed terminal cursor. Canonical output contains the requested response marker.

[Example evidence](examples-verification.json) records offline text/image construction in both modes, a completed real Codex call combining Context with text and PNG image input, a completed context-aware convenience invocation, and a successful concurrent example with two independent Codex calls.

The image test verifies accepted multimodal execution and completion; it does not benchmark visual understanding. Concurrency here uses two Codex invocations. Claude Code, OpenClaw and DeepSeek Harness were not exercised live.

The smoke uses caller-owned service and Provider authentication, creates private receipts under ignored `.tmp/`, and leaves the Kernel running. Public evidence records counts and outcomes without raw Provider output, credentials or machine-specific socket paths.

## Reproduce

Follow the root README to install/start Runtime, authenticate Codex and set the actual `OCTO_KERNEL_ENDPOINT`. Then run:

```sh
go test -race ./...
go vet ./...
go build -o .tmp/playboard ./cmd/playboard
node scripts/live-smoke.mjs "$OCTO_KERNEL_ENDPOINT"
```

Use [the examples](../examples/README.md) to repeat AgentEnv, image and concurrent calls. Live checks execute your installed Provider and require its working authentication.

## Scope

Verified on this macOS arm64 machine only. Linux is a transport target but was not tested here. Windows named pipes are unsupported. Live interaction approval round trips, other Providers, service restart recovery and sustained-load behavior remain unverified. These small clients do not implement a managed invocation registry, automatic owner drain or every upstream validator.

Verification is local and explicitly invoked. No GitHub workflows, jobs, matrices, triggers, required checks or CI gates are added.

## AgentEnv Migration Verification (2026-11-11)

Aligned with upstream `c9d3bb6a642abce3177ac28cf9f26993909c3d32` (#352). Local Go tests passed for all packages and examples. Canonical/native dry-run outputs from all three clients, OpenClaw request examples, AgentEnv files and a minimal request passed 13 checks using `assertInvokeInput` compiled from that exact upstream commit in a temporary directory.

The identity contract now targets Kernel IPC v2. Run contract v7 and RunRef/RPC names remain the same. This migration changes the handwritten invocation boundary to `inputs` + `agentEnv` and removes the old Context APIs. Live verification on 2026-11-11 against the existing IPC v2 Kernel completed both canonical and native Codex runs using full request JSON: two ordered text parts and explicit AgentEnv Adapter, prepared workspace, limits and private settings. Both modes returned the expected response marker and completed; receipt lookup, exact frame replay and consumed-terminal cursor checks passed. Results are recorded in [live-smoke.json](live-smoke.json). Historical `examples-verification.json` predates this migration. GitHub workflows and CI gates were not changed.
