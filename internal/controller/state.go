package controller

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	currentModeGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "modeswitch_current_mode",
		Help: "1 for the currently applied mode.",
	}, []string{"mode"})
	modeChanges = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "modeswitch_mode_changes_total",
		Help: "Number of mode changes received.",
	})
)

func init() {
	metrics.Registry.MustRegister(currentModeGauge, modeChanges)
}

// ModeState holds the last mode received from the mode server.
// Until the first mode arrives, the controller makes no changes.
type ModeState struct {
	mu   sync.RWMutex
	mode string
	set  bool
}

func (s *ModeState) Current() (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mode, s.set
}

// Set records mode and reports whether it changed.
func (s *ModeState) Set(mode string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.set && s.mode == mode {
		return false
	}
	s.mode, s.set = mode, true
	currentModeGauge.Reset()
	currentModeGauge.WithLabelValues(mode).Set(1)
	modeChanges.Inc()
	return true
}
