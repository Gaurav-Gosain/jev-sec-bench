// Package metrics scores probabilistic classifiers.
//
// Every function takes probabilities in [0,1] alongside 0/1 labels of the same
// length. Threshold free measures (AUC, Brier, ECE) are reported next to
// threshold bound ones, because a model can order cases well while stating
// probabilities that sit at the wrong absolute level.
package metrics

import (
	"math"
	"sort"
)

// Confusion holds the four counts at one decision threshold.
type Confusion struct {
	Threshold      float64
	TP, FP, FN, TN int
}

// Total is the number of samples counted.
func (c Confusion) Total() int { return c.TP + c.FP + c.FN + c.TN }

// Accuracy is the share of samples put on the right side of the threshold.
func (c Confusion) Accuracy() float64 { return ratio(c.TP+c.TN, c.Total()) }

// Precision is the share of flagged samples that were positive.
func (c Confusion) Precision() float64 { return ratio(c.TP, c.TP+c.FP) }

// Recall is the share of positive samples that were flagged.
func (c Confusion) Recall() float64 { return ratio(c.TP, c.TP+c.FN) }

// F1 is the harmonic mean of precision and recall.
func (c Confusion) F1() float64 {
	p, r := c.Precision(), c.Recall()
	if p+r == 0 {
		return 0
	}
	return 2 * p * r / (p + r)
}

func ratio(num, den int) float64 {
	if den == 0 {
		return 0
	}
	return float64(num) / float64(den)
}

// Confuse counts outcomes treating every probability at or above threshold as a
// predicted positive.
func Confuse(probs []float64, labels []int, threshold float64) Confusion {
	c := Confusion{Threshold: threshold}
	for i, p := range probs {
		switch {
		case p >= threshold && labels[i] == 1:
			c.TP++
		case p >= threshold:
			c.FP++
		case labels[i] == 1:
			c.FN++
		default:
			c.TN++
		}
	}
	return c
}

// ROCAUC is the probability that a random positive scores above a random
// negative. It measures ordering only, so the threshold does not affect it.
//
// Ties are handled by averaging ranks, so a model that returns the same value
// for everything scores 0.5 rather than something better.
func ROCAUC(probs []float64, labels []int) float64 {
	type point struct {
		p float64
		y int
	}
	points := make([]point, len(probs))
	for i := range probs {
		points[i] = point{probs[i], labels[i]}
	}
	sort.Slice(points, func(i, j int) bool { return points[i].p < points[j].p })

	ranks := make([]float64, len(points))
	for start := 0; start < len(points); {
		stop := start
		for stop+1 < len(points) && points[stop+1].p == points[start].p {
			stop++
		}
		shared := float64(start+stop)/2 + 1
		for i := start; i <= stop; i++ {
			ranks[i] = shared
		}
		start = stop + 1
	}

	var positives, negatives int
	var rankSum float64
	for i, pt := range points {
		if pt.y == 1 {
			positives++
			rankSum += ranks[i]
			continue
		}
		negatives++
	}
	if positives == 0 || negatives == 0 {
		return math.NaN()
	}
	return (rankSum - float64(positives)*float64(positives+1)/2) /
		(float64(positives) * float64(negatives))
}

// Bin is one bucket of a reliability diagram: what the model claimed against
// what actually happened.
type Bin struct {
	Low, High  float64
	Count      int
	MeanProb   float64
	ActualRate float64
}

// Calibrate groups predictions into equal width buckets.
func Calibrate(probs []float64, labels []int, bins int) []Bin {
	out := make([]Bin, bins)
	for b := range bins {
		low, high := float64(b)/float64(bins), float64(b+1)/float64(bins)
		out[b] = Bin{Low: low, High: high}
		var sumProb, sumLabel float64
		for i, p := range probs {
			last := b == bins-1
			if (p >= low && p < high) || (last && p == 1) {
				out[b].Count++
				sumProb += p
				sumLabel += float64(labels[i])
			}
		}
		if out[b].Count > 0 {
			out[b].MeanProb = sumProb / float64(out[b].Count)
			out[b].ActualRate = sumLabel / float64(out[b].Count)
		}
	}
	return out
}

// ECE is the expected calibration error: the average gap between the stated
// probability and the observed rate, weighted by how many samples fall in each
// bucket. Zero means every stated probability matched reality.
func ECE(probs []float64, labels []int, bins int) float64 {
	if len(probs) == 0 {
		return math.NaN()
	}
	var total float64
	for _, b := range Calibrate(probs, labels, bins) {
		if b.Count == 0 {
			continue
		}
		total += float64(b.Count) / float64(len(probs)) * math.Abs(b.MeanProb-b.ActualRate)
	}
	return total
}

// Brier is the mean squared error of the probabilities. Lower is better.
func Brier(probs []float64, labels []int) float64 {
	if len(probs) == 0 {
		return math.NaN()
	}
	var total float64
	for i, p := range probs {
		diff := p - float64(labels[i])
		total += diff * diff
	}
	return total / float64(len(probs))
}

// BestF1 scans the observed probabilities and returns the threshold with the
// highest F1, along with that F1.
//
// This is an upper bound fitted on the same data, so report it next to the
// default threshold rather than instead of it.
func BestF1(probs []float64, labels []int) (threshold, f1 float64) {
	seen := make(map[float64]bool, len(probs))
	threshold, f1 = 0.5, 0
	for _, p := range probs {
		if seen[p] {
			continue
		}
		seen[p] = true
		if score := Confuse(probs, labels, p).F1(); score > f1 {
			threshold, f1 = p, score
		}
	}
	return threshold, f1
}

// Mean averages a slice, returning NaN when it is empty.
func Mean(values []float64) float64 {
	if len(values) == 0 {
		return math.NaN()
	}
	var total float64
	for _, v := range values {
		total += v
	}
	return total / float64(len(values))
}
