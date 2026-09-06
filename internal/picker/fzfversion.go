package picker

import (
	"regexp"
	"strconv"
	"strings"
)

// Features is which fzf capabilities the installed binary actually has.
//
// sesh-bro's live picker is built out of four fzf features that landed across
// four releases, and users install fzf from wherever their distro puts it. A
// picker that hard-required the newest one would simply fail to open for
// anyone a version behind, so every feature is detected and every absence
// degrades to the behaviour that shipped before it: no Listen means no pushed
// updates (the picker is a snapshot again, exactly as in 0.3.0), no TrackID
// means a reload resets the cursor, no Footer means the key hints stay in the
// header where they have always been.
//
// A version that cannot be parsed at all sets nothing, which is the safe
// direction: the 0.3.0 picker with none of this is a working picker.
type Features struct {
	// Version is the parsed "MAJOR.MINOR.PATCH", for diagnostics.
	Version string

	// Listen is --listen on a unix socket, fzf 0.66. It is what lets an
	// external process push `reload(...)` into a RUNNING picker, which is the
	// whole basis of updating the list without polling.
	Listen bool
	// TrackID is --track --id-nth, fzf 0.71. Without it, tracking follows the
	// cursor's index, so a list that re-sorts under you moves the selection to
	// a different agent. With it the cursor follows the row's identity field,
	// which is what makes live re-sorting tolerable at all.
	TrackID bool
	// Footer is --footer, fzf 0.72, so the key hints can leave the header and
	// let the header carry live status counts instead.
	Footer bool
	// Every is the every(N) timer bind, fzf 0.73. Nothing uses it: updates
	// are pushed, so there is no timer. It is detected because a fallback for
	// an fzf that has every() but not --listen is a plausible future, and
	// probing one more threshold costs nothing.
	Every bool
}

// versionRe matches the leading version in `fzf --version` output, which is
// shaped like "0.74.3 (Homebrew)" or "0.65.0 (devel)".
var versionRe = regexp.MustCompile(`^(\d+)\.(\d+)(?:\.(\d+))?`)

// Detect parses `fzf --version` and reports what that build supports.
//
// run is injected so tests do not need an fzf on PATH and can pin every
// threshold. Any error, and any output that does not begin with a version,
// yields the zero Features — see the type's doc comment for why that is the
// safe direction.
func Detect(run func(name string, args ...string) ([]byte, error)) Features {
	out, err := run("fzf", "--version")
	if err != nil {
		return Features{}
	}
	m := versionRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return Features{}
	}
	major, _ := strconv.Atoi(m[1])
	minor, _ := strconv.Atoi(m[2])
	patch := 0
	if m[3] != "" {
		patch, _ = strconv.Atoi(m[3])
	}

	atLeast := func(wantMajor, wantMinor int) bool {
		if major != wantMajor {
			return major > wantMajor
		}
		return minor >= wantMinor
	}

	v := strconv.Itoa(major) + "." + strconv.Itoa(minor) + "." + strconv.Itoa(patch)
	return Features{
		Version: v,
		Listen:  atLeast(0, 66),
		TrackID: atLeast(0, 71),
		Footer:  atLeast(0, 72),
		Every:   atLeast(0, 73),
	}
}

// Live reports whether this fzf can host the pushed-update picker at all.
// Below it, the picker still opens and still works; it just shows the list as
// it was when you pressed the key.
func (f Features) Live() bool { return f.Listen }
