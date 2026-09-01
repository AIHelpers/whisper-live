// Package diarize provides a lightweight usecase.Diarizer: no ML model,
// just per-segment acoustic summary "embeddings" (pitch/energy statistics)
// clustered against running per-speaker centroids by cosine similarity.
// This is intentionally not full diarization (spec section 4 calls it out
// as "lightweight... without needing full diarization pipelines") — good
// enough to tell "probably a different speaker" apart in a two/three
// person conversation, not to identify individuals across sessions.
package diarize

import (
	"context"
	"fmt"
	"math"
	"sync"
)

const embeddingDim = 4 // [meanEnergy, energyVariance, zeroCrossingRate, meanAbsDelta]

// EmbeddingCluster assigns speaker labels by nearest-centroid clustering
// on a simple 4-dimensional acoustic feature vector computed per segment.
type EmbeddingCluster struct {
	// SimilarityThreshold: a segment whose best-matching centroid scores
	// below this cosine similarity is assumed to be a *new* speaker rather
	// than a repeat of the closest existing one.
	SimilarityThreshold float64
	// MaxSpeakers caps how many distinct speaker labels will be created;
	// beyond this, segments are assigned to the nearest existing centroid
	// regardless of similarity.
	MaxSpeakers int

	mu        sync.Mutex
	centroids [][embeddingDim]float64
	counts    []int
	nextLabel int
}

// NewEmbeddingCluster builds a clusterer with sensible defaults.
func NewEmbeddingCluster() *EmbeddingCluster {
	return &EmbeddingCluster{
		SimilarityThreshold: 0.90,
		MaxSpeakers:         8,
	}
}

// Assign implements usecase.Diarizer.
func (c *EmbeddingCluster) Assign(ctx context.Context, pcm16 []int16, sampleRate int) (string, error) {
	if len(pcm16) == 0 {
		return "", fmt.Errorf("diarize: empty audio window")
	}
	feat := extractFeatures(pcm16)

	c.mu.Lock()
	defer c.mu.Unlock()

	bestIdx := -1
	bestSim := -math.MaxFloat64
	for i, cen := range c.centroids {
		sim := cosineSimilarity(cen, feat)
		if sim > bestSim {
			bestSim = sim
			bestIdx = i
		}
	}

	if bestIdx >= 0 && (bestSim >= c.SimilarityThreshold || len(c.centroids) >= c.MaxSpeakers) {
		// Update the matched centroid as a running mean (cheap online
		// clustering — no need to store every past embedding).
		n := c.counts[bestIdx]
		for d := 0; d < embeddingDim; d++ {
			c.centroids[bestIdx][d] = (c.centroids[bestIdx][d]*float64(n) + feat[d]) / float64(n+1)
		}
		c.counts[bestIdx]++
		return fmt.Sprintf("Speaker %d", bestIdx+1), nil
	}

	// New speaker.
	c.centroids = append(c.centroids, feat)
	c.counts = append(c.counts, 1)
	label := fmt.Sprintf("Speaker %d", len(c.centroids))
	return label, nil
}

func extractFeatures(pcm16 []int16) [embeddingDim]float64 {
	n := float64(len(pcm16))

	var sumSq, sumAbsDelta float64
	var zeroCrossings int
	prev := float64(pcm16[0])
	for i, s := range pcm16 {
		f := float64(s)
		sumSq += f * f
		if i > 0 {
			sumAbsDelta += math.Abs(f - prev)
			if (f >= 0) != (prev >= 0) {
				zeroCrossings++
			}
		}
		prev = f
	}
	meanEnergy := sumSq / n

	var varSum float64
	for _, s := range pcm16 {
		f := float64(s)
		d := f*f - meanEnergy
		varSum += d * d
	}
	energyVariance := varSum / n
	zcr := float64(zeroCrossings) / n
	meanAbsDelta := sumAbsDelta / n

	return [embeddingDim]float64{
		math.Sqrt(meanEnergy),
		math.Sqrt(energyVariance),
		zcr,
		meanAbsDelta,
	}
}

func cosineSimilarity(a, b [embeddingDim]float64) float64 {
	var dot, na, nb float64
	for i := 0; i < embeddingDim; i++ {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}
