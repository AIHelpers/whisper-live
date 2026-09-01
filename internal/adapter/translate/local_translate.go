// Package translate provides usecase.Translator implementations for the
// "live translation overlay" feature (spec section 4).
package translate

import (
	"context"
	"fmt"
)

// Local is a placeholder for a local/on-device translation model
// (e.g. a small NLLB/M2M100 ONNX model run in-process). Wire a real model
// runner into Translate; the interface is intentionally minimal so any
// backend can satisfy it.
type Local struct {
	// ModelPath to a local translation model, if/when wired up.
	ModelPath string
}

// NewLocal builds a Local translator targeting the given model path.
func NewLocal(modelPath string) *Local {
	return &Local{ModelPath: modelPath}
}

// Translate implements usecase.Translator.
func (l *Local) Translate(ctx context.Context, text, sourceLang, targetLang string) (string, error) {
	if l.ModelPath == "" {
		return "", fmt.Errorf("translate: Local has no ModelPath configured")
	}
	return "", fmt.Errorf("translate: Local model runner not implemented — configure Remote for now")
}
