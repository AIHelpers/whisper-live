package transcriber

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"whisper-live/internal/usecase"
)

// WhisperCppLocal runs the whisper.cpp CLI (the `main`/`whisper-cli`
// binary produced by github.com/ggerganov/whisper.cpp) as a subprocess
// against a WAV file written from each window's PCM16 samples.
//
// The dev plan's "OR migrated to whisper.cpp's Go bindings" option
// (github.com/ggerganov/whisper.cpp/bindings/go) is the lower-latency,
// in-process alternative to this subprocess approach — it avoids a
// process-spawn per window, which matters once windows fire every
// DefaultHopMS (2s). That binding is cgo-based and requires the compiled
// libwhisper static library at build time, which this sandbox cannot
// fetch/build (no network access to the model/toolchain), so this
// subprocess adapter is the portable default: it works with any
// whisper.cpp build the operator already has installed, and the two
// share the exact same usecase.Transcriber interface, so switching
// between them is a one-line change in cmd/server wiring.
type WhisperCppLocal struct {
	// BinaryPath is the path to whisper.cpp's CLI binary (e.g. "main" or
	// "whisper-cli").
	BinaryPath string
	// ModelPath is the path to a ggml model file (e.g. "ggml-tiny.bin").
	ModelPath string
	// Timeout bounds each subprocess call so a stuck window can't stall
	// the pipeline forever.
	Timeout time.Duration
}

// NewWhisperCppLocal builds an adapter targeting the given binary/model.
func NewWhisperCppLocal(binaryPath, modelPath string) *WhisperCppLocal {
	return &WhisperCppLocal{
		BinaryPath: binaryPath,
		ModelPath:  modelPath,
		Timeout:    10 * time.Second,
	}
}

// Transcribe implements usecase.Transcriber.
func (w *WhisperCppLocal) Transcribe(ctx context.Context, pcm16 []int16, sampleRate int, language string) (usecase.TranscribeResult, error) {
	if w.BinaryPath == "" || w.ModelPath == "" {
		return usecase.TranscribeResult{}, fmt.Errorf("transcriber: WhisperCppLocal not configured (binary/model path empty)")
	}
	if _, err := os.Stat(w.BinaryPath); err != nil {
		return usecase.TranscribeResult{}, fmt.Errorf("transcriber: whisper.cpp binary not found at %s: %w", w.BinaryPath, err)
	}

	wavPath, cleanup, err := writeTempWAV(pcm16, sampleRate)
	if err != nil {
		return usecase.TranscribeResult{}, err
	}
	defer cleanup()

	timeout := w.Timeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	args := []string{"-m", w.ModelPath, "-f", wavPath, "-nt", "-np"}
	if language != "" && language != "auto" {
		args = append(args, "-l", language)
	}
	cmd := exec.CommandContext(cctx, w.BinaryPath, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return usecase.TranscribeResult{}, fmt.Errorf("transcriber: whisper.cpp exec failed: %w (stderr: %s)", err, stderr.String())
	}

	text := strings.TrimSpace(stdout.String())
	return usecase.TranscribeResult{Text: text, Confidence: 0}, nil
}

// writeTempWAV writes mono 16-bit PCM samples to a temp .wav file and
// returns its path plus a cleanup func.
func writeTempWAV(pcm16 []int16, sampleRate int) (string, func(), error) {
	f, err := os.CreateTemp("", "whisper-live-*.wav")
	if err != nil {
		return "", func() {}, fmt.Errorf("transcriber: temp file: %w", err)
	}
	path := f.Name()
	cleanup := func() { os.Remove(path) }

	if err := writeWAV(f, pcm16, sampleRate); err != nil {
		f.Close()
		cleanup()
		return "", func() {}, err
	}
	if err := f.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	return path, cleanup, nil
}

// writeWAV writes a minimal canonical PCM WAV (mono, 16-bit) header
// followed by the sample data — no external audio library needed.
func writeWAV(f *os.File, pcm16 []int16, sampleRate int) error {
	const (
		numChannels   = 1
		bitsPerSample = 16
	)
	byteRate := sampleRate * numChannels * bitsPerSample / 8
	blockAlign := numChannels * bitsPerSample / 8
	dataSize := len(pcm16) * 2
	riffSize := 36 + dataSize

	var hdr bytes.Buffer
	hdr.WriteString("RIFF")
	binary.Write(&hdr, binary.LittleEndian, uint32(riffSize))
	hdr.WriteString("WAVE")
	hdr.WriteString("fmt ")
	binary.Write(&hdr, binary.LittleEndian, uint32(16)) // PCM fmt chunk size
	binary.Write(&hdr, binary.LittleEndian, uint16(1))  // PCM
	binary.Write(&hdr, binary.LittleEndian, uint16(numChannels))
	binary.Write(&hdr, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&hdr, binary.LittleEndian, uint32(byteRate))
	binary.Write(&hdr, binary.LittleEndian, uint16(blockAlign))
	binary.Write(&hdr, binary.LittleEndian, uint16(bitsPerSample))
	hdr.WriteString("data")
	binary.Write(&hdr, binary.LittleEndian, uint32(dataSize))

	if _, err := f.Write(hdr.Bytes()); err != nil {
		return fmt.Errorf("transcriber: write wav header: %w", err)
	}
	samples := make([]byte, dataSize)
	for i, s := range pcm16 {
		binary.LittleEndian.PutUint16(samples[i*2:], uint16(s))
	}
	if _, err := f.Write(samples); err != nil {
		return fmt.Errorf("transcriber: write wav samples: %w", err)
	}
	return nil
}
