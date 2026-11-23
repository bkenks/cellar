package main

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
)

type tabID int

const (
	tabInstalled tabID = iota
	tabOutdated
	tabSearch
	tabServices
	numTabs
)

var tabNames = [numTabs]string{"Installed", "Outdated", "Search", "Services"}

type scope int

const (
	scopeAll scope = iota
	scopeFormulae
	scopeCasks
	scopeLeaves
	numScopes
)

var scopeNames = [numScopes]string{"All", "Formulae", "Casks", "Leaves"}

type cursor struct{ idx, off int }

type confirmDlg struct {
	title   string
	body    []string
	okLabel string
	danger  bool
	focusOK bool
	onOK    func(m *model) tea.Cmd
}

type model struct {
	w, h     int
	th       theme
	spin     spinner.Model
	spinning bool
	tab      tabID
	version  string

	installed        []*Pkg
	byKey            map[string]*Pkg
	loadingInstalled bool
	errInstalled     error

	outdated        []Outdated
	outdatedByKey   map[string]Outdated
	loadingOutdated bool
	errOutdated     error
	selected        map[string]bool

	services        []Service
	loadingServices bool
	errServices     error

	scope     scope
	filter    textinput.Model
	filtering bool
	filters   [numTabs]string

	search        textinput.Model
	searchFocused bool
	searching     bool
	lastQuery     string
	results       []SearchResult
	errSearch     error
	info          map[string]*Pkg
	infoErr       map[string]error
	infoLoading   map[string]bool

	cur          [numTabs]cursor
	detailKey    string
	detailScroll int

	task     *task
	confirm  *confirmDlg
	showHelp bool
	// showDetail swaps the list for the detail pane on narrow terminals.
	showDetail bool

	toast    string
	toastErr bool
	toastID  int
}

// --- messages ---------------------------------------------------------------

type installedMsg struct {
	pkgs []*Pkg
	err  error
}
type outdatedMsg struct {
	items []Outdated
	err   error
}
type servicesMsg struct {
	items []Service
	err   error
}
type versionMsg string
type searchMsg struct {
	query   string
	results []SearchResult
	err     error
}
type infoReqMsg struct{ key string }
type infoMsg struct {
	key string
	pkg *Pkg
	err error
}
type quickDoneMsg struct {
	ok  string
	err error
}
type toastExpireMsg struct{ id int }

// --- commands ---------------------------------------------------------------

func loadInstalled() tea.Msg {
	p, err := fetchInstalled()
	return installedMsg{p, err}
}

func loadOutdated() tea.Msg {
	o, err := fetchOutdated()
	return outdatedMsg{o, err}
}

func loadServices() tea.Msg {
	s, err := fetchServices()
	return servicesMsg{s, err}
}

func loadVersion() tea.Msg { return versionMsg(fetchBrewVersion()) }

func doSearch(q string) tea.Cmd {
	return func() tea.Msg {
		r, err := searchBrew(q)
		return searchMsg{q, r, err}
	}
}

func loadInfo(key string, name string, kind Kind) tea.Cmd {
	return func() tea.Msg {
		p, err := fetchInfo(name, kind)
		return infoMsg{key, p, err}
	}
}

func quick(ok string, args ...string) tea.Cmd {
	return func() tea.Msg {
		_, err := brewOutput(args...)
		return quickDoneMsg{ok, err}
	}
}

func openURL(url string) tea.Cmd {
	return func() tea.Msg {
		_ = exec.Command("open", url).Start()
		return nil
	}
}

// --- model ------------------------------------------------------------------

func newModel() model {
	m := model{
		th:               newTheme(true),
		byKey:            map[string]*Pkg{},
		selected:         map[string]bool{},
		info:             map[string]*Pkg{},
		infoErr:          map[string]error{},
		infoLoading:      map[string]bool{},
		loadingInstalled: true,
		loadingOutdated:  true,
		loadingServices:  true,
		spinning:         true, // Init starts the tick loop
	}
	m.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot))

	m.filter = textinput.New()
	m.filter.Prompt = "/ "
	m.filter.Placeholder = "filter…"

	m.search = textinput.New()
	m.search.Prompt = "❯ "
	m.search.Placeholder = "search formulae & casks, then press enter"

	m.applyTheme()
	return m
}

