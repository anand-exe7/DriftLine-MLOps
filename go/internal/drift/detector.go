// Package drift compares live model inputs against the training baseline.
//
// The baseline (baseline_stats.json, produced by ml/.../baseline.py) stores,
// per feature, decile bin edges and the share of training rows in each bin.
// Live values are bucketed with the *same* edges and the *same* rule
// (value v falls in bin i when edges[i-1] < v <= edges[i]), then compared:
//
//   - PSI  (Population Stability Index): sum over bins of (a - e) * ln(a / e)
//   - KS   (Kolmogorov-Smirnov, binned): max gap between the two CDFs at the
//     bin edges, with an asymptotic p-value
package drift

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"

	"driftline/go/internal/models"
)

// Baseline mirrors baseline_stats.json (only the fields drift needs).
type Baseline struct {
	FeatureOrder []string                   `json:"feature_order"`
	Features     map[string]BaselineFeature `json:"features"`
}

type BaselineFeature struct {
	Type string `json:"type"` // "binary" | "continuous"
	Bins struct {
		Edges       []float64 `json:"edges"`
		Proportions []float64 `json:"proportions"`
	} `json:"bins"`
}

func ParseBaseline(raw []byte) (Baseline, error) {
	var b Baseline
	if err := json.Unmarshal(raw, &b); err != nil {
		return Baseline{}, fmt.Errorf("drift: parse baseline: %w", err)
	}
	if len(b.Features) == 0 {
		return Baseline{}, fmt.Errorf("drift: baseline has no features")
	}
	for name, f := range b.Features {
		if len(f.Bins.Proportions) != len(f.Bins.Edges)+1 {
			return Baseline{}, fmt.Errorf("drift: feature %s: %d proportions for %d edges", name, len(f.Bins.Proportions), len(f.Bins.Edges))
		}
	}
	return b, nil
}

// Thresholds follow the usual PSI rule of thumb: < 0.1 stable, 0.1-0.25
// moderate shift, > 0.25 significant.
type Thresholds struct {
	FeaturePSI float64 // any single feature at or above this => drift
	OverallPSI float64 // mean PSI across features at or above this => drift
}

var DefaultThresholds = Thresholds{FeaturePSI: 0.25, OverallPSI: 0.1}

// epsilon replaces empty bins: ln(a/0) is infinite, and an empty bin in a
// small live window is usually sampling noise, not infinite drift.
const epsilon = 1e-4

// binIndex mirrors numpy.searchsorted(edges, v, side="left").
func binIndex(edges []float64, v float64) int {
	return sort.SearchFloat64s(edges, v)
}

// Proportions buckets values with the baseline edges.
func Proportions(edges []float64, values []float64) []float64 {
	counts := make([]float64, len(edges)+1)
	for _, v := range values {
		counts[binIndex(edges, v)]++
	}
	if len(values) > 0 {
		for i := range counts {
			counts[i] /= float64(len(values))
		}
	}
	return counts
}

// PSI between expected (baseline) and actual (live) bin shares.
func PSI(expected, actual []float64) float64 {
	var psi float64
	for i := range expected {
		e := math.Max(expected[i], epsilon)
		a := math.Max(actual[i], epsilon)
		psi += (a - e) * math.Log(a/e)
	}
	return psi
}

// KSBinned returns the largest gap between the cumulative bin shares and an
// asymptotic p-value for n live samples against the fixed reference
// distribution (Stephens' small-sample correction).
func KSBinned(expected, actual []float64, n int) (stat, p float64) {
	var ce, ca float64
	for i := 0; i < len(expected)-1; i++ { // the last CDF point is always 1 vs 1
		ce += expected[i]
		ca += actual[i]
		stat = math.Max(stat, math.Abs(ce-ca))
	}
	if n <= 0 {
		return stat, 1
	}
	sn := math.Sqrt(float64(n))
	return stat, kolmogorovQ((sn + 0.12 + 0.11/sn) * stat)
}

// kolmogorovQ is the survival function of the Kolmogorov distribution:
// Q(λ) = 2 Σ_{k≥1} (-1)^{k-1} exp(-2 k² λ²).
func kolmogorovQ(lambda float64) float64 {
	if lambda < 1e-3 {
		return 1
	}
	var sum float64
	sign := 1.0
	for k := 1; k <= 100; k++ {
		term := sign * math.Exp(-2*float64(k*k)*lambda*lambda)
		sum += term
		if math.Abs(term) < 1e-10 {
			break
		}
		sign = -sign
	}
	return math.Min(1, math.Max(0, 2*sum))
}

// Result is the outcome of comparing one window of live inputs to a baseline.
type Result struct {
	SampleSize    int
	OverallPSI    float64
	DriftDetected bool
	Features      map[string]models.FeatureDrift
}

// Analyze scores every baseline feature present in the samples.
func Analyze(b Baseline, samples []map[string]float64, t Thresholds) Result {
	res := Result{SampleSize: len(samples), Features: map[string]models.FeatureDrift{}}
	if len(samples) == 0 {
		return res
	}

	names := b.FeatureOrder
	if len(names) == 0 {
		for n := range b.Features {
			names = append(names, n)
		}
		sort.Strings(names)
	}

	var total float64
	var scored int
	values := make([]float64, 0, len(samples))
	for _, name := range names {
		f, ok := b.Features[name]
		if !ok {
			continue
		}
		values = values[:0]
		for _, s := range samples {
			if v, ok := s[name]; ok && !math.IsNaN(v) {
				values = append(values, v)
			}
		}
		if len(values) == 0 {
			continue
		}

		actual := Proportions(f.Bins.Edges, values)
		fd := models.FeatureDrift{PSI: PSI(f.Bins.Proportions, actual)}
		if f.Type != "binary" { // a KS test on two buckets says nothing PSI doesn't
			stat, p := KSBinned(f.Bins.Proportions, actual, len(values))
			fd.KSStat, fd.KSP = &stat, &p
		}
		res.Features[name] = fd
		total += fd.PSI
		scored++
		if fd.PSI >= t.FeaturePSI {
			res.DriftDetected = true
		}
	}
	if scored > 0 {
		res.OverallPSI = total / float64(scored)
	}
	if res.OverallPSI >= t.OverallPSI {
		res.DriftDetected = true
	}
	return res
}
