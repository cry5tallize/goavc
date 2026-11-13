# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project status

Pure Go H.264/AVC encoder + decoder with **zero CGO**, stdlib-only runtime, and optional Go assembly (SSE2/AVX2/NEON) with mandatory scalar fallbacks. Module path: `github.com/goavc/h264` (root package is `h264`); `go.mod` declares Go 1.27 and CI uses 1.27.x.

Check actual code and tests before assuming an API or codec feature exists. Authoritative progress lives in `docs/implementation-status.md`; milestones and exit criteria live in `docs/Roadmap.md`; gate evidence is under `docs/gates/`. See `AGENTS.md` for the repository-wide working rules and PowerShell verification commands.

## Commands

常规构建、测试与工具运行必须设置 `CGO_ENABLED=0`；Makefile 与 verify.sh 已导出该值。唯一例外是开发期 `make race`：Go 竞态检测工具链需要 `CGO_ENABLED=1` 和 C 编译器；项目源码仍禁止导入 C，正常运行时保持纯 Go。

```bash
make verify            # full pre-commit gate: vet, build, test, deps, manifest
./scripts/verify.sh    # equivalent when make is unavailable (e.g. Git Bash on Windows)
make build             # compile all packages, including tools/ and benchmarks/
make test              # CGO_ENABLED=0 go test ./...
make race              # CGO_ENABLED=1 go test -race ./... (M3 gate; needs C compiler)
make vet
make fmt               # gofmt -w .
make fmt-check         # fails if any file is not gofmt-clean (PR gate)
make cross             # compile six GOOS/GOARCH combinations (compile-only proof)
make bench             # benchmarks (requires reproducible idle hardware)
```

Single test / package loop:

```bash
CGO_ENABLED=0 go test -run TestCAVLC -v ./internal/entropy/cavlc
CGO_ENABLED=0 go test -bench BenchmarkFrame -benchmem ./benchmarks
```

Tooling (all run in CI):

```bash
go run ./tools/depscheck                                    # R1/R2/R3/R5 import-graph rules; exit 1 on violation
go run ./tools/manifest -file testdata/manifest.json        # corpus schema, enums, file presence, SHA-256
go run ./tools/oracle -input clip.264 -ref clip.yuv -width 352 -height 288 -pixfmt yuv420p
```

Oracle exit codes matter: `0` identical, `2` pixel mismatch, `3` ffmpeg not installed (treat as skipped, not failed), `4` tool/environment error. FFmpeg is development-only and must never become a runtime dependency.

## Architecture

Data flows strictly downward:

1. **Public API** — root package `h264`: `Frame`/`Plane`/`PixelFormat`, `Packet`, `EncoderConfig`/`DecoderConfig`, sentinel errors.
2. **Pipelines** — `decoder/`, `encoder/`, `stream/`.
3. **Bitstream/syntax** — `bitstream/` (bit reader/writer, Exp-Golomb, RBSP/EBSP), `nal/` (Annex-B, AVCC, avcC, access units), `syntax/` (SPS/PPS/VUI/ScalingList/SEI/slice headers).
4. **Shared codec core** — `internal/picture`, `prediction`, `transform`, `motion` (encoder-only), `filter`, `dpb`, `entropy/cavlc`, `entropy/cabac`, `simd`, `buffer`.
5. **Test infrastructure** — `internal/testutil/{importgraph,manifest,yuvdiff}` and `tools/`.

### Dependency rules (docs/architecture.md)

- **R1**: `encoder` and `decoder` must never import each other.
- **R2**: `internal/*` must never import `decoder`, `encoder`, or `stream`.
- **R3**: only whitelisted packages may import `internal/simd` (`prediction`, `transform`, `motion`, `filter`, `buffer`, `simd`, `decoder`, `encoder`). Adding a package to this list is a deliberate dependency review; update the whitelist in `internal/testutil/importgraph/graph.go`.
- **R4**: public API must not expose internal types (review + future API snapshot).
- **R5**: no import cycles.
- **R6**: no giant files (~≤800 effective lines); review-enforced.

`tools/depscheck` automatically blocks R1/R2/R3/R5 violations in CI.

### Non-negotiable design invariants

