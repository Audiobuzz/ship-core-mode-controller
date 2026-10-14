package modespec

import "testing"

var known = map[string]bool{"normal": true, "peak": true, "offpeak": true, "maintenance": true, "standby": true}

func TestParse(t *testing.T) {
	cases := []struct {
		raw     string
		wantErr bool
		want    map[string]int32
	}{
		{raw: "normal=3,peak=6", want: map[string]int32{"normal": 3, "peak": 6, "offpeak": 0, "standby": 0}},
		{raw: " normal = 2 , *=1 ", want: map[string]int32{"normal": 2, "peak": 1, "maintenance": 1}},
		{raw: "*=0,maintenance=1", want: map[string]int32{"maintenance": 1, "normal": 0}},
		{raw: "", wantErr: true},
		{raw: "normal", wantErr: true},
		{raw: "normal=-1", wantErr: true},
		{raw: "normal=x", wantErr: true},
		{raw: "bogus=1", wantErr: true},
		{raw: "normal=1,normal=2", wantErr: true},
		{raw: "*=1,*=2", wantErr: true},
		{raw: ",,", wantErr: true},
	}
	for _, c := range cases {
		sp, err := Parse(c.raw, known)
		if c.wantErr {
			if err == nil {
				t.Errorf("Parse(%q): expected error", c.raw)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): %v", c.raw, err)
			continue
		}
		for mode, want := range c.want {
			if got := sp.ReplicasFor(mode); got != want {
				t.Errorf("Parse(%q).ReplicasFor(%q) = %d, want %d", c.raw, mode, got, want)
			}
		}
	}
}
