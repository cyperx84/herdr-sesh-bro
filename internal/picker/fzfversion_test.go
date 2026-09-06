package picker

import (
	"errors"
	"testing"
)

func fakeRun(out string, err error) func(string, ...string) ([]byte, error) {
	return func(string, ...string) ([]byte, error) { return []byte(out), err }
}

func TestDetectThresholds(t *testing.T) {
	for _, tc := range []struct {
		out                                        string
		version                                    string
		listen, track, footer, every, transformPut bool
	}{
		{"0.74.3 (Homebrew)\n", "0.74.3", true, true, true, true, true},
		{"0.73.0 (devel)", "0.73.0", true, true, true, true, false},
		{"0.72.1", "0.72.1", true, true, true, false, false},
		{"0.71.0", "0.71.0", true, true, false, false, false},
		{"0.66.0", "0.66.0", true, false, false, false, false},
		{"0.65.9", "0.65.9", false, false, false, false, false},
		{"1.0.0", "1.0.0", true, true, true, true, true},
		// No patch component is still a version.
		{"0.74 (x)", "0.74.0", true, true, true, true, true},
	} {
		got := Detect(fakeRun(tc.out, nil))
		if got.Version != tc.version {
			t.Errorf("%q: Version = %q, want %q", tc.out, got.Version, tc.version)
		}
		if got.Listen != tc.listen || got.TrackID != tc.track ||
			got.Footer != tc.footer || got.Every != tc.every {
			t.Errorf("%q: got %+v, want listen=%v track=%v footer=%v every=%v",
				tc.out, got, tc.listen, tc.track, tc.footer, tc.every)
		}
	}
}

// An fzf that cannot be run or whose output is unrecognisable must leave every
// feature off — the picker without any of them is the one that shipped in
// 0.3.0, so the failure mode is "older picker", never "no picker".
func TestDetectDegradesSafely(t *testing.T) {
	for name, run := range map[string]func(string, ...string) ([]byte, error){
		"exec error":     fakeRun("", errors.New("not found")),
		"garbage output": fakeRun("this is not a version", nil),
		"empty output":   fakeRun("", nil),
	} {
		got := Detect(run)
		if got != (Features{}) {
			t.Errorf("%s: got %+v, want zero Features", name, got)
		}
		if got.Live() {
			t.Errorf("%s: Live() = true, want false", name)
		}
	}
}
