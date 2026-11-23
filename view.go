package main

import (
	"fmt"
	"image/color"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func (m model) View() tea.View {
	var v tea.View
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	v.WindowTitle = "🍺 cellar"
	if m.w == 0 || m.h == 0 {
		return v
	}

	listW, detailW, bodyH := m.layout()
	body := m.viewList(listW, bodyH)
	if detailW == 0 && m.showDetail {
		body = m.viewDetail(m.w, bodyH)
	}
	if detailW > 0 {
		body = lipgloss.JoinHorizontal(lipgloss.Top, body, m.viewDetail(detailW, bodyH))
	}
	screen := strings.Join([]string{m.viewHeader(), body, m.viewFooter()}, "\n")

	var overlay string
	switch {
	case m.task != nil:
		overlay = m.viewTask()
	case m.confirm != nil:
		overlay = m.viewConfirm()
	case m.showHelp:
		overlay = m.viewHelp()
	}
	if overlay != "" {
		// Dim everything behind the modal so it pops.
		dim := m.th.s(m.th.faint)
		lines := strings.Split(ansi.Strip(screen), "\n")
		for i, l := range lines {
			lines[i] = dim.Render(l)
		}
		screen = strings.Join(lines, "\n")
		ow, oh := lipgloss.Width(overlay), lipgloss.Height(overlay)
		x, y := max(0, (m.w-ow)/2), max(0, (m.h-oh)/2)
		screen = lipgloss.NewCanvas(m.w, m.h).
			Compose(lipgloss.NewCompositor(
				lipgloss.NewLayer(screen),
				lipgloss.NewLayer(overlay).X(x).Y(y).Z(1),
			)).
			Render()
	}
	v.SetContent(screen)
	return v
}

// --- header -----------------------------------------------------------------

const logo = " cellar "

func (m model) tabLabel(i tabID) string {
	label := fmt.Sprintf(" %d %s", i+1, tabNames[i])
	switch i {
	case tabInstalled:
		if !m.loadingInstalled || len(m.installed) > 0 {
			label += fmt.Sprintf(" %d", len(m.installed))
		}
	case tabOutdated:
		if !m.loadingOutdated || len(m.outdated) > 0 {
			label += fmt.Sprintf(" %d", len(m.outdated))
		}
	case tabSearch:
		if len(m.results) > 0 {
			label += fmt.Sprintf(" %d", len(m.results))
		}
	case tabServices:
		running := 0
		for _, s := range m.services {
			if s.Status == "started" {
				running++
			}
		}
		if running > 0 {
			label += fmt.Sprintf(" %d", running)
		}
	}
	return label + " "
}

// tabSpans returns the [start, end) screen columns of each tab label.
func (m model) tabSpans() [numTabs][2]int {
	var spans [numTabs][2]int
	x := lipgloss.Width(logo) + 2
	for i := range numTabs {
		w := lipgloss.Width(m.tabLabel(i))
		spans[i] = [2]int{x, x + w}
		x += w + 1
	}
	return spans
}

func (m model) viewHeader() string {
	t := m.th
	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Background(t.accent).Foreground(t.onAccent).Bold(true).Render(logo))
	b.WriteString("  ")
	for i := range numTabs {
		label := m.tabLabel(i)
		var st lipgloss.Style
		switch {
		case i == m.tab:
			st = lipgloss.NewStyle().Background(t.selBg).Foreground(t.accent).Bold(true)
		case i == tabOutdated && len(m.outdated) > 0:
			st = t.s(t.yellow)
		default:
			st = t.s(t.muted)
		}
		b.WriteString(st.Render(label))
		if i < numTabs-1 {
			b.WriteString(" ")
		}
	}
	left := b.String()

	right := t.s(t.faint).Render(m.version)
	if m.busy() {
		right = m.spin.View() + " " + t.s(t.muted).Render(m.busyLabel()) + "  " + right
	}
	gap := m.w - lipgloss.Width(left) - lipgloss.Width(right) - 1
	if gap < 1 {
		return fit(left, m.w)
	}
	return left + strings.Repeat(" ", gap) + right + " "
}

