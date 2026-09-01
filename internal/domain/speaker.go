package domain

// Speaker is a lightweight diarization label. Embedding is a fixed-length
// feature vector (e.g. pitch/MFCC-style summary stats) used for nearest-
// centroid clustering by internal/adapter/diarize.
type Speaker struct {
	ID        string
	Label     string // e.g. "Speaker 1"
	Embedding []float32
}
