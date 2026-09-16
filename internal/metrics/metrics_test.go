package metrics_test

import (
	"math"
	"testing"

	"github.com/Gaurav-Gosain/jev-sec-bench/internal/metrics"
)

const tolerance = 1e-9

func close(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %v; want %v", name, got, want)
	}
}

func TestConfusionCounts(t *testing.T) {
	t.Parallel()
	probs := []float64{0.9, 0.8, 0.4, 0.1}
	labels := []int{1, 0, 1, 0}

	c := metrics.Confuse(probs, labels, 0.5)
	if c.TP != 1 || c.FP != 1 || c.FN != 1 || c.TN != 1 {
		t.Fatalf("counts = %+v", c)
	}
	close(t, "Accuracy", c.Accuracy(), 0.5)
	close(t, "Precision", c.Precision(), 0.5)
	close(t, "Recall", c.Recall(), 0.5)
	close(t, "F1", c.F1(), 0.5)
}

func TestConfusionAtThresholdBoundary(t *testing.T) {
	t.Parallel()
	// A probability exactly at the threshold counts as a predicted positive.
	c := metrics.Confuse([]float64{0.5}, []int{1}, 0.5)
	if c.TP != 1 {
		t.Errorf("a probability equal to the threshold should be a positive: %+v", c)
	}
}

func TestPerfectAndInvertedSeparation(t *testing.T) {
	t.Parallel()
	probs := []float64{0.1, 0.2, 0.8, 0.9}

	close(t, "perfect AUC", metrics.ROCAUC(probs, []int{0, 0, 1, 1}), 1.0)
	close(t, "inverted AUC", metrics.ROCAUC(probs, []int{1, 1, 0, 0}), 0.0)
}

func TestAUCWithAllTiesIsChance(t *testing.T) {
	t.Parallel()
	probs := []float64{0.5, 0.5, 0.5, 0.5}
	close(t, "tied AUC", metrics.ROCAUC(probs, []int{1, 0, 1, 0}), 0.5)
}

func TestAUCMatchesHandCount(t *testing.T) {
	t.Parallel()
	// Positives at 0.6 and 0.4; negatives at 0.5 and 0.1.
	// Pairs won by the positive: (0.6,0.5), (0.6,0.1), (0.4,0.1). Lost: (0.4,0.5).
	probs := []float64{0.6, 0.4, 0.5, 0.1}
	labels := []int{1, 1, 0, 0}
	close(t, "AUC", metrics.ROCAUC(probs, labels), 0.75)
}

func TestAUCIsNaNWithOneClass(t *testing.T) {
	t.Parallel()
	if got := metrics.ROCAUC([]float64{0.3, 0.7}, []int{1, 1}); !math.IsNaN(got) {
		t.Errorf("AUC with no negatives = %v; want NaN", got)
	}
}

func TestPerfectCalibrationHasNoError(t *testing.T) {
	t.Parallel()
	// Ten samples at 0.5, five of them positive: the claim matches reality.
	probs := make([]float64, 10)
	labels := make([]int, 10)
	for i := range probs {
		probs[i] = 0.5
		if i < 5 {
			labels[i] = 1
		}
	}
	close(t, "ECE", metrics.ECE(probs, labels, 10), 0)
}

func TestOverconfidenceShowsAsCalibrationError(t *testing.T) {
	t.Parallel()
	// The model claims 0.9 every time but is right half the time.
	probs := make([]float64, 10)
	labels := make([]int, 10)
	for i := range probs {
		probs[i] = 0.9
		if i < 5 {
			labels[i] = 1
		}
	}
	close(t, "ECE", metrics.ECE(probs, labels, 10), 0.4)
}

func TestCalibrationBinsCoverEveryone(t *testing.T) {
	t.Parallel()
	probs := []float64{0, 0.25, 0.5, 0.75, 1.0}
	labels := []int{0, 0, 1, 1, 1}

	var counted int
	for _, b := range metrics.Calibrate(probs, labels, 4) {
		counted += b.Count
	}
	if counted != len(probs) {
		t.Errorf("bins hold %d samples; want %d (1.0 must land in the last bin)", counted, len(probs))
	}
}

func TestBrier(t *testing.T) {
	t.Parallel()
	// Errors of 0.2 and 0.3, so (0.04 + 0.09) / 2.
	close(t, "Brier", metrics.Brier([]float64{0.8, 0.3}, []int{1, 0}), 0.065)
}

func TestBestF1FindsASeparatingThreshold(t *testing.T) {
	t.Parallel()
	// Separable at 0.4, but not at the default 0.5.
	probs := []float64{0.45, 0.42, 0.2, 0.1}
	labels := []int{1, 1, 0, 0}

	if got := metrics.Confuse(probs, labels, 0.5).F1(); got != 0 {
		t.Errorf("F1 at 0.5 = %v; want 0, since nothing clears the default", got)
	}
	threshold, f1 := metrics.BestF1(probs, labels)
	close(t, "best F1", f1, 1.0)
	if threshold > 0.45 || threshold <= 0.2 {
		t.Errorf("threshold = %v; want one that separates the classes", threshold)
	}
}

func TestEmptyInputsAreNaNNotPanics(t *testing.T) {
	t.Parallel()
	if got := metrics.Brier(nil, nil); !math.IsNaN(got) {
		t.Errorf("Brier(nil) = %v; want NaN", got)
	}
	if got := metrics.ECE(nil, nil, 10); !math.IsNaN(got) {
		t.Errorf("ECE(nil) = %v; want NaN", got)
	}
	if got := metrics.Mean(nil); !math.IsNaN(got) {
		t.Errorf("Mean(nil) = %v; want NaN", got)
	}
}
