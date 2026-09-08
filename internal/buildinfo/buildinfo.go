// Package buildinfo reports what this binary is: which version, which commit,
// which build produced it, and what it is running on. The values come from
// -ldflags at build time, falling back to the VCS stamps the Go toolchain
// embeds automatically.
package buildinfo

import (
	"runtime"
	"runtime/debug"
	"strings"
	"time"
)

// Set with -ldflags "-X netis/internal/buildinfo.Version=v1.2.3" and friends.
// A release build fills all four in; a local `go build` fills none, and Get
// recovers what it can from the embedded VCS stamps instead.
var (
	Version = "dev"
	Commit  = ""
	Build   = ""
	Date    = ""
)

// startTime is when this process began, for the uptime shown on the about page.
var startTime = time.Now()

// Info is a snapshot of everything the about page reports.
type Info struct {
	Version   string
	Commit    string // full hash, empty when unknown
	Build     string // CI run number, empty for local builds
	Date      string // RFC3339, empty when unknown
	Modified  bool   // built from a dirty working tree
	GoVersion string
	OS        string
	Arch      string
	Deps      []Dep
}

// Dep is one module this binary was built against.
type Dep struct{ Path, Version string }

// interestingDeps are the modules worth showing: the ones that decide how netis
// talks to its database, renders its pages, and pings the network.
var interestingDeps = []string{
	"github.com/a-h/templ",
	"github.com/jackc/pgx/v5",
	"modernc.org/sqlite",
	"github.com/prometheus-community/pro-bing",
	"golang.org/x/crypto",
}

// Get assembles the build information. Values injected at link time win; the
// toolchain's VCS stamps fill the rest, which is what makes a local build
// report a real commit even though nothing was passed to -ldflags. A container
// image is built from a source copy with no .git, so there the CI-injected
// values are the only source.
func Get() Info {
	i := Info{
		Version:   Version,
		Commit:    Commit,
		Build:     Build,
		Date:      Date,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return i
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			if i.Commit == "" {
				i.Commit = s.Value
			}
		case "vcs.time":
			if i.Date == "" {
				i.Date = s.Value
			}
		case "vcs.modified":
			i.Modified = s.Value == "true"
		}
	}
	i.Deps = pickDeps(bi.Deps)
	return i
}

// pickDeps reduces a binary's full module list to interestingDeps, keeping
// that list's order so the about page is stable between builds.
func pickDeps(mods []*debug.Module) []Dep {
	have := make(map[string]string, len(mods))
	for _, m := range mods {
		if m != nil {
			have[m.Path] = m.Version
		}
	}
	var out []Dep
	for _, p := range interestingDeps {
		if v, ok := have[p]; ok {
			out = append(out, Dep{Path: p, Version: v})
		}
	}
	return out
}

// ShortCommit is the commit abbreviated to the usual 7 characters, or "" when
// the commit is unknown.
func (i Info) ShortCommit() string {
	if len(i.Commit) < 7 {
		return i.Commit
	}
	return i.Commit[:7]
}

// CommitURL links to the commit on GitHub, or "" when there is no commit to
// link to.
func (i Info) CommitURL() string {
	if i.Commit == "" {
		return ""
	}
	return "https://github.com/13/netis/commit/" + i.Commit
}

// Label is the short form shown in the footer: the version, marked when the
// working tree was dirty at build time.
func (i Info) Label() string {
	v := i.Version
	if !strings.HasPrefix(v, "v") && v != "dev" {
		v = "v" + v
	}
	if i.Modified {
		v += "+dirty"
	}
	return v
}

// Uptime is how long this process has been running, truncated to the second.
func Uptime() time.Duration { return time.Since(startTime).Truncate(time.Second) }