func (m *model) applyTheme() {
	t := m.th
	m.spin.Style = t.s(t.accent)
	st := textinput.DefaultStyles(t.dark)
	st.Focused.Prompt = t.s(t.accent).Bold(true)
	st.Blurred.Prompt = t.s(t.muted)
	st.Focused.Text = t.s(t.fg)
	st.Blurred.Text = t.s(t.muted)
	st.Focused.Placeholder = t.s(t.faint)
	st.Blurred.Placeholder = t.s(t.faint)
	st.Cursor.Color = t.accent
	m.filter.SetStyles(st)
	m.search.SetStyles(st)
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		tea.RequestBackgroundColor,
		loadInstalled, loadOutdated, loadServices, loadVersion,
		m.spin.Tick,
	)
}

func (m model) busy() bool {
	if m.loadingInstalled || m.loadingOutdated || m.loadingServices || m.searching {
		return true
	}
	if m.task != nil && m.task.running {
		return true
	}
	for _, v := range m.infoLoading {
		if v {
			return true
		}
	}
	return false
}

func (m *model) ensureSpin() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinning = true
	return m.spin.Tick
}

func (m *model) reload() tea.Cmd {
	m.loadingInstalled, m.loadingOutdated, m.loadingServices = true, true, true
	return tea.Batch(loadInstalled, loadOutdated, loadServices, m.ensureSpin())
}

func (m *model) setToast(s string, isErr bool) tea.Cmd {
	m.toastID++
	m.toast, m.toastErr = s, isErr
	id := m.toastID
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg { return toastExpireMsg{id} })
}

// --- derived data -----------------------------------------------------------

func matches(q string, fields ...string) bool {
	if q == "" {
		return true
	}
	q = strings.ToLower(q)
	for _, f := range fields {
		if strings.Contains(strings.ToLower(f), q) {
			return true
		}
	}
	return false
}

func (m model) visInstalled() []*Pkg {
	q := m.filters[tabInstalled]
	out := make([]*Pkg, 0, len(m.installed))
	var descOnly []*Pkg // description-only matches sort after name matches
	for _, p := range m.installed {
		switch m.scope {
		case scopeFormulae:
			if p.Kind != KindFormula {
				continue
			}
		case scopeCasks:
			if p.Kind != KindCask {
				continue
			}
		case scopeLeaves:
			if !p.OnRequest {
				continue
			}
		}
		switch {
		case matches(q, p.Name, p.DisplayName):
			out = append(out, p)
		case matches(q, p.Desc):
			descOnly = append(descOnly, p)
		}
	}
	return append(out, descOnly...)
}

func (m model) visOutdated() []Outdated {
	q := m.filters[tabOutdated]
	var out []Outdated
	for _, o := range m.outdated {
		if matches(q, o.Name) {
			out = append(out, o)
		}
	}
	return out
}

func (m model) visServices() []Service {
	q := m.filters[tabServices]
	var out []Service
	for _, s := range m.services {
		if matches(q, s.Name, s.Status) {
			out = append(out, s)
		}
	}
	return out
}

func (m model) count(t tabID) int {
	switch t {
	case tabInstalled:
		return len(m.visInstalled())
	case tabOutdated:
		return len(m.visOutdated())
	case tabSearch:
		return len(m.results)
	case tabServices:
		return len(m.visServices())
	}
	return 0
}

// currentKey identifies the item under the cursor (used for detail state).
func (m model) currentKey() string {
	i := m.cur[m.tab].idx
	switch m.tab {
	case tabInstalled:
		if v := m.visInstalled(); i < len(v) {
			return v[i].Key()
		}
	case tabOutdated:
		if v := m.visOutdated(); i < len(v) {
			return v[i].Key()
		}
	case tabSearch:
		if i < len(m.results) {
			return m.results[i].Key()
		}
	case tabServices:
		if v := m.visServices(); i < len(v) {
			return pkgKey(KindFormula, v[i].Name)
		}
	}
	return ""
}

// currentPkg returns the best package info we have for the cursor item.
func (m model) currentPkg() *Pkg {
	key := m.currentKey()
	if key == "" {
		return nil
	}
	if p, ok := m.byKey[key]; ok {
		return p
	}
	return m.info[key]
}

func (m model) currentService() (Service, bool) {
	if m.tab != tabServices {
		return Service{}, false
	}
	v := m.visServices()
	if i := m.cur[tabServices].idx; i < len(v) {
		return v[i], true
	}
	return Service{}, false
}

// --- layout -----------------------------------------------------------------

const headerH, footerH = 1, 1

func (m model) layout() (listW, detailW, bodyH int) {
	bodyH = max(m.h-headerH-footerH, 6)
	if m.w >= 88 {
		listW = min(max(m.w*44/100, 38), 72)
		detailW = m.w - listW
	} else {
		listW = m.w
	}
	return
}

// listRows is how many list rows fit inside the list panel.
func (m model) listRows() int {
	_, _, bodyH := m.layout()
	return max(bodyH-4, 1) // borders + header + rule
}

