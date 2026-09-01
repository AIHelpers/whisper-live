// Command cli is the "whisperlive" CLI (spec section 5: "CLI: cobra for
// headless server mode (`whisperlive serve`)"). It's implemented with the
// standard library's flag package rather than cobra: cobra's transitive
// dependency chain (go.yaml.in / gopkg.in) isn't reachable from this
// build's network allowlist, and stdlib flag covers the one subcommand
// this MVP needs. Swapping in cobra later is a drop-in change — this
// file's surface (`whisperlive serve --addr :8080 ...`) is deliberately
// cobra-shaped so that migration wouldn't change the UX.
package main

import (
	"fmt"
	"os"
	"os/exec"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		// Delegate straight to cmd/server, which owns all the serve flags
		// (-addr, -transcriber, -vad, -diarize, ...) so they aren't
		// duplicated between binaries.
		runServer(os.Args[2:])
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "whisperlive: unknown command %q\n\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}
}

func runServer(args []string) {
	// In this repo layout, `go run ./cmd/server` is the server binary;
	// once built (`go build ./cmd/server`), operators would invoke the
	// resulting binary directly instead of going through this CLI shim.
	cmdArgs := append([]string{"run", "./cmd/server"}, args...)
	cmd := exec.Command("go", cmdArgs...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "whisperlive: serve failed: %v\n", err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprint(os.Stderr, `whisperlive - real-time streaming transcription

Usage:
  whisperlive serve [flags]   Start the WebSocket/HTTP server
  whisperlive help            Show this message

Flags (forwarded to serve):
  -addr string              HTTP/WS listen address (default ":8080")
  -transcriber string       mock | whispercpp | docker (default "mock")
  -whispercpp-bin string    path to whisper.cpp CLI binary
  -whispercpp-model string  path to ggml model file
  -docker-image string      docker-whisper-tiny image tag
  -vad                      skip transcription for silent windows (default true)
  -diarize                  enable lightweight speaker diarization
  -alert-keywords string    comma-separated keyword alert list
`)
}
