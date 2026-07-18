package rules

import (
	"encoding/json"
	"os"
	"sort"
)

// Baseline holds fingerprints of grandfathered violations so that lintel
// can be adopted in a codebase that already has violations: existing ones
// are recorded here, and only new violations fail the check.
type Baseline struct {
	Fingerprints []string `json:"fingerprints"`
}

// LoadBaseline reads a baseline file. A missing file yields an empty baseline.
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &Baseline{}, nil
	}
	if err != nil {
		return nil, err
	}
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// WriteBaseline records the given violations as the new baseline.
func WriteBaseline(path string, violations []Violation) error {
	fps := make([]string, 0, len(violations))
	for _, v := range violations {
		fps = append(fps, v.Fingerprint())
	}
	sort.Strings(fps)
	data, err := json.MarshalIndent(Baseline{Fingerprints: fps}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Filter splits violations into new ones and baselined ones. stale counts
// baseline entries whose violation no longer occurs — debt already paid
// down, so the baseline can be regenerated smaller.
func (b *Baseline) Filter(violations []Violation) (fresh, baselined []Violation, stale int) {
	known := make(map[string]bool, len(b.Fingerprints))
	for _, fp := range b.Fingerprints {
		known[fp] = true
	}
	seen := map[string]bool{}
	for _, v := range violations {
		fp := v.Fingerprint()
		if known[fp] {
			baselined = append(baselined, v)
			seen[fp] = true
		} else {
			fresh = append(fresh, v)
		}
	}
	return fresh, baselined, len(known) - len(seen)
}
