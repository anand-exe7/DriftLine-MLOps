package drift

import (
	"math"
	"math/rand/v2"
	"testing"
)

func TestBinIndexMatchesNumpySearchsortedLeft(t *testing.T) {
	edges := []float64{10, 20, 30}
	tests := []struct {
		v    float64
		want int
	}{
		{5, 0},  // below first edge
		{10, 0}, // right-closed: 10 belongs to (-inf, 10]
		{10.01, 1},
		{20, 1},
		{25, 2},
		{30, 2},
		{31, 3}, // above last edge
	}
	for _, tc := range tests {
		if got := binIndex(edges, tc.v); got != tc.want {
			t.Errorf("binIndex(%v) = %d, want %d", tc.v, got, tc.want)
		}
	}
}

func TestPSI(t *testing.T) {
	tests := []struct {
		name     string
		expected []float64
		actual   []float64
		want     float64
		tol      float64
	}{
		{"identical", []float64{0.25, 0.25, 0.25, 0.25}, []float64{0.25, 0.25, 0.25, 0.25}, 0, 1e-12},
		// (0.4-0.5)ln(0.4/0.5) + (0.6-0.5)ln(0.6/0.5)
		{"small shift", []float64{0.5, 0.5}, []float64{0.4, 0.6}, 0.1*math.Log(1.25) + 0.1*math.Log(1.2), 1e-12},
		{"empty live bin is smoothed, not infinite", []float64{0.5, 0.5}, []float64{0, 1}, (epsilon-0.5)*math.Log(epsilon/0.5) + 0.5*math.Log(2), 1e-9},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := PSI(tc.expected, tc.actual)
			if math.Abs(got-tc.want) > tc.tol {
				t.Fatalf("PSI = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestPSIIsSymmetricInSign(t *testing.T) {
	a, b := []float64{0.2, 0.3, 0.5}, []float64{0.4, 0.4, 0.2}
	if PSI(a, b) <= 0 || PSI(b, a) <= 0 {
		t.Fatal("PSI must be positive for different distributions")
	}
}

func TestKSBinned(t *testing.T) {
	expected := []float64{0.25, 0.25, 0.25, 0.25}

	stat, p := KSBinned(expected, expected, 1000)
	if stat != 0 || p != 1 {
		t.Fatalf("identical: stat=%v p=%v", stat, p)
	}

	// CDFs: expected .25 .5 .75 ; actual .1 .2 .4 -> max gap .35
	stat, p = KSBinned(expected, []float64{0.1, 0.1, 0.2, 0.6}, 1000)
	if math.Abs(stat-0.35) > 1e-12 {
		t.Fatalf("stat = %v, want 0.35", stat)
	}
	if p > 1e-6 {
		t.Fatalf("a 0.35 gap over 1000 samples should be highly significant, p=%v", p)
	}

	// Same gap with only 5 samples is not significant.
	if _, p := KSBinned(expected, []float64{0.1, 0.1, 0.2, 0.6}, 5); p < 0.05 {
		t.Fatalf("n=5 should not be significant, p=%v", p)
	}
}

func TestKolmogorovQKnownValues(t *testing.T) {
	// Q(1.36) ≈ 0.05 and Q(1.63) ≈ 0.01 are the classic critical values.
	if q := kolmogorovQ(1.358); math.Abs(q-0.05) > 0.001 {
		t.Errorf("Q(1.358) = %v, want ~0.05", q)
	}
	if q := kolmogorovQ(1.628); math.Abs(q-0.01) > 0.001 {
		t.Errorf("Q(1.628) = %v, want ~0.01", q)
	}
}

func baselineFixture() Baseline {
	var age BaselineFeature
	age.Type = "continuous"
	age.Bins.Edges = []float64{30, 40, 50}
	age.Bins.Proportions = []float64{0.25, 0.25, 0.25, 0.25}
	var flag BaselineFeature
	flag.Type = "binary"
	flag.Bins.Edges = []float64{0.5}
	flag.Bins.Proportions = []float64{0.5, 0.5}
	return Baseline{
		FeatureOrder: []string{"Age", "HasCoSigner"},
		Features:     map[string]BaselineFeature{"Age": age, "HasCoSigner": flag},
	}
}

// sample draws n rows: Age uniform over [lo, hi), HasCoSigner Bernoulli(pFlag).
func sample(n int, lo, hi, pFlag float64, rng *rand.Rand) []map[string]float64 {
	out := make([]map[string]float64, n)
	for i := range out {
		flag := 0.0
		if rng.Float64() < pFlag {
			flag = 1
		}
		out[i] = map[string]float64{"Age": lo + rng.Float64()*(hi-lo), "HasCoSigner": flag}
	}
	return out
}

func TestAnalyze(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	b := baselineFixture()

	tests := []struct {
		name      string
		samples   []map[string]float64
		wantDrift bool
	}{
		// Uniform 20-60 matches the baseline quartiles 30/40/50 exactly.
		{"matches training distribution", sample(4000, 20, 60, 0.5, rng), false},
		{"everyone is now over 50", sample(4000, 50, 70, 0.5, rng), true},
		{"co-signers vanished", sample(4000, 20, 60, 0.02, rng), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := Analyze(b, tc.samples, DefaultThresholds)
			if res.DriftDetected != tc.wantDrift {
				t.Fatalf("drift = %v, want %v (overall %.3f, features %+v)", res.DriftDetected, tc.wantDrift, res.OverallPSI, res.Features)
			}
			if res.SampleSize != len(tc.samples) || len(res.Features) != 2 {
				t.Fatalf("unexpected result shape: %+v", res)
			}
			if res.Features["HasCoSigner"].KSStat != nil {
				t.Error("binary features should not get a KS statistic")
			}
			if res.Features["Age"].KSStat == nil {
				t.Error("continuous features should get a KS statistic")
			}
		})
	}
}

func TestAnalyzeEmpty(t *testing.T) {
	res := Analyze(baselineFixture(), nil, DefaultThresholds)
	if res.DriftDetected || res.SampleSize != 0 {
		t.Fatalf("empty window must not report drift: %+v", res)
	}
}

func TestParseBaselineValidates(t *testing.T) {
	if _, err := ParseBaseline([]byte(`{"features":{}}`)); err == nil {
		t.Error("empty features should fail")
	}
	bad := `{"features":{"x":{"type":"continuous","bins":{"edges":[1,2],"proportions":[0.5,0.5]}}}}`
	if _, err := ParseBaseline([]byte(bad)); err == nil {
		t.Error("mismatched edges/proportions should fail")
	}
}
