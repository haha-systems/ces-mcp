package cognition

import (
	"fmt"
	"math"
)

// ProgressState and ProgressReport are model-reported execution metadata.
// They do not become verified evidence when an artifact is accepted.
type ProgressState string

const (
	ProgressUnknown    ProgressState = "unknown"
	ProgressLow        ProgressState = "low"
	ProgressMeaningful ProgressState = "meaningful"
)

type ProgressReport struct {
	State ProgressState `json:"state"`
}

func validateQACRequest(r QACRequest) error {
	if r.Direction != "escalate" && r.Direction != "release" {
		return fmt.Errorf("qac request: invalid direction %q", r.Direction)
	}

	for _, value := range []float64{r.Uncertainty, r.Novelty, r.ExpectedGain} {
		if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > 1 {
			return fmt.Errorf("qac request: signal must be in [0,1]")
		}
	}

	if r.FailedAttempts < 0 {
		return fmt.Errorf("qac request: failed_attempts must be non-negative")
	}

	return nil
}
