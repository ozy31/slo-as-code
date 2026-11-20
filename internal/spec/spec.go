// Package spec defines the SLO file format and validates it.
package spec

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// WindowPlaceholder is replaced with each evaluation window ("5m", "1h", ...)
// when the SLI queries are expanded into recording rules.
const WindowPlaceholder = "$window"

// File is one service's SLO definition.
type File struct {
	Service string            `yaml:"service"`
	Labels  map[string]string `yaml:"labels,omitempty"`
	SLOs    []SLO             `yaml:"slos"`
}

// SLO is a single objective measured as a ratio of bad events to all events.
type SLO struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description,omitempty"`
	Objective   float64  `yaml:"objective"` // percent, e.g. 99.9
	Window      Duration `yaml:"window"`    // compliance period, e.g. 30d
	SLI         SLI      `yaml:"sli"`
	Alerting    Alerting `yaml:"alerting"`
}

// SLI holds PromQL for bad and total events. Both must contain $window.
type SLI struct {
	Errors string `yaml:"errors"`
	Total  string `yaml:"total"`
}

// Alerting configures the generated burn-rate alerts.
type Alerting struct {
	Disabled bool              `yaml:"disabled,omitempty"`
	Runbook  string            `yaml:"runbook,omitempty"`
	Labels   map[string]string `yaml:"labels,omitempty"`
	Page     Severity          `yaml:"page,omitempty"`
	Ticket   Severity          `yaml:"ticket,omitempty"`
}

// Severity is the label set attached to one class of alert.
type Severity struct {
	Labels map[string]string `yaml:"labels,omitempty"`
}

// Duration accepts Prometheus-style durations including days ("30d").
type Duration struct{ time.Duration }

var durationRe = regexp.MustCompile(`^([0-9]+)(m|h|d|w)$`)

// UnmarshalYAML parses values like "28d", "4w" or "720h".
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	parsed, err := ParseDuration(n.Value)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

// ParseDuration parses a Prometheus-style duration with a single unit.
func ParseDuration(s string) (time.Duration, error) {
	m := durationRe.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("invalid duration %q (use e.g. 30d, 4w, 720h)", s)
	}
	var n int64
	fmt.Sscanf(m[1], "%d", &n)
	unit := map[string]time.Duration{"m": time.Minute, "h": time.Hour, "d": 24 * time.Hour, "w": 7 * 24 * time.Hour}[m[2]]
	return time.Duration(n) * unit, nil
}

// Load reads and validates a file. Unknown keys are rejected so a typo in
// a field name fails loudly instead of silently dropping configuration.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	f.applyDefaults()
	if err := f.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &f, nil
}

func (f *File) applyDefaults() {
	for i := range f.SLOs {
		if f.SLOs[i].Window.Duration == 0 {
			f.SLOs[i].Window.Duration = 30 * 24 * time.Hour
		}
	}
}

var nameRe = regexp.MustCompile(`^[a-z][a-z0-9-]*$`)

// Validate reports every problem in the file, not just the first.
func (f *File) Validate() error {
	var errs []error
	if !nameRe.MatchString(f.Service) {
		errs = append(errs, fmt.Errorf("service %q must match %s", f.Service, nameRe))
	}
	if len(f.SLOs) == 0 {
		errs = append(errs, errors.New("at least one SLO is required"))
	}
	seen := map[string]bool{}
	for i, s := range f.SLOs {
		at := fmt.Sprintf("slos[%d] (%s)", i, s.Name)
		if !nameRe.MatchString(s.Name) {
			errs = append(errs, fmt.Errorf("%s: name must match %s", at, nameRe))
		}
		if seen[s.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate name", at))
		}
		seen[s.Name] = true
		if s.Objective <= 0 || s.Objective >= 100 {
			errs = append(errs, fmt.Errorf("%s: objective %.4g must be between 0 and 100 (exclusive)", at, s.Objective))
		}
		if s.Window.Duration < 24*time.Hour {
			errs = append(errs, fmt.Errorf("%s: window must be at least 1d", at))
		}
		for field, q := range map[string]string{"sli.errors": s.SLI.Errors, "sli.total": s.SLI.Total} {
			if strings.TrimSpace(q) == "" {
				errs = append(errs, fmt.Errorf("%s: %s is required", at, field))
			} else if !strings.Contains(q, WindowPlaceholder) {
				errs = append(errs, fmt.Errorf("%s: %s must use %s as the range", at, field, WindowPlaceholder))
			}
		}
	}
	return errors.Join(errs...)
}

// ErrorBudget is the allowed bad-event ratio, e.g. 0.001 for 99.9%.
func (s SLO) ErrorBudget() float64 { return 1 - s.Objective/100 }
