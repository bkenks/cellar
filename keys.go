package main

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

func (m model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	k := msg.String()

	switch {
	case m.task != nil:
		return m.taskKey(k)
	case m.confirm != nil:
		return m.confirmKey(k)
	case m.showHelp:
		m.showHelp = false
		if k == "ctrl+c" {
			return m, tea.Quit
		}
		return m, nil
	}

	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if m.filtering {
		return m.filterKey(msg)
	}
	if m.tab == tabSearch && m.searchFocused {
		return m.searchKey(msg)
	}

	rows := m.listRows()
	switch k {
	case "q":
		return m, tea.Quit
	case "?":
		m.showHelp = true
		return m, nil
	case "v":
		if _, detailW, _ := m.layout(); detailW == 0 {
			m.showDetail = !m.showDetail
		}
		return m, nil
	case "tab":
		next := m.setTab((m.tab + 1) % numTabs)
		return m, next
	case "shift+tab":
		next := m.setTab((m.tab + numTabs - 1) % numTabs)
		return m, next
	case "1", "2", "3", "4":
		next := m.setTab(tabID(k[0] - '1'))
		return m, next
	case "up", "k":
		next := m.move(-1)
		return m, next
	case "down", "j":
		next := m.move(1)
		return m, next
	case "pgup", "ctrl+b":
		next := m.move(-rows)
		return m, next
	case "pgdown", "ctrl+f":
		next := m.move(rows)
		return m, next
	case "home", "g":
		next := m.move(-1 << 30)
		return m, next
	case "end", "G":
		next := m.move(1 << 30)
		return m, next
	case "J", "shift+down", "ctrl+d":
		m.detailScroll += 3
		return m, nil
	case "K", "shift+up", "ctrl+u":
		m.detailScroll = max(0, m.detailScroll-3)
		return m, nil
	case "/":
		if m.tab == tabSearch {
			m.searchFocused = true
			next := m.search.Focus()
			return m, next
		}
		m.filtering = true
		m.filter.SetValue(m.filters[m.tab])
		m.filter.CursorEnd()
		next := m.filter.Focus()
		return m, next
	case "esc":
		if m.filters[m.tab] != "" {
			m.filters[m.tab] = ""
			m.clampCursor()
			next := m.afterMove()
			return m, next
		}
		if m.tab == tabSearch {
			m.searchFocused = true
			next := m.search.Focus()
			return m, next
		}
		return m, nil
	case "ctrl+r":
		next := tea.Batch(m.reload(), m.setToast("Refreshing…", false))
		return m, next
	case "U":
		next := m.runTask("Updating Homebrew", "update")
		return m, next
	case "D":
		next := m.runTask("Running brew doctor", "doctor")
		m.task.softFail = true
		return m, next
	case "C":
		m.confirm = &confirmDlg{
			title:   "Clean up",
			body:    []string{"Remove stale lock files, outdated downloads and", "old versions of installed packages?", "", "Runs `brew cleanup`."},
			okLabel: "Clean up",
			focusOK: true,
			onOK: func(m *model) tea.Cmd {
				return m.runTask("Cleaning up", "cleanup")
			},
		}
		return m, nil
	case "o":
		if p := m.currentPkg(); p != nil && p.Homepage != "" {
			next := tea.Batch(openURL(p.Homepage), m.setToast("Opening "+p.Homepage, false))
			return m, next
		}
		return m, nil
	}

	switch m.tab {
	case tabInstalled:
		return m.installedKey(k)
	case tabOutdated:
		return m.outdatedKey(k)
	case tabSearch:
		return m.searchListKey(k)
	case tabServices:
		return m.servicesKey(k)
	}
	return m, nil
}

