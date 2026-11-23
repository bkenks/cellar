package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"
)

// Kind distinguishes formulae from casks.
type Kind int

const (
	KindFormula Kind = iota
	KindCask
)

func (k Kind) String() string {
	if k == KindCask {
		return "cask"
	}
	return "formula"
}

func (k Kind) flag() string {
	if k == KindCask {
		return "--cask"
	}
	return "--formula"
}

// Pkg is a normalised view of a formula or cask.
type Pkg struct {
	Name        string
	FullName    string
	DisplayName string // casks: human name, e.g. "Visual Studio Code"
	Kind        Kind
	Desc        string
	Homepage    string
	Tap         string
	License     string
	Version     string // installed version ("" if not installed)
	Latest      string // latest stable version
	Installed   bool
	InstalledAt time.Time
	OnRequest   bool // explicitly installed (vs. pulled in as a dependency)
	Pinned      bool
	Outdated    bool
	KegOnly     bool
	Deprecated  bool
	Disabled    bool
	AutoUpdates bool
	Bottle      bool
	Deps        []string
	BuildDeps   []string
	UsedBy      []string // computed locally from installed packages
	Caveats     string
	Aliases     []string
}

func (p *Pkg) Key() string { return pkgKey(p.Kind, p.Name) }

func pkgKey(k Kind, name string) string { return k.String() + ":" + name }

// Outdated is one entry from `brew outdated`.
type Outdated struct {
	Name      string
	Kind      Kind
	Installed string
	Current   string
	Pinned    bool
}

func (o Outdated) Key() string { return pkgKey(o.Kind, o.Name) }

// Service is one entry from `brew services list`.
type Service struct {
	Name     string
	Status   string
	User     string
	File     string
	ExitCode *int
}

// SearchResult is a name returned by `brew search`.
type SearchResult struct {
	Name string
	Kind Kind
}

func (s SearchResult) Key() string { return pkgKey(s.Kind, s.Name) }

// --- raw JSON shapes --------------------------------------------------------

type rawInfo struct {
	Formulae []rawFormula `json:"formulae"`
	Casks    []rawCask    `json:"casks"`
}

type rawFormula struct {
	Name     string `json:"name"`
	FullName string `json:"full_name"`
	Tap      string `json:"tap"`
	Desc     string `json:"desc"`
	License  string `json:"license"`
	Homepage string `json:"homepage"`
	Versions struct {
		Stable string `json:"stable"`
		Bottle bool   `json:"bottle"`
	} `json:"versions"`
	Revision  int `json:"revision"`
	Installed []struct {
		Version             string `json:"version"`
		Time                int64  `json:"time"`
		InstalledOnRequest  bool   `json:"installed_on_request"`
		PouredFromBottle    bool   `json:"poured_from_bottle"`
		RuntimeDependencies []struct {
			FullName string `json:"full_name"`
		} `json:"runtime_dependencies"`
	} `json:"installed"`
	Dependencies      []string `json:"dependencies"`
	BuildDependencies []string `json:"build_dependencies"`
	Pinned            bool     `json:"pinned"`
	Outdated          bool     `json:"outdated"`
	KegOnly           bool     `json:"keg_only"`
	Deprecated        bool     `json:"deprecated"`
	Disabled          bool     `json:"disabled"`
	Caveats           string   `json:"caveats"`
	Aliases           []string `json:"aliases"`
}

type rawCask struct {
	Token         string          `json:"token"`
	FullToken     string          `json:"full_token"`
	Tap           string          `json:"tap"`
	Name          []string        `json:"name"`
	Desc          string          `json:"desc"`
	Homepage      string          `json:"homepage"`
	Version       string          `json:"version"`
	Installed     string          `json:"installed"`
	InstalledTime int64           `json:"installed_time"`
	Outdated      bool            `json:"outdated"`
	AutoUpdates   bool            `json:"auto_updates"`
	Deprecated    bool            `json:"deprecated"`
	Disabled      bool            `json:"disabled"`
	Caveats       string          `json:"caveats"`
	DependsOn     json.RawMessage `json:"depends_on"`
}