func (m model) busyLabel() string {
	switch {
	case m.task != nil && m.task.running:
		return "working"
	case m.searching:
		return "searching"
	case m.loadingInstalled || m.loadingOutdated || m.loadingServices:
		return "refreshing"
	}
	return "loading"
}

// --- list -------------------------------------------------------------------

type seg struct {
	text string
	st   lipgloss.Style
}

// row renders a list row of exactly w cells: marker, prefix, flexible name,
// right-aligned suffix. Selected rows get a full-width highlight.
func (m model) row(sel bool, w int, prefix []seg, name seg, right []seg) string {
	t := m.th
	base := lipgloss.NewStyle()
	if sel {
		base = base.Background(t.selBg)
		name.st = name.st.Foreground(t.foam).Bold(true)
	}
	r := func(s seg) string { return s.st.Inherit(base).Render(s.text) }

	marker := " "
	if sel {
		marker = "▌"
	}
	pre := r(seg{marker, t.s(t.accent)}) + r(seg{" ", lipgloss.NewStyle()})
	for _, p := range prefix {
		pre += r(p)
	}
	var suf string
	for _, s := range right {
		suf += r(s)
	}
	preW, sufW := lipgloss.Width(pre), lipgloss.Width(suf)
	avail := w - preW - sufW - 2
	if avail < 6 {
		suf, sufW = "", 0
		avail = w - preW - 1
	}
	name.text = trunc(name.text, avail)
	gap := max(0, w-preW-lipgloss.Width(name.text)-sufW-1)
	return pre + r(name) + base.Render(strings.Repeat(" ", gap)) + suf + base.Render(" ")
}

func (m model) viewList(w, h int) string {
	t := m.th
	inner := w - 4
	rows := h - 4
	c := m.cur[m.tab]
	n := m.count(m.tab)

	lines := []string{m.listHeader(inner), t.s(t.border).Render(strings.Repeat("─", inner))}

	var items []string
	switch m.tab {
	case tabInstalled:
		v := m.visInstalled()
		for i := c.off; i < min(len(v), c.off+rows); i++ {
			items = append(items, m.installedRow(v[i], i == c.idx, inner))
		}
	case tabOutdated:
		v := m.visOutdated()
		for i := c.off; i < min(len(v), c.off+rows); i++ {
			items = append(items, m.outdatedRow(v[i], i == c.idx, inner))
		}
	case tabSearch:
		for i := c.off; i < min(len(m.results), c.off+rows); i++ {
			items = append(items, m.searchRow(m.results[i], i == c.idx && !m.searchFocused, inner))
		}
	case tabServices:
		v := m.visServices()
		for i := c.off; i < min(len(v), c.off+rows); i++ {
			items = append(items, m.serviceRow(v[i], i == c.idx, inner))
		}
	}
	if len(items) == 0 {
		items = m.emptyState(inner, rows)
	}
	lines = append(lines, items...)

	note := ""
	if n > 0 {
		note = t.s(t.muted).Render(fmt.Sprintf("%d/%d", c.idx+1, n))
		if n > rows {
			pct := 0
			if n-rows > 0 {
				pct = c.off * 100 / (n - rows)
			}
			note = t.s(t.faint).Render(fmt.Sprintf("%d%% ", pct)) + note
		}
	}
	title := t.s(t.foam).Bold(true).Render(tabNames[m.tab])
	return t.panel(title, note, lines, w, h, !m.searchFocused || m.tab != tabSearch)
}

