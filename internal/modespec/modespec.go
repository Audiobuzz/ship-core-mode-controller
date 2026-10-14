// Package modespec parses the per-workload replicas annotation.
//
// Format: "modeA=3,modeB=1,*=0"
//   - each entry is mode=replicas (non-negative integer)
//   - "*" sets the replica count for any mode not listed
//   - modes not listed and no "*" => 0 (workload does not run in that mode)
package modespec

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

const Wildcard = "*"

type Spec struct {
	perMode map[string]int32
	def     *int32
}

// Parse validates raw against the set of known modes.
func Parse(raw string, known map[string]bool) (Spec, error) {
	sp := Spec{perMode: map[string]int32{}}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Spec{}, errors.New("annotation is empty")
	}
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			return Spec{}, fmt.Errorf("entry %q: expected mode=replicas", part)
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		n, err := strconv.ParseInt(v, 10, 32)
		if err != nil || n < 0 {
			return Spec{}, fmt.Errorf("entry %q: replicas must be a non-negative integer", part)
		}
		r := int32(n)
		if k == Wildcard {
			if sp.def != nil {
				return Spec{}, errors.New("wildcard \"*\" specified more than once")
			}
			sp.def = &r
			continue
		}
		if !known[k] {
			return Spec{}, fmt.Errorf("entry %q: unknown mode %q", part, k)
		}
		if _, dup := sp.perMode[k]; dup {
			return Spec{}, fmt.Errorf("mode %q specified more than once", k)
		}
		sp.perMode[k] = r
	}
	if len(sp.perMode) == 0 && sp.def == nil {
		return Spec{}, errors.New("no entries")
	}
	return sp, nil
}

// ReplicasFor returns the desired replica count in the given mode.
func (s Spec) ReplicasFor(mode string) int32 {
	if r, ok := s.perMode[mode]; ok {
		return r
	}
	if s.def != nil {
		return *s.def
	}
	return 0
}