func (m model) installedKey(k string) (tea.Model, tea.Cmd) {
	if k == "s" || k == "left" || k == "right" || k == "h" || k == "l" {
		if k == "left" || k == "h" {
			m.scope = (m.scope + numScopes - 1) % numScopes
		} else {
			m.scope = (m.scope + 1) % numScopes
		}
		m.clampCursor()
		next := m.afterMove()
		return m, next
	}
	p := m.currentPkg()
	if p == nil {
		return m, nil
	}
	switch k {
	case "u":
		if p.Pinned {
			next := m.setToast("⚑ "+p.Name+" is pinned — press p to unpin", true)
			return m, next
		}
		if _, ok := m.outdatedByKey[p.Key()]; !ok {
			next := m.setToast(p.Name+" is already up to date", false)
			return m, next
		}
		next := m.runTask("Upgrading "+p.Name, "upgrade", p.Kind.flag(), p.Name)
		return m, next
	case "x", "d", "delete", "backspace":
		m.askUninstall(p)
		return m, nil
	case "R":
		next := m.runTask("Reinstalling "+p.Name, "reinstall", p.Kind.flag(), p.Name)
		return m, next
	case "p":
		if p.Kind != KindFormula {
			next := m.setToast("Only formulae can be pinned", true)
			return m, next
		}
		if p.Pinned {
			next := tea.Batch(quick("Unpinned "+p.Name, "unpin", p.Name), m.ensureSpin())
			return m, next
		}
		next := tea.Batch(quick("⚑ Pinned "+p.Name, "pin", p.Name), m.ensureSpin())
		return m, next
	}
	return m, nil
}

func (m model) outdatedKey(k string) (tea.Model, tea.Cmd) {
	v := m.visOutdated()
	switch k {
	case "space", " ":
		if i := m.cur[tabOutdated].idx; i < len(v) {
			key := v[i].Key()
			if m.selected[key] {
				delete(m.selected, key)
			} else {
				m.selected[key] = true
			}
			next := m.move(1)
			return m, next
		}
	case "a":
		all := true
		for _, o := range v {
			if !m.selected[o.Key()] {
				all = false
				break
			}
		}
		for _, o := range v {
			if all {
				delete(m.selected, o.Key())
			} else {
				m.selected[o.Key()] = true
			}
		}
	case "u", "enter":
		cmd := m.upgradeOutdated()
		return m, cmd
	}
	return m, nil
}

func (m model) searchListKey(k string) (tea.Model, tea.Cmd) {
	i := m.cur[tabSearch].idx
	if i >= len(m.results) {
		return m, nil
	}
	r := m.results[i]
	switch k {
	case "i", "enter":
		if _, ok := m.byKey[r.Key()]; ok {
			next := m.setToast(r.Name+" is already installed", false)
			return m, next
		}
		next := m.runTask("Installing "+r.Name, "install", r.Kind.flag(), r.Name)
		return m, next
	case "x", "d":
		if p, ok := m.byKey[r.Key()]; ok {
			m.askUninstall(p)
		}
	}
	return m, nil
}

func (m model) servicesKey(k string) (tea.Model, tea.Cmd) {
	s, ok := m.currentService()
	if !ok {
		return m, nil
	}
	switch k {
	case "s", "enter":
		if s.Status == "started" {
			next := m.setToast(s.Name+" is already running", false)
			return m, next
		}
		next := m.runTask("Starting "+s.Name, "services", "start", s.Name)
		return m, next
	case "x":
		next := m.runTask("Stopping "+s.Name, "services", "stop", s.Name)
		return m, next
	case "R":
		next := m.runTask("Restarting "+s.Name, "services", "restart", s.Name)
		return m, next
	}
	return m, nil
}

func (m model) filterKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		m.filtering = false
		m.filter.Blur()
		return m, nil
	case "esc":
		m.filtering = false
		m.filter.Blur()
		m.filters[m.tab] = ""
		m.clampCursor()
		next := m.afterMove()
		return m, next
	case "up":
		next := m.move(-1)
		return m, next
	case "down":
		next := m.move(1)
		return m, next
	case "tab", "shift+tab":
		m.filtering = false
		m.filter.Blur()
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.filter, cmd = m.filter.Update(msg)
	if v := m.filter.Value(); v != m.filters[m.tab] {
		m.filters[m.tab] = v
		m.cur[m.tab] = cursor{}
		m.clampCursor()
		next := tea.Batch(cmd, m.afterMove())
		return m, next
	}
	return m, cmd
}