func (r rawFormula) toPkg() *Pkg {
	p := &Pkg{
		Name:       r.Name,
		FullName:   r.FullName,
		Kind:       KindFormula,
		Desc:       r.Desc,
		Homepage:   r.Homepage,
		Tap:        r.Tap,
		License:    r.License,
		Latest:     r.Versions.Stable,
		Pinned:     r.Pinned,
		Outdated:   r.Outdated,
		KegOnly:    r.KegOnly,
		Deprecated: r.Deprecated,
		Disabled:   r.Disabled,
		Caveats:    strings.TrimSpace(r.Caveats),
		Aliases:    r.Aliases,
		BuildDeps:  r.BuildDependencies,
		Deps:       r.Dependencies,
	}
	if r.Revision > 0 && p.Latest != "" {
		p.Latest = fmt.Sprintf("%s_%d", p.Latest, r.Revision)
	}
	if n := len(r.Installed); n > 0 {
		in := r.Installed[n-1]
		p.Installed = true
		p.Version = in.Version
		p.OnRequest = in.InstalledOnRequest
		p.Bottle = in.PouredFromBottle
		if in.Time > 0 {
			p.InstalledAt = time.Unix(in.Time, 0)
		}
		if len(in.RuntimeDependencies) > 0 {
			p.Deps = p.Deps[:0:0]
			for _, d := range in.RuntimeDependencies {
				p.Deps = append(p.Deps, d.FullName)
			}
		}
	}
	return p
}

func (r rawCask) toPkg() *Pkg {
	p := &Pkg{
		Name:        r.Token,
		FullName:    r.FullToken,
		Kind:        KindCask,
		Desc:        r.Desc,
		Homepage:    r.Homepage,
		Tap:         r.Tap,
		Latest:      r.Version,
		Version:     r.Installed,
		Installed:   r.Installed != "",
		OnRequest:   true,
		Outdated:    r.Outdated,
		AutoUpdates: r.AutoUpdates,
		Deprecated:  r.Deprecated,
		Disabled:    r.Disabled,
		Caveats:     strings.TrimSpace(r.Caveats),
	}
	if len(r.Name) > 0 {
		p.DisplayName = r.Name[0]
	}
	if r.InstalledTime > 0 {
		p.InstalledAt = time.Unix(r.InstalledTime, 0)
	}
	var dep struct {
		Formula []string `json:"formula"`
		Cask    []string `json:"cask"`
	}
	if len(r.DependsOn) > 0 && json.Unmarshal(r.DependsOn, &dep) == nil {
		p.Deps = append(dep.Formula, dep.Cask...)
	}
	return p
}

// --- running brew ----------------------------------------------------------

func brewEnv() []string {
	return append(os.Environ(),
		"HOMEBREW_NO_ENV_HINTS=1",
		"HOMEBREW_NO_COLOR=1",
	)
}

// brewOutput runs a read-only brew command and returns stdout.
func brewOutput(args ...string) ([]byte, error) {
	cmd := exec.Command("brew", args...)
	cmd.Env = append(brewEnv(), "HOMEBREW_NO_AUTO_UPDATE=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := lastNonEmptyLine(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return out, fmt.Errorf("brew %s: %s", strings.Join(args, " "), msg)
	}
	return out, nil
}

func lastNonEmptyLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l
		}
	}
	return ""
}

func parseInfo(data []byte) ([]*Pkg, error) {
	var raw rawInfo
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	pkgs := make([]*Pkg, 0, len(raw.Formulae)+len(raw.Casks))
	for _, f := range raw.Formulae {
		pkgs = append(pkgs, f.toPkg())
	}
	for _, c := range raw.Casks {
		pkgs = append(pkgs, c.toPkg())
	}
	return pkgs, nil
}

func fetchInstalled() ([]*Pkg, error) {
	out, err := brewOutput("info", "--json=v2", "--installed")
	if err != nil {
		return nil, err
	}
	pkgs, err := parseInfo(out)
	if err != nil {
		return nil, err
	}

	// Compute reverse dependencies ("used by") locally.
	byName := map[string]*Pkg{}
	for _, p := range pkgs {
		if p.Kind == KindFormula {
			byName[p.Name] = p
			byName[p.FullName] = p
		}
	}
	for _, p := range pkgs {
		for _, d := range p.Deps {
			if dp, ok := byName[d]; ok && dp != p {
				dp.UsedBy = append(dp.UsedBy, p.Name)
			}
		}
	}
	for _, p := range pkgs {
		sort.Strings(p.UsedBy)
	}

	sort.SliceStable(pkgs, func(i, j int) bool {
		return strings.ToLower(pkgs[i].Name) < strings.ToLower(pkgs[j].Name)
	})
	return pkgs, nil
}

