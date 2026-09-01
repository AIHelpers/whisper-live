// Package config centralizes server configuration, loaded from flags/env
// so cmd/server, cmd/cli, and cmd/desktop can all share one shape.
package config

import (
	"os"
	"strconv"
)

// Config holds every runtime-tunable value the adapters need.
type Config struct {
	// Addr is the HTTP/WS listen address, e.g. ":8080".
	Addr string

	// TranscriberBackend selects which usecase.Transcriber adapter to
	// wire up: "mock" (default, no external deps), "whispercpp" (local
	// subprocess via whisper.cpp CLI), or "docker" (docker-whisper-tiny
	// sidecar).
	TranscriberBackend string
	WhisperCppBinary   string
	WhisperCppModel    string
	DockerImage        string

	// EnableVAD toggles energy-based silence skipping.
	EnableVAD bool
	// EnableDiarization toggles lightweight speaker clustering.
	EnableDiarization bool

	DefaultLanguage string
}

// FromEnv loads config from environment variables, falling back to
// sensible defaults for local/dev use. Env var names are prefixed
// WHISPERLIVE_.
func FromEnv() Config {
	return Config{
		Addr:               envOr("WHISPERLIVE_ADDR", ":8080"),
		TranscriberBackend: envOr("WHISPERLIVE_TRANSCRIBER", "mock"),
		WhisperCppBinary:   envOr("WHISPERLIVE_WHISPERCPP_BIN", ""),
		WhisperCppModel:    envOr("WHISPERLIVE_WHISPERCPP_MODEL", ""),
		DockerImage:        envOr("WHISPERLIVE_DOCKER_IMAGE", "docker-whisper-tiny:latest"),
		EnableVAD:          envBoolOr("WHISPERLIVE_ENABLE_VAD", true),
		EnableDiarization:  envBoolOr("WHISPERLIVE_ENABLE_DIARIZATION", false),
		DefaultLanguage:    envOr("WHISPERLIVE_LANGUAGE", "auto"),
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envBoolOr(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}