func (m model) listHeader(w int) string {
	t := m.th
	if m.filtering {
		return m.filter.View()
	}
	if q := m.filters[m.tab]; q != "" && m.tab != tabSearch {
		return t.s(t.accent).Bold(true).Render("/ ") + t.s(t.fg).Render(q) +
			t.s(t.faint).Render("   esc to clear")
	}
	switch m.tab {
	case tabInstalled:
		var parts []string
		for i := range numScopes {
			name := scopeNames[i]
			if i == m.scope {
				parts = append(parts, t.pill(t.accent, name))
			} else {
				parts = append(parts, t.s(t.muted).Padding(0, 1).Render(name))
			}
		}
		return strings.Join(parts, " ")
	case tabOutdated:
		if len(m.outdated) == 0 {
			return t.s(t.muted).Render("Nothing to upgrade")
		}
		s := t.s(t.yellow).Bold(true).Render(fmt.Sprintf("%d update%s available", len(m.outdated), plural(len(m.outdated))))
		if n := len(m.selected); n > 0 {
			s += t.s(t.faint).Render(" · ") + t.s(t.green).Render(fmt.Sprintf("%d selected", n))
		}
		return s
	case tabSearch:
		return m.search.View()
	case tabServices:
		running := 0
		for _, s := range m.services {
			if s.Status == "started" {
				running++
			}
		}
		return t.s(t.muted).Render(fmt.Sprintf("%d service%s", len(m.services), plural(len(m.services)))) +
			t.s(t.faint).Render(" · ") + t.s(t.green).Render(fmt.Sprintf("%d running", running))
	}
	return ""
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func (m model) installedRow(p *Pkg, sel bool, w int) string {
	t := m.th
	// Badge column (right-aligned, fixed width) then version column.
	var badges []seg
	bw := 0
	add := func(text string, st lipgloss.Style) {
		badges = append(badges, seg{text, st})
		bw += lipgloss.Width(text)
	}
	if p.Pinned {
		add("⚑ ", t.s(t.purple))
	}
	if _, ok := m.outdatedByKey[p.Key()]; ok {
		add("↑ ", t.s(t.yellow).Bold(true))
	}
	if p.Kind == KindFormula && !p.OnRequest {
		add("dep ", t.s(t.faint))
	}
	right := []seg{{strings.Repeat(" ", max(0, 8-bw)), lipgloss.NewStyle()}}
	right = append(right, badges...)
	right = append(right, seg{fit(trunc(p.Version, 12), 12), t.s(t.muted)})
	return m.row(sel, w,
		[]seg{{kindGlyph(p.Kind) + " ", t.s(t.kindColor(p.Kind))}},
		seg{p.Name, t.s(t.fg)},
		right)
}

func (m model) outdatedRow(o Outdated, sel bool, w int) string {
	t := m.th
	check := seg{"○ ", t.s(t.faint)}
	if m.selected[o.Key()] {
		check = seg{"◉ ", t.s(t.green).Bold(true)}
	}
	right := []seg{
		{trunc(o.Installed, 12), t.s(t.muted)},
		{" → ", t.s(t.faint)},
		{trunc(o.Current, 12), t.s(t.green).Bold(true)},
	}
	if o.Pinned {
		right = append([]seg{{"⚑ pinned  ", t.s(t.purple)}}, right...)
	}
	return m.row(sel, w,
		[]seg{check, {kindGlyph(o.Kind) + " ", t.s(t.kindColor(o.Kind))}},
		seg{o.Name, t.s(t.fg)},
		right)
}

func (m model) searchRow(r SearchResult, sel bool, w int) string {
	t := m.th
	var right []seg
	if _, ok := m.byKey[r.Key()]; ok {
		right = append(right, seg{"✓ installed", t.s(t.green)})
	} else {
		right = append(right, seg{r.Kind.String(), t.s(t.faint)})
	}
	return m.row(sel, w,
		[]seg{{kindGlyph(r.Kind) + " ", t.s(t.kindColor(r.Kind))}},
		seg{r.Name, t.s(t.fg)},
		right)
}

func (m model) statusColor(status string) color.Color {
	switch status {
	case "started", "scheduled":
		return m.th.green
	case "error":
		return m.th.red
	case "none", "stopped":
		return m.th.faint
	}
	return m.th.yellow
}

func (m model) serviceRow(s Service, sel bool, w int) string {
	t := m.th
	dot := "●"
	if s.Status == "none" || s.Status == "stopped" {
		dot = "○"
	}
	status := s.Status
	if status == "none" {
		status = "stopped"
	}
	right := []seg{{status, t.s(m.statusColor(s.Status))}}
	if s.User != "" {
		right = append(right, seg{"  " + s.User, t.s(t.faint)})
	}
	return m.row(sel, w,
		[]seg{{dot + " ", t.s(m.statusColor(s.Status))}},
		seg{s.Name, t.s(t.fg)},
		right)
}

func (m model) emptyState(w, h int) []string {
	t := m.th
	var msg []string
	loading := func(what string) []string {
		return []string{m.spin.View() + " " + t.s(t.muted).Render("Loading "+what+"…")}
	}
	errLines := func(err error) []string {
		out := []string{t.s(t.red).Bold(true).Render("✗ Something went wrong")}
		for _, l := range wrap(err.Error(), w-4) {
			out = append(out, t.s(t.red).Render(l))
		}
		return append(out, "", t.s(t.faint).Render("ctrl+r to retry"))
	}
	filtered := m.filters[m.tab] != ""

	switch m.tab {
	case tabInstalled:
		switch {
		case m.loadingInstalled && len(m.installed) == 0:
			msg = loading("installed packages")
		case m.errInstalled != nil:
			msg = errLines(m.errInstalled)
		case filtered:
			msg = []string{t.s(t.muted).Render("No matches for “" + m.filters[m.tab] + "”")}
		default:
			msg = []string{t.s(t.muted).Render("Nothing installed in this view")}
		}
	case tabOutdated:
		switch {
		case m.loadingOutdated && len(m.outdated) == 0:
			msg = loading("outdated packages")
		case m.errOutdated != nil:
			msg = errLines(m.errOutdated)
		case filtered:
			msg = []string{t.s(t.muted).Render("No matches")}
		default:
			msg = []string{
				t.s(t.foam).Bold(true).Render("🍺  All caught up!"),
				"",
				t.s(t.muted).Render("Everything is up to date."),
				t.s(t.faint).Render("Press U to fetch the newest formulae."),
			}
		}
	case tabSearch:
		switch {
		case m.searching:
			msg = []string{m.spin.View() + " " + t.s(t.muted).Render("Searching for “"+m.lastQuery+"”…")}
		case m.errSearch != nil:
			msg = errLines(m.errSearch)
		case m.lastQuery != "":
			msg = []string{t.s(t.muted).Render("No results for “" + m.lastQuery + "”")}
		default:
			msg = []string{
				t.s(t.foam).Bold(true).Render("Find something new"),
				"",
				t.s(t.muted).Render("Type a name and press enter."),
				t.s(t.faint).Render("Results include formulae ") + t.s(t.accent).Render("●") +
					t.s(t.faint).Render(" and casks ") + t.s(t.teal).Render("◆"),
			}
		}
	case tabServices:
		switch {
		case m.loadingServices && len(m.services) == 0:
			msg = loading("services")
		case m.errServices != nil:
			msg = errLines(m.errServices)
		default:
			msg = []string{t.s(t.muted).Render("No services"), "", t.s(t.faint).Render("Formulae with a background service show up here.")}
		}
	}

	// Center vertically and horizontally.
	out := make([]string, max(0, (h-len(msg))/2-1))
	for _, l := range msg {
		pad := max(0, (w-lipgloss.Width(l))/2)
		out = append(out, strings.Repeat(" ", pad)+l)
	}
	return out
}

// --- detail -----------------------------------------------------------------

func (m model) viewDetail(w, h int) string {
	t := m.th
	inner := w - 4
	rows := h - 2
	lines := m.detailLines(inner)

	maxScroll := max(0, len(lines)-rows)
	scroll := min(m.detailScroll, maxScroll)
	note := ""
	if maxScroll > 0 {
		if scroll < maxScroll {
			note = t.s(t.faint).Render("J/K scroll ↓")
		} else {
			note = t.s(t.faint).Render("J/K scroll ↑")
		}
	}
	return t.panel(t.s(t.foam).Bold(true).Render("Details"), note, lines[scroll:], w, h, false)
}

func (m model) detailLines(w int) []string {
	t := m.th
	key := m.currentKey()
	if key == "" {
		return []string{"", t.s(t.faint).Render("Select a package to see details.")}
	}

	var head []string
	switch m.tab {
	case tabOutdated:
		v := m.visOutdated()
		o := v[m.cur[tabOutdated].idx]
		head = append(head,
			t.s(t.yellow).Bold(true).Render("↑ Update available"),
			t.s(t.muted).Render(o.Installed)+t.s(t.faint).Render("  →  ")+t.s(t.green).Bold(true).Render(o.Current),
			"")
	case tabServices:
		s, _ := m.currentService()
		status := s.Status
		if status == "none" {
			status = "stopped"
		}
		head = append(head, t.s(t.foam).Bold(true).Render("⚙ "+s.Name+" service"), "")
		head = append(head, kv(t, "Status", t.s(m.statusColor(s.Status)).Bold(true).Render(status)))
		if s.User != "" {
			head = append(head, kv(t, "User", s.User))
		}
		if s.ExitCode != nil {
			head = append(head, kv(t, "Exit code", fmt.Sprint(*s.ExitCode)))
		}
		if s.File != "" {
			for i, l := range wrap(s.File, w-11) {
				label := ""
				if i == 0 {
					label = "Plist"
				}
				head = append(head, kv(t, label, t.s(t.muted).Render(l)))
			}
		}
		head = append(head, "", sectionRule(t, "Package", -1, w), "")
	case tabSearch:
		if _, installed := m.byKey[key]; !installed {
			switch {
			case m.infoLoading[key]:
				return []string{"", m.spin.View() + " " + t.s(t.muted).Render("Fetching info…")}
			case m.infoErr[key] != nil:
				out := []string{"", t.s(t.red).Render("Couldn't load info:")}
				for _, l := range wrap(m.infoErr[key].Error(), w) {
					out = append(out, t.s(t.red).Render(l))
				}
				return out
			case m.info[key] == nil:
				return []string{"", t.s(t.faint).Render("…")}
			}
		}
	}

	p := m.currentPkg()
	if p == nil {
		return head
	}
	return append(head, m.pkgDetail(p, w)...)
}

func kv(t theme, k, v string) string {
	return t.s(t.muted).Render(fmt.Sprintf("%-10s", k)) + " " + v
}

func sectionRule(t theme, title string, n int, w int) string {
	s := t.s(t.accent).Bold(true).Render(strings.ToUpper(title))
	if n >= 0 {
		s += t.s(t.muted).Render(fmt.Sprintf(" %d", n))
	}
	fill := w - lipgloss.Width(s) - 1
	if fill > 0 {
		s += " " + t.s(t.border).Render(strings.Repeat("─", fill))
	}
	return s
}

// flow lays out styled words left-to-right, wrapping at w.
func flow(words []string, w int, sep string) []string {
	var lines []string
	cur, curW := "", 0
	sepW := lipgloss.Width(sep)
	for _, wd := range words {
		ww := lipgloss.Width(wd)
		if curW > 0 && curW+sepW+ww > w {
			lines = append(lines, cur)
			cur, curW = "", 0
		}
		if curW > 0 {
			cur += sep
			curW += sepW
		}
		cur += wd
		curW += ww
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func (m model) pkgDetail(p *Pkg, w int) []string {
	t := m.th
	var L []string

	title := t.s(t.kindColor(p.Kind)).Render(kindGlyph(p.Kind)) + " " + t.s(t.foam).Bold(true).Render(p.Name)
	if p.DisplayName != "" && !strings.EqualFold(p.DisplayName, p.Name) {
		title += t.s(t.muted).Render("  " + p.DisplayName)
	}
	L = append(L, title)
	if p.Desc != "" {
		for _, l := range wrap(p.Desc, w) {
			L = append(L, t.s(t.fg).Italic(true).Render(l))
		}
	}
	L = append(L, "")

	pills := []string{t.pill(t.kindColor(p.Kind), p.Kind.String())}
	if p.Installed {
		pills = append(pills, t.pill(t.green, "installed"))
	} else {
		pills = append(pills, t.pill(t.faint, "not installed"))
	}
	o, isOutdated := m.outdatedByKey[p.Key()]
	if isOutdated {
		pills = append(pills, t.pill(t.yellow, "outdated"))
	}
	if p.Pinned {
		pills = append(pills, t.pill(t.purple, "pinned"))
	}
	if p.Installed && p.Kind == KindFormula && !p.OnRequest {
		pills = append(pills, t.pill(t.muted, "dependency"))
	}
	if p.KegOnly {
		pills = append(pills, t.pill(t.muted, "keg-only"))
	}
	if p.AutoUpdates {
		pills = append(pills, t.pill(t.teal, "auto-updates"))
	}
	if p.Deprecated {
		pills = append(pills, t.pill(t.red, "deprecated"))
	}
	if p.Disabled {
		pills = append(pills, t.pill(t.red, "disabled"))
	}
	L = append(L, flow(pills, w, " ")...)
	L = append(L, "")

	if p.Installed {
		v := t.s(t.fg).Render(p.Version)
		if isOutdated {
			v += t.s(t.faint).Render(" → ") + t.s(t.green).Bold(true).Render(o.Current)
		}
		L = append(L, kv(t, "Version", v))
		if !p.InstalledAt.IsZero() {
			L = append(L, kv(t, "Installed", t.s(t.fg).Render(relTime(p.InstalledAt))+
				t.s(t.faint).Render("  "+p.InstalledAt.Format("Jan 2, 2006"))))
		}
	} else if p.Latest != "" {
		L = append(L, kv(t, "Version", t.s(t.fg).Render(p.Latest)))
	}
	if p.Tap != "" {
		L = append(L, kv(t, "Tap", t.s(t.fg).Render(p.Tap)))
	}
	if p.License != "" {
		L = append(L, kv(t, "License", t.s(t.fg).Render(p.License)))
	}
	if p.Homepage != "" {
		L = append(L, kv(t, "Homepage", t.s(t.accent).Underline(true).Hyperlink(p.Homepage).Render(trunc(p.Homepage, w-11))))
	}
	if len(p.Aliases) > 0 {
		L = append(L, kv(t, "Aliases", t.s(t.fg).Render(strings.Join(p.Aliases, ", "))))
	}

	styledList := func(names []string) []string {
		words := make([]string, len(names))
		for i, n := range names {
			st := t.s(t.fg)
			if _, ok := m.byKey[pkgKey(KindFormula, n)]; !ok && p.Installed {
				st = t.s(t.muted)
			}
			words[i] = st.Render(n)
		}
		return flow(words, w, t.s(t.faint).Render(" · "))
	}
	if len(p.Deps) > 0 {
		L = append(L, "", sectionRule(t, "Depends on", len(p.Deps), w))
		L = append(L, styledList(p.Deps)...)
	}
	if len(p.UsedBy) > 0 {
		L = append(L, "", sectionRule(t, "Used by", len(p.UsedBy), w))
		L = append(L, styledList(p.UsedBy)...)
	}
	if p.Caveats != "" {
		L = append(L, "", sectionRule(t, "Caveats", -1, w))
		for _, para := range strings.Split(p.Caveats, "\n") {
			for _, l := range wrap(para, w) {
				L = append(L, t.s(t.muted).Render(l))
			}
		}
	}
	return L
}

// --- footer -----------------------------------------------------------------

type hint struct{ key, desc string }

func (m model) hints() []hint {
	switch {
	case m.filtering:
		return []hint{{"type", "filter"}, {"↑↓", "move"}, {"enter", "done"}, {"esc", "clear"}}
	case m.tab == tabSearch && m.searchFocused:
		return []hint{{"enter", "search"}, {"↓/esc", "results"}, {"tab", "switch tab"}}
	}
	var h []hint
	switch m.tab {
	case tabInstalled:
		h = []hint{{"u", "upgrade"}, {"x", "uninstall"}, {"p", "pin"}, {"s", "scope"}, {"o", "homepage"}}
	case tabOutdated:
		h = []hint{{"space", "select"}, {"a", "all"}, {"u", "upgrade"}}
	case tabSearch:
		h = []hint{{"/", "new search"}, {"i", "install"}, {"o", "homepage"}}
	case tabServices:
		h = []hint{{"s", "start"}, {"x", "stop"}, {"R", "restart"}}
	}
	if m.tab != tabSearch {
		h = append(h, hint{"/", "filter"})
	}
	if _, detailW, _ := m.layout(); detailW == 0 {
		h = append([]hint{{"v", "details"}}, h...)
	}
	return append(h, hint{"?", "help"}, hint{"q", "quit"})
}

func (m model) renderHints(hs []hint) string {
	t := m.th
	parts := make([]string, len(hs))
	for i, h := range hs {
		parts[i] = t.s(t.foam).Bold(true).Render(h.key) + " " + t.s(t.muted).Render(h.desc)
	}
	return strings.Join(parts, t.s(t.faint).Render("  ·  "))
}

func (m model) viewFooter() string {
	t := m.th
	left := " " + m.renderHints(m.hints())
	if m.toast == "" {
		return fit(left, m.w)
	}
	st := t.s(t.green)
	if m.toastErr {
		st = t.s(t.red)
	}
	right := st.Render(trunc(m.toast, m.w/2)) + " "
	avail := m.w - lipgloss.Width(right) - 1
	return fit(left, avail) + " " + right
}

// --- overlays ---------------------------------------------------------------

func (m model) modal(title string, lines []string, w int, accent color.Color) string {
	t := m.th
	inner := w - 6
	var body []string
	for _, l := range lines {
		body = append(body, fit(l, inner))
	}
	content := strings.Join(body, "\n")
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(accent).
		Padding(1, 2).
		Render(t.s(accent).Bold(true).Render(title) + "\n\n" + content)
}

func (m model) viewConfirm() string {
	t := m.th
	c := m.confirm
	w := min(max(56, m.w/2), m.w-4)
	accent := t.accent
	if c.danger {
		accent = t.red
	}
	lines := []string{}
	for _, l := range c.body {
		for _, wl := range wrap(l, w-6) {
			lines = append(lines, t.s(t.fg).Render(wl))
		}
	}
	btn := func(label string, focused bool, col color.Color) string {
		if focused {
			return lipgloss.NewStyle().Background(col).Foreground(t.onAccent).Bold(true).Padding(0, 2).Render(label)
		}
		return lipgloss.NewStyle().Foreground(t.muted).Padding(0, 2).Render(label)
	}
	buttons := btn(c.okLabel, c.focusOK, accent) + "  " + btn("Cancel", !c.focusOK, t.muted)
	lines = append(lines, "", buttons, "", t.s(t.faint).Render("y confirm · n/esc cancel · ←→ choose"))
	return m.modal(c.title, lines, w, accent)
}

func (m model) viewTask() string {
	t := m.task
	th := m.th
	w := max(min(m.w-6, 110), 40)
	h := max(m.h-4, 10)
	inner := w - 4
	logRows := h - 6

	// Title line.
	var status string
	accent := th.accent
	switch {
	case t.running:
		status = m.spin.View() + " " + th.s(th.foam).Bold(true).Render(t.title) +
			th.s(th.faint).Render("  "+fmtDuration(t.elapsed()))
	case t.err != nil && t.softFail:
		accent = th.yellow
		status = th.s(th.yellow).Bold(true).Render("! "+t.title) + th.s(th.muted).Render(" — finished with warnings")
	case t.err == nil:
		accent = th.green
		status = th.s(th.green).Bold(true).Render("✓ "+t.title) + th.s(th.muted).Render(" — done in "+fmtDuration(t.elapsed()))
	default:
		accent = th.red
		status = th.s(th.red).Bold(true).Render("✗ "+t.title) + th.s(th.muted).Render(" — "+t.err.Error())
	}

	// Log lines, wrapped, styled.
	var all []string
	for _, l := range t.lines {
		for _, wl := range wrap(l, inner) {
			all = append(all, m.styleLog(wl))
		}
	}
	maxScroll := max(0, len(all)-logRows)
	scroll := min(t.scroll, maxScroll)
	end := len(all) - scroll
	start := max(0, end-logRows)
	logLines := all[start:end]

	var footer string
	switch {
	case t.running:
		footer = m.renderHints([]hint{{"↑↓", "scroll"}, {"ctrl+c", "cancel"}})
	case t.err != nil && !t.softFail:
		footer = m.renderHints([]hint{{"t", "retry in terminal"}, {"↑↓", "scroll"}, {"esc", "close"}})
	default:
		footer = m.renderHints([]hint{{"↑↓", "scroll"}, {"enter/esc", "close"}})
	}
	if scroll > 0 {
		footer += th.s(th.faint).Render(fmt.Sprintf("   ↑ %d more below", scroll))
	}

	lines := []string{status, th.s(th.border).Render(strings.Repeat("─", inner))}
	lines = append(lines, logLines...)
	for len(lines) < logRows+2 {
		lines = append(lines, "")
	}
	lines = append(lines, th.s(th.border).Render(strings.Repeat("─", inner)), footer)

	b := th.s(accent)
	out := []string{b.Render("╭" + strings.Repeat("─", w-2) + "╮")}
	for _, l := range lines {
		out = append(out, b.Render("│")+" "+fit(l, inner)+" "+b.Render("│"))
	}
	out = append(out, b.Render("╰"+strings.Repeat("─", w-2)+"╯"))
	return strings.Join(out, "\n")
}

func (m model) styleLog(l string) string {
	t := m.th
	trim := strings.TrimSpace(l)
	switch {
	case strings.HasPrefix(l, "$ "):
		return t.s(t.faint).Render(l)
	case strings.HasPrefix(trim, "==>"):
		return t.s(t.accent).Bold(true).Render("▸") + t.s(t.foam).Bold(true).Render(strings.TrimPrefix(trim, "==>"))
	case strings.HasPrefix(trim, "Error"), strings.HasPrefix(trim, "fatal"):
		return t.s(t.red).Bold(true).Render(l)
	case strings.HasPrefix(trim, "Warning"):
		return t.s(t.yellow).Render(l)
	case strings.HasPrefix(trim, "🍺"), strings.HasPrefix(trim, "✔"):
		return t.s(t.green).Render(l)
	case strings.HasPrefix(trim, "^C"):
		return t.s(t.yellow).Render(l)
	}
	return t.s(t.fg).Render(l)
}

func (m model) viewHelp() string {
	t := m.th
	section := func(title string, hs []hint) []string {
		out := []string{t.s(t.accent).Bold(true).Render(title)}
		for _, h := range hs {
			out = append(out, "  "+t.s(t.foam).Bold(true).Render(fmt.Sprintf("%-11s", h.key))+t.s(t.muted).Render(h.desc))
		}
		return append(out, "")
	}
	left := section("Navigate", []hint{
		{"tab / 1-4", "switch tab"}, {"↑↓ / jk", "move"}, {"g / G", "top / bottom"},
		{"pgup/pgdn", "page"}, {"J / K", "scroll details"}, {"v", "details (narrow)"}, {"/", "filter / search"}, {"esc", "clear filter"},
	})
	left = append(left, section("Homebrew", []hint{
		{"U", "brew update"}, {"C", "brew cleanup"}, {"D", "brew doctor"}, {"ctrl+r", "refresh lists"}, {"o", "open homepage"},
	})...)
	right := section("Installed", []hint{
		{"u", "upgrade"}, {"x", "uninstall"}, {"R", "reinstall"}, {"p", "pin / unpin"}, {"s / ←→", "cycle scope"},
	})
	right = append(right, section("Outdated", []hint{{"space", "toggle select"}, {"a", "select all"}, {"u / enter", "upgrade"}})...)
	right = append(right, section("Search", []hint{{"enter / i", "install"}, {"x", "uninstall"}})...)
	right = append(right, section("Services", []hint{{"s", "start"}, {"x", "stop"}, {"R", "restart"}})...)

	colW := 32
	var rows []string
	for i := 0; i < max(len(left), len(right)); i++ {
		l, r := "", ""
		if i < len(left) {
			l = left[i]
		}
		if i < len(right) {
			r = right[i]
		}
		rows = append(rows, fit(l, colW)+r)
	}
	rows = append(rows, t.s(t.faint).Render("Legend: ")+t.s(t.accent).Render("● formula  ")+t.s(t.teal).Render("◆ cask  ")+
		t.s(t.yellow).Render("↑ outdated  ")+t.s(t.purple).Render("⚑ pinned"))
	return m.modal("Keyboard shortcuts", rows, colW*2+6, t.accent)
}
