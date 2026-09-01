# whisper-live

Real-time streaming transcription, built as a clean-architecture Go
service per the dev plan in `05-whisper-live.md`. This implements the
MVP (milestones 1–3 in full, plus scaffolding for 4–10): a WebSocket
server that ingests live PCM audio, runs it through a sliding-window
buffer + VAD gate + transcriber + overlap-reconciliation pipeline, and
streams back incremental captions — plus session export (SRT/VTT/plain
text) and a browser-based live-caption client.

## What actually works, end-to-end, in this build

- **The whole streaming pipeline**: `SlidingWindowBuffer` → VAD gate →
  `Transcriber` → `OverlapReconciler` → `SegmentSink`, exercised by real
  unit + integration tests (`go test ./... -race`).
- **The overlap-reconciliation algorithm** — the piece the spec calls out
  as "the critical correctness path" — stitches overlapping window
  transcripts into one coherent, de-duplicated running transcript. See
  `internal/usecase/finalize_segment.go` and its test file for the exact
  model (a "confirm on next overlap" two-stage pending→stable design).
- **The WebSocket protocol**: `WS /ws/sessions/:id/stream`. Send
  `{"type":"start","track":"mic","sampleRate":16000,"language":"en"}`,
  then binary PCM16LE mono frames, then `{"type":"stop"}`. You get back
  JSON `segment` events (interim and final), tagged with track/speaker/
  timestamps.
- **The HTTP API**: `POST /api/sessions`, `GET
  /api/sessions/:id/transcript.{srt,vtt,txt}`, `POST
  /api/sessions/:id/summarize` (501 unless a `Summarizer` backend is
  wired in — none ships by default).
- **A browser-based live-caption client** at `frontend/web/index.html`,
  served by the Go server itself at `/`. It captures mic audio via the
  Web Audio API, downsamples to 16kHz PCM16, streams it over the exact
  protocol above, and renders interim/final captions with a level meter
  and SRT/VTT/TXT export links.
- **A deterministic mock transcriber** (`internal/adapter/transcriber/mock.go`)
  so the entire pipeline is testable/runnable with zero external
  dependencies, no model download, no GPU.

Run it:

```bash
go run ./cmd/server -addr :8080 -transcriber mock
# open http://localhost:8080 in a browser, click "Start captioning"
```

## What's stubbed, and why

The original dev plan calls for a native **Wails desktop app** with
**malgo**-based system/mic audio capture and **in-process whisper.cpp
inference** via cgo bindings. None of those are buildable in this
sandbox:

- **Wails desktop shell**: requires a native GUI toolchain (WebView2/
  GTK/Cocoa) this sandbox doesn't have. The browser client at
  `frontend/web/` is a faithful stand-in for the *live-caption UI* half
  of the desktop app's job — same protocol, same captioning UX — but
  isn't the packaged, always-on-top desktop window the plan describes.
  `cmd/desktop/` is intentionally left unwritten rather than filled with
  Wails boilerplate that can't be built or run here.
- **`malgo` mic/system capture** (`internal/adapter/audiosource/malgo_capture.go`):
  needs cgo + a platform audio backend. The type is defined with the
  exact `usecase.AudioSource` shape a real implementation would fill in;
  right now it returns a clear error directing you to the WebSocket PCM
  path instead (which is what the browser client uses, and works today).
- **In-process whisper.cpp Go bindings**: also cgo, and needs a compiled
  `libwhisper` + model weights this sandbox can't fetch (network is
  allowlisted to package registries only, not model hosts). Instead,
  `internal/adapter/transcriber/whispercpp_local.go` shells out to a
  whisper.cpp CLI binary you point it at (`-whispercpp-bin`,
  `-whispercpp-model`), and `whispertiny_docker_sidecar.go` calls the
  existing `docker-whisper-tiny` image via `docker run`. Both satisfy the
  same `usecase.Transcriber` interface as the mock, so switching backends
  is a one-line change in `cmd/server/main.go`'s `buildTranscriber`.
- **`cobra` CLI**: the spec asks for cobra, but its transitive
  dependencies (`go.yaml.in`, `gopkg.in`) aren't reachable from this
  sandbox's network allowlist (only `proxy.golang.org`-style mirrors
  and direct GitHub/npm/pip package hosts are open). `cmd/cli/main.go`
  is written in the same `whisperlive serve [flags]` shape cobra would
  produce, using the standard library's `flag` package instead — a
  drop-in swap later.
- **Live translation, meeting summarization, model auto-scaling**: the
  `usecase.Translator` and `usecase.Summarizer` ports and their adapter
  shells (`internal/adapter/translate/`) exist and are wired into
  `SummarizeSession`, but no default backend ships (translation needs a
  model or paid API; summarization needs an LLM backend) — the HTTP
  summarize endpoint returns 501 until you plug one in.
- **Speaker diarization** (`internal/adapter/diarize/embedding_cluster.go`)
  is real and runs (simple RMS/zero-crossing-rate feature clustering,
  not a trained embedding model) — genuinely "lightweight," per spec,
  not a stub, but don't expect ML-grade speaker ID from it.

## Layout

Matches the dev plan's section 6 exactly:

```
cmd/{server,cli,desktop}/     entrypoints (desktop/ left for a Wails build)
internal/domain/              Session, TranscriptSegment, Speaker, AlertRule
internal/usecase/             ports.go + the sliding-window/reconciliation/
                               export/summarize interactors — zero adapter deps
internal/adapter/
  transcriber/                mock, whisper.cpp-subprocess, docker-sidecar
  vad/                        energy-based VAD
  diarize/                    lightweight embedding clustering
  translate/                  local (stub) + remote (HTTP) translators
  audiosource/                malgo capture (stub) + AudioProxy sink
  ws/                         WebSocket handler + protocol types
  httpapi/                    session/export/summarize HTTP endpoints
  alert/                      keyword-spotting notifier (log + webhook)
internal/infra/{config,logger}
frontend/web/                 browser live-caption client
```

## Testing

```bash
go test ./... -race            # everything, race detector on
go test ./internal/usecase/... -run Reconcile -v   # the critical-path algorithm
go test ./internal/adapter/ws/... -run TestLoad -v # concurrent-session soak test
```

Covers, per spec section 12:
- Unit tests for overlap reconciliation, with synthetic overlapping
  transcript fixtures (`finalize_segment_test.go`).
- Sliding-window buffer unit tests (`ingest_audio_chunk_test.go`).
- An integration test feeding a scripted "audio" stream through the full
  pipeline and asserting the reconciled transcript is correct
  (`integration_test.go`) — using a scripted fake transcriber rather
  than a real WAV fixture + WER threshold, since no real model runs in
  this sandbox; the harness (`AudioChunk` → `SessionRunner` →
  `FinalSegments()`) is exactly what a real-WAV version would use.
- A load test opening 40 concurrent WebSocket sessions and checking for
  goroutine leaks after teardown (`internal/adapter/ws/load_test.go`).

## Docker

```bash
docker build -t whisper-live .
docker run -p 8080:8080 whisper-live
```

See `Dockerfile` for notes on wiring in a real `docker-whisper-tiny`
sidecar or a baked-in whisper.cpp binary instead of the default mock
backend.
