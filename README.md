# 🍺 cellar

A cozy terminal UI for Homebrew, built with Go and the Charm stack
(Bubble Tea v2, Bubbles v2, Lip Gloss v2).

```
go build -o cellar . && ./cellar
# or: go install . (puts `cellar` in ~/go/bin)
```

## Tabs

| Tab           | What you can do                                                        |
|---------------|------------------------------------------------------------------------|
| **Installed** | Browse formulae & casks, filter by scope (All / Formulae / Casks / Leaves), upgrade, uninstall, reinstall, pin/unpin |
| **Outdated**  | Multi-select updates (`space`, `a`) and upgrade them in one go          |
| **Search**    | Search formulae & casks; details load as you move; `enter` installs     |
| **Services**  | Start / stop / restart `brew services`                                  |

The details pane shows version info, install date, tap, license, homepage,
dependencies, **reverse dependencies** ("used by", computed locally) and caveats.

Long-running commands (install, upgrade, update…) stream their output into a
modal. If one needs an interactive terminal (e.g. a `sudo` password for some
cask uninstalls), it will fail fast — press `t` to re-run it in the real terminal.

## Keys

`?` shows everything. The highlights:

- `tab` / `1`–`4` switch tabs · `↑↓`/`jk` move · `/` filter · `J`/`K` scroll details
- `u` upgrade · `x` uninstall · `R` reinstall · `p` pin · `o` open homepage
- `U` brew update · `C` brew cleanup · `D` brew doctor · `ctrl+r` refresh
- Mouse: wheel scrolls, click tabs and rows
- Narrow terminal (< 88 cols): `v` toggles the details view

## Layout

- `brew.go` – runs brew, parses its JSON
- `task.go` – streaming command runner for the output modal
- `model.go` / `keys.go` – state and input handling
- `view.go` / `theme.go` – rendering and the amber light/dark palette