func (m model) searchKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		q := strings.TrimSpace(m.search.Value())
		if q == "" {
			return m, nil
		}
		m.lastQuery = q
		m.searching = true
		m.errSearch = nil
		m.searchFocused = false
		m.search.Blur()
		next := tea.Batch(doSearch(q), m.ensureSpin())
		return m, next
	case "esc", "down":
		if len(m.results) > 0 || msg.String() == "esc" {
			m.searchFocused = false
			m.search.Blur()
		}
		return m, nil
	case "tab", "shift+tab":
		m.searchFocused = false
		m.search.Blur()
		return m.handleKey(msg)
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m, cmd
}

func (m model) taskKey(k string) (tea.Model, tea.Cmd) {
	t := m.task
	switch k {
	case "up", "k":
		t.scroll++
	case "down", "j":
		t.scroll = max(0, t.scroll-1)
	case "pgup", "ctrl+u":
		t.scroll += 10
	case "pgdown", "ctrl+d":
		t.scroll = max(0, t.scroll-10)
	case "end", "G":
		t.scroll = 0
	case "ctrl+c":
		if t.running {
			t.cancel()
			t.lines = append(t.lines, "^C — cancelling…")
			return m, nil
		}
		m.task = nil
	case "esc", "enter", "q", "space":
		if !t.running {
			m.task = nil
		}
	case "t":
		if !t.running && t.err != nil && !t.softFail {
			return m, runInTerminal(t.args)
		}
	}
	return m, nil
}

func (m model) confirmKey(k string) (tea.Model, tea.Cmd) {
	c := m.confirm
	switch k {
	case "left", "right", "tab", "shift+tab", "h", "l":
		c.focusOK = !c.focusOK
	case "y":
		m.confirm = nil
		cmd := c.onOK(&m)
		return m, cmd
	case "enter":
		m.confirm = nil
		if c.focusOK {
			cmd := c.onOK(&m)
			return m, cmd
		}
	case "n", "esc", "q":
		m.confirm = nil
	case "ctrl+c":
		return m, tea.Quit
	}
	return m, nil
}

// --- mouse ------------------------------------------------------------------

func (m model) handleWheel(msg tea.MouseWheelMsg) (tea.Model, tea.Cmd) {
	up := msg.Button == tea.MouseWheelUp
	down := msg.Button == tea.MouseWheelDown
	if m.task != nil {
		if up {
			m.task.scroll += 3
		} else if down {
			m.task.scroll = max(0, m.task.scroll-3)
		}
		return m, nil
	}
	if m.confirm != nil || m.showHelp {
		return m, nil
	}
	listW, _, _ := m.layout()
	if msg.X >= listW {
		if up {
			m.detailScroll = max(0, m.detailScroll-2)
		} else if down {
			m.detailScroll += 2
		}
		return m, nil
	}
	if up {
		next := m.move(-1)
		return m, next
	} else if down {
		next := m.move(1)
		return m, next
	}
	return m, nil
}

func (m model) handleClick(msg tea.MouseClickMsg) (tea.Model, tea.Cmd) {
	if m.task != nil || m.confirm != nil || msg.Button != tea.MouseLeft {
		return m, nil
	}
	if m.showHelp {
		m.showHelp = false
		return m, nil
	}
	if msg.Y == 0 {
		for i, span := range m.tabSpans() {
			if msg.X >= span[0] && msg.X < span[1] {
				next := m.setTab(tabID(i))
				return m, next
			}
		}
		return m, nil
	}
	listW, _, _ := m.layout()
	top := m.listTop()
	if msg.X < listW && msg.Y >= top && msg.Y < top+m.listRows() {
		idx := m.cur[m.tab].off + msg.Y - top
		if idx < m.count(m.tab) {
			if m.tab == tabSearch && m.searchFocused {
				m.searchFocused = false
				m.search.Blur()
			}
			m.cur[m.tab].idx = idx
			m.clampCursor()
			next := m.afterMove()
			return m, next
		}
	}
	return m, nil
}