// listTop is the screen row of the first list item.
func (m model) listTop() int { return headerH + 1 + 2 }

func (m *model) clampCursor() {
	n := m.count(m.tab)
	rows := m.listRows()
	c := &m.cur[m.tab]
	if c.idx >= n {
		c.idx = n - 1
	}
	if c.idx < 0 {
		c.idx = 0
	}
	if c.idx < c.off {
		c.off = c.idx
	}
	if c.idx >= c.off+rows {
		c.off = c.idx - rows + 1
	}
	if c.off > max(0, n-rows) {
		c.off = max(0, n-rows)
	}
	if c.off < 0 {
		c.off = 0
	}
}

func (m *model) move(d int) tea.Cmd {
	m.cur[m.tab].idx += d
	m.clampCursor()
	return m.afterMove()
}

// afterMove resets detail scrolling on selection change and lazily fetches
// info for search results.
func (m *model) afterMove() tea.Cmd {
	key := m.currentKey()
	if key != m.detailKey {
		m.detailKey = key
		m.detailScroll = 0
	}
	if m.tab == tabSearch && key != "" {
		if _, ok := m.byKey[key]; ok {
			return nil
		}
		if _, ok := m.info[key]; ok || m.infoLoading[key] || m.infoErr[key] != nil {
			return nil
		}
		// Debounce: only fetch if the cursor rests here briefly.
		return tea.Tick(140*time.Millisecond, func(time.Time) tea.Msg { return infoReqMsg{key} })
	}
	return nil
}