func fetchInfo(name string, kind Kind) (*Pkg, error) {
	out, err := brewOutput("info", "--json=v2", kind.flag(), name)
	if err != nil {
		return nil, err
	}
	pkgs, err := parseInfo(out)
	if err != nil {
		return nil, err
	}
	if len(pkgs) == 0 {
		return nil, fmt.Errorf("no info for %s", name)
	}
	return pkgs[0], nil
}

func fetchOutdated() ([]Outdated, error) {
	out, err := brewOutput("outdated", "--json=v2")
	if err != nil {
		return nil, err
	}
	var raw struct {
		Formulae []struct {
			Name              string   `json:"name"`
			InstalledVersions []string `json:"installed_versions"`
			CurrentVersion    string   `json:"current_version"`
			Pinned            bool     `json:"pinned"`
		} `json:"formulae"`
		Casks []struct {
			Name              string   `json:"name"`
			InstalledVersions []string `json:"installed_versions"`
			CurrentVersion    string   `json:"current_version"`
		} `json:"casks"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	last := func(s []string) string {
		if len(s) == 0 {
			return ""
		}
		return s[len(s)-1]
	}
	var items []Outdated
	for _, f := range raw.Formulae {
		items = append(items, Outdated{Name: f.Name, Kind: KindFormula, Installed: last(f.InstalledVersions), Current: f.CurrentVersion, Pinned: f.Pinned})
	}
	for _, c := range raw.Casks {
		items = append(items, Outdated{Name: c.Name, Kind: KindCask, Installed: last(c.InstalledVersions), Current: c.CurrentVersion})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].Name < items[j].Name })
	return items, nil
}

func fetchServices() ([]Service, error) {
	out, err := brewOutput("services", "list", "--json")
	if err != nil {
		return nil, err
	}
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, nil
	}
	var raw []struct {
		Name     string  `json:"name"`
		Status   string  `json:"status"`
		User     *string `json:"user"`
		File     string  `json:"file"`
		ExitCode *int    `json:"exit_code"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, err
	}
	svcs := make([]Service, 0, len(raw))
	for _, r := range raw {
		s := Service{Name: r.Name, Status: r.Status, File: r.File, ExitCode: r.ExitCode}
		if r.User != nil {
			s.User = *r.User
		}
		svcs = append(svcs, s)
	}
	return svcs, nil
}

func searchBrew(query string) ([]SearchResult, error) {
	var (
		wg              sync.WaitGroup
		formulae, casks []byte
		ferr, cerr      error
	)
	wg.Add(2)
	go func() { defer wg.Done(); formulae, ferr = brewOutput("search", "--formula", query) }()
	go func() { defer wg.Done(); casks, cerr = brewOutput("search", "--cask", query) }()
	wg.Wait()

	parse := func(b []byte, k Kind) []SearchResult {
		var res []SearchResult
		for _, line := range strings.Split(string(b), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "==>") || strings.Contains(line, " ") {
				continue
			}
			res = append(res, SearchResult{Name: line, Kind: k})
		}
		return res
	}
	results := append(parse(formulae, KindFormula), parse(casks, KindCask)...)
	if len(results) == 0 && ferr != nil && cerr != nil {
		// `brew search` exits non-zero when nothing matches.
		if strings.Contains(ferr.Error(), "No formulae or casks found") ||
			strings.Contains(ferr.Error(), "No formulae found") {
			return nil, nil
		}
		return nil, ferr
	}

	// Exact and prefix matches first, then alphabetical.
	q := strings.ToLower(query)
	rank := func(r SearchResult) int {
		n := strings.ToLower(r.Name)
		switch {
		case n == q:
			return 0
		case strings.HasPrefix(n, q):
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(results, func(i, j int) bool {
		ri, rj := rank(results[i]), rank(results[j])
		if ri != rj {
			return ri < rj
		}
		return results[i].Name < results[j].Name
	})
	return results, nil
}

func fetchBrewVersion() string {
	out, err := brewOutput("--version")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
}