- **One implementation per shared kernel.** Prediction, transform (both directions), inverse quantization, reconstruction, deblocking, and picture buffers are shared by encoder and decoder. The encoder's local reconstruction must call exactly the same kernels the decoder uses. Writing a second implementation "for cross-validation" is forbidden — the two drift and produce progressively divergent reference frames.
- **Display vs coded dimensions.** `Frame.Width/Height` are display dimensions and may be odd. Internal picture buffers are macroblock-aligned (16), edge-replicated, and cropped via the SPS crop window on output/padded on input. Plane geometry always rounds up; never assume even dimensions.
- **Memory ownership.** `SendFrame` may retain a frame until consumed; the caller may reuse it after Send returns. `ReceiveFrame` returns a caller-owned frame that must be `Release()`d (idempotent, invalidates the frame); use `Clone()` for long-lived copies (not pooled). `SendPacket` does not retain `Data` after return; `ReceivePacket` data is caller-owned.
- **Error semantics.** Always use `errors.Is`. `ErrAgain` is flow control ("no output yet"), not an error. `ErrEOF` is `io.EOF`. `ErrInvalidData` is fatal input corruption (reset and wait for IDR). `ErrUnsupported` means legal-but-unimplemented and must be returned explicitly — never silently degrade or fake support. `ErrResourceLimit` means a configured/derived bound was exceeded. `IsFatal` classifies the fatal ones.
- **Concurrency.** `Encoder`/`Decoder` instances are not concurrency-safe; parallel streams use separate instances. Any internal parallelization must produce bit-identical output to the serial path and pass `-race`.
- **SIMD.** Dispatch is selected once at package init. Assembly files live only in `internal/simd`. Every assembly kernel requires a pure-Go scalar implementation and element-wise parity tests, including non-aligned strides, odd sizes, and extreme inputs. The generic path must remain fully functional on every architecture.
- **Untrusted input.** Everything derived from a bitstream must be bounds-checked against configured limits before allocation; overflow checks are mandatory for size arithmetic.

## Conventions

- **Commit messages**: `type[module]: lowercase english subject`, ≤72 characters, no trailing period. Allowed types and module vocabulary are in `docs/Implementation-Plan.md` §1. The `commit-msg` hook blocks malformed subjects; enable with `git config core.hooksPath .githooks` (already configured in this checkout). Subjects are English; commit bodies may be English or Chinese, consistent within one commit.
- **Comments and docs are Chinese** and must be substantive: cite the spec clause (e.g. `Spec: 8.5.10`), invariants, edge cases, and *why* the code is written that way. Comments that merely restate code are rejected.
- **Status tracking in the same change**: any change in support status updates both `docs/implementation-status.md` (Translation Ledger) and `docs/supported-features.md` (Feature Matrix). `Conformant` requires Oracle or analytic-spec evidence; self-round-trip alone caps at `Partial`.
- **Atomic commits**: one logical change per commit; separate `fix` commits from features; no TODO without an issue link; new behavior needs tests in the same commit; performance changes need before/after benchmark data.
- **Generated code**: `tables_gen.go` is produced by `gen_tables.go` (`//go:build ignore`). Never hand-edit generated output; regenerate instead.
- **Naming**: multiword files use snake_case; platform suffixes `_amd64.go`, `_arm64.go`, `_generic.go`, `_amd64.s`; tests `TestSubject_Behavior`, benchmarks `BenchmarkKernel_Size_Backend`.
- **Dependencies**: `go.mod` must have no `require` before v1.0; no CGO, no C/C++, no external codec libraries. `unsafe` only when a PR justifies it and documents the bounds checks.
- **Milestone scope is a hard boundary.** Read the current milestone in `docs/Roadmap.md` before implementing; out-of-scope features must return `ErrUnsupported` rather than being partially implemented.

## Documentation map

| File | Role |
|---|---|
| `docs/Roadmap.md` | Single source of truth for milestones, scope, and exit criteria |
| `docs/Implementation-Plan.md` | Commit-by-commit plan; commit/naming rules |
| `docs/architecture.md` | Layering, R1–R6, ownership, error and concurrency model |
| `docs/supported-features.md` | Feature Matrix (per-tool support status) |
| `docs/implementation-status.md` | Translation Ledger (per-module status, tests, known gaps) |
| `docs/performance.md` | Benchmark protocol, hardware registration, results |
| `docs/licensing.md`, `PATENTS.md` | Code license (MIT) and H.264 patents are separate matters |
| `testdata/README.md` | Corpus naming, manifest schema, status semantics |