func (m *model) setTab(t tabID) tea.Cmd {
	if t == m.tab {
		return nil
	}
	m.tab = t
	m.filtering = false
	m.filter.Blur()
	var cmds []tea.Cmd
	if t == tabSearch && len(m.results) == 0 {
		m.searchFocused = true
		cmds = append(cmds, m.search.Focus())
	}
	m.clampCursor()
	cmds = append(cmds, m.afterMove())
	return tea.Batch(cmds...)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		listW, _, _ := m.layout()
		m.search.SetWidth(listW - 8)
		m.filter.SetWidth(listW - 8)
		m.clampCursor()
		return m, nil

	case tea.BackgroundColorMsg:
		m.th = newTheme(msg.IsDark())
		m.applyTheme()
		return m, nil

	case spinner.TickMsg:
		if !m.busy() {
			m.spinning = false
			return m, nil
		}
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		m.spinning = true
		return m, cmd

	case versionMsg:
		m.version = string(msg)
		return m, nil

	case installedMsg:
		m.loadingInstalled = false
		m.errInstalled = msg.err
		if msg.err == nil {
			m.installed = msg.pkgs
			m.byKey = make(map[string]*Pkg, len(msg.pkgs))
			for _, p := range msg.pkgs {
				m.byKey[p.Key()] = p
			}
		}
		m.clampCursor()
		next := m.afterMove()
		return m, next

	case outdatedMsg:
		m.loadingOutdated = false
		m.errOutdated = msg.err
		if msg.err == nil {
			m.outdated = msg.items
			m.outdatedByKey = make(map[string]Outdated, len(msg.items))
			for _, o := range msg.items {
				m.outdatedByKey[o.Key()] = o
			}
			for k := range m.selected {
				if _, ok := m.outdatedByKey[k]; !ok {
					delete(m.selected, k)
				}
			}
		}
		m.clampCursor()
		return m, nil

	case servicesMsg:
		m.loadingServices = false
		m.errServices = msg.err
		if msg.err == nil {
			m.services = msg.items
		}
		m.clampCursor()
		return m, nil

	case searchMsg:
		if msg.query != m.lastQuery {
			return m, nil
		}
		m.searching = false
		m.results, m.errSearch = msg.results, msg.err
		m.cur[tabSearch] = cursor{}
		m.detailKey = ""
		next := m.afterMove()
		return m, next

	case infoReqMsg:
		if msg.key != m.currentKey() || m.infoLoading[msg.key] {
			return m, nil
		}
		if _, ok := m.info[msg.key]; ok {
			return m, nil
		}
		for _, r := range m.results {
			if r.Key() == msg.key {
				m.infoLoading[msg.key] = true
				next := tea.Batch(loadInfo(msg.key, r.Name, r.Kind), m.ensureSpin())
				return m, next
			}
		}
		return m, nil

	case infoMsg:
		delete(m.infoLoading, msg.key)
		if msg.err != nil {
			m.infoErr[msg.key] = msg.err
		} else {
			m.info[msg.key] = msg.pkg
		}
		return m, nil

	case quickDoneMsg:
		if msg.err != nil {
			next := tea.Batch(m.setToast(msg.err.Error(), true), m.reload())
			return m, next
		}
		next := tea.Batch(m.setToast(msg.ok, false), m.reload())
		return m, next

	case toastExpireMsg:
		if msg.id == m.toastID {
			m.toast = ""
		}
		return m, nil

	case taskLineMsg:
		if m.task == nil || msg.id != m.task.id {
			return m, nil
		}
		m.task.lines = append(m.task.lines, msg.line)
		return m, waitTask(m.task.ch)

	case taskDoneMsg:
		if m.task == nil || msg.id != m.task.id {
			return m, nil
		}
		t := m.task
		t.running = false
		t.err = msg.err
		t.ended = time.Now()
		// Search info for touched packages is stale now.
		m.info = map[string]*Pkg{}
		m.infoErr = map[string]error{}
		var toast tea.Cmd
		if msg.err != nil && t.softFail {
			toast = m.setToast("! "+t.title+" — finished with warnings", true)
		} else if msg.err == nil {
			toast = m.setToast("✓ "+t.title+" — done in "+fmtDuration(t.elapsed()), false)
		} else {
			toast = m.setToast("✗ "+t.title+" failed", true)
		}
		next := tea.Batch(toast, m.reload())
		return m, next

	case execDoneMsg:
		m.task = nil
		m.info = map[string]*Pkg{}
		m.infoErr = map[string]error{}
		next := m.reload()
		return m, next

	case tea.MouseWheelMsg:
		return m.handleWheel(msg)

	case tea.MouseClickMsg:
		return m.handleClick(msg)

	case tea.KeyPressMsg:
		return m.handleKey(msg)
	}

	// Let focused inputs process other messages (cursor blink etc).
	var cmds []tea.Cmd
	var cmd tea.Cmd
	if m.filtering {
		m.filter, cmd = m.filter.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.searchFocused {
		m.search, cmd = m.search.Update(msg)
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

// --- actions ----------------------------------------------------------------

func (m *model) runTask(title string, args ...string) tea.Cmd {
	t, cmd := startTask(title, args...)
	m.task = t
	return tea.Batch(cmd, m.ensureSpin())
}

func (m *model) askUninstall(p *Pkg) {
	body := []string{
		fmt.Sprintf("Remove %s %s %s?", p.Kind, p.Name, p.Version),
	}
	if len(p.UsedBy) > 0 {
		body = append(body, "",
			fmt.Sprintf("⚠ Required by %d installed package(s):", len(p.UsedBy)),
			"  "+strings.Join(p.UsedBy, ", "),
			"Homebrew will refuse unless those are removed first.")
	}
	m.confirm = &confirmDlg{
		title:   "Uninstall " + p.Name,
		body:    body,
		okLabel: "Uninstall",
		danger:  true,
		focusOK: true,
		onOK: func(m *model) tea.Cmd {
			return m.runTask("Uninstalling "+p.Name, "uninstall", p.Kind.flag(), p.Name)
		},
	}
}

func (m *model) upgradeOutdated() tea.Cmd {
	var targets []Outdated
	for _, o := range m.outdated {
		if m.selected[o.Key()] {
			targets = append(targets, o)
		}
	}
	if len(targets) == 0 {
		v := m.visOutdated()
		if i := m.cur[tabOutdated].idx; i < len(v) {
			targets = append(targets, v[i])
		}
	}
	if len(targets) == 0 {
		return nil
	}
	var names, skipped []string
	for _, o := range targets {
		if o.Pinned {
			skipped = append(skipped, o.Name)
			continue
		}
		names = append(names, o.Name)
	}
	if len(names) == 0 {
		return m.setToast("⚑ "+strings.Join(skipped, ", ")+" is pinned — unpin it from the Installed tab first", true)
	}
	if len(names) == 1 {
		return m.runTask("Upgrading "+names[0], "upgrade", names[0])
	}
	body := []string{fmt.Sprintf("Upgrade %d packages?", len(names)), ""}
	for _, o := range targets {
		if !o.Pinned {
			body = append(body, fmt.Sprintf("  %s  %s → %s", o.Name, o.Installed, o.Current))
		}
	}
	if len(skipped) > 0 {
		body = append(body, "", "Skipping pinned: "+strings.Join(skipped, ", "))
	}
	m.confirm = &confirmDlg{
		title:   "Upgrade",
		body:    body,
		okLabel: "Upgrade",
		focusOK: true,
		onOK: func(m *model) tea.Cmd {
			m.selected = map[string]bool{}
			return m.runTask(fmt.Sprintf("Upgrading %d packages", len(names)), append([]string{"upgrade"}, names...)...)
		},
	}
	return nil
}
