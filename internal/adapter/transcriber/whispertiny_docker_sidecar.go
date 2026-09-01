package transcriber

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"whisper-live/internal/usecase"
)

// WhisperTinyDockerSidecar reuses the existing docker-whisper-tiny image
// for inference by running `docker run` per window, mounting a temp WAV
// file written from the window's PCM16 samples and reading the resulting
// transcript from stdout. This gives parity with the existing batch
// transcription tool without requiring whisper.cpp bindings on the host,
// at the cost of one container start per window — acceptable for the
// server/CI deployment path (spec section 10: "server mode as a Docker
// image reusing the existing docker-whisper-tiny base"), less so for the
// desktop app's tight latency budget, where WhisperCppLocal is preferred.
type WhisperTinyDockerSidecar struct {
	// ImageName is the docker-whisper-tiny image tag, e.g.
	// "docker-whisper-tiny:latest".
	ImageName string
	// DockerBinary defaults to "docker" if empty.
	DockerBinary string
	Timeout      time.Duration
}

// NewWhisperTinyDockerSidecar builds an adapter targeting the given image.
func NewWhisperTinyDockerSidecar(imageName string) *WhisperTinyDockerSidecar {
	return &WhisperTinyDockerSidecar{
		ImageName:    imageName,
		DockerBinary: "docker",
		Timeout:      15 * time.Second,
	}
}

// Transcribe implements usecase.Transcriber.
func (w *WhisperTinyDockerSidecar) Transcribe(ctx context.Context, pcm16 []int16, sampleRate int, language string) (usecase.TranscribeResult, error) {
	if w.ImageName == "" {
		return usecase.TranscribeResult{}, fmt.Errorf("transcriber: WhisperTinyDockerSidecar has no ImageName configured")
	}
	dockerBin := w.DockerBinary
	if dockerBin == "" {
		dockerBin = "docker"
	}
	if _, err := exec.LookPath(dockerBin); err != nil {
		return usecase.TranscribeResult{}, fmt.Errorf("transcriber: docker binary not found: %w", err)
	}

	wavPath, cleanup, err := writeTempWAV(pcm16, sampleRate)
	if err != nil {
		return usecase.TranscribeResult{}, err
	}
	defer cleanup()

	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	mountDir := os.TempDir()
	args := []string{
		"run", "--rm",
		"-v", mountDir + ":/audio",
		w.ImageName,
		"--input", "/audio/" + baseName(wavPath),
	}
	if language != "" && language != "auto" {
		args = append(args, "--language", language)
	}

	cmd := exec.CommandContext(cctx, dockerBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return usecase.TranscribeResult{}, fmt.Errorf("transcriber: docker-whisper-tiny run failed: %w (stderr: %s)", err, stderr.String())
	}

	return usecase.TranscribeResult{Text: strings.TrimSpace(stdout.String())}, nil
}

func baseName(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[i+1:]
		}
	}
	return path
}
