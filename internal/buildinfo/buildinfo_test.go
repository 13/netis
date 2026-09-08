package buildinfo

import (
	"runtime/debug"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestLabel(t *testing.T) {
	cases := []struct {
		info Info
		want string
	}{
		// A bare semver gains the v; one that already has it is left alone.
		{Info{Version: "1.2.3"}, "v1.2.3"},
		{Info{Version: "v1.2.3"}, "v1.2.3"},
		{Info{Version: "dev"}, "dev"},
		{Info{Version: "dev", Modified: true}, "dev+dirty"},
		{Info{Version: "v1.2.3", Modified: true}, "v1.2.3+dirty"},
	}
	for _, c := range cases {
		if got := c.info.Label(); got != c.want {
			t.Errorf("Info%+v.Label() = %q, want %q", c.info, got, c.want)
		}
	}
}

func TestShortCommitAndURL(t *testing.T) {
	full := "0123456789abcdef0123456789abcdef01234567"
	i := Info{Commit: full}
	if got := i.ShortCommit(); got != "0123456" {
		t.Errorf("ShortCommit() = %q", got)
	}
	if got := i.CommitURL(); !strings.HasSuffix(got, full) {
		t.Errorf("CommitURL() = %q, want it to end in the full hash", got)
	}
	// A short or absent commit must not panic or produce a broken link.
	if got := (Info{Commit: "abc"}).ShortCommit(); got != "abc" {
		t.Errorf("short commit = %q", got)
	}
	if got := (Info{}).CommitURL(); got != "" {
		t.Errorf("CommitURL() with no commit = %q, want empty", got)
	}
}

// Get must always report the runtime facts, whether or not anything was
// injected at link time — the test binary has no -X flags.
func TestGetFillsRuntimeFacts(t *testing.T) {
	i := Get()
	if i.Version == "" {
		t.Error("Version is empty")
	}
	if !strings.HasPrefix(i.GoVersion, "go") {
		t.Errorf("GoVersion = %q", i.GoVersion)
	}
	if i.OS == "" || i.Arch == "" {
		t.Errorf("platform = %q/%q", i.OS, i.Arch)
	}
	// Deps reflects what this particular binary links in, which for this test
	// binary is very little; whatever is listed must come from the curated set.
	for _, d := range i.Deps {
		if !slices.Contains(interestingDeps, d.Path) {
			t.Errorf("Deps includes %q, which is not in interestingDeps", d.Path)
		}
	}
}

func TestPickDeps(t *testing.T) {
	mods := []*debug.Module{
		{Path: "modernc.org/sqlite", Version: "v1.54.0"},
		{Path: "github.com/some/unrelated", Version: "v0.1.0"},
		nil, // ReadBuildInfo can hand back nil entries for replaced modules
		{Path: "github.com/a-h/templ", Version: "v0.3.1020"},
	}
	got := pickDeps(mods)
	// interestingDeps lists templ before sqlite, and that order is what the
	// page shows, regardless of the order the modules arrive in.
	want := []Dep{
		{Path: "github.com/a-h/templ", Version: "v0.3.1020"},
		{Path: "modernc.org/sqlite", Version: "v1.54.0"},
	}
	if !slices.Equal(got, want) {
		t.Errorf("pickDeps() = %+v, want %+v", got, want)
	}
	if got := pickDeps(nil); got != nil {
		t.Errorf("pickDeps(nil) = %+v, want nil", got)
	}
}

func TestUptime(t *testing.T) {
	if u := Uptime(); u < 0 || u > time.Hour {
		t.Errorf("Uptime() = %v, want a small non-negative duration", u)
	}
}
