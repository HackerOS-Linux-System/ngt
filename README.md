# ngt

Cli night tool — a dual-panel TUI file manager with `cd`, `mkdir`, `touch`, `cp`, `mv`, `rm`
and the `hedit` editor, plus SFTP, Podman containers, fuzzy search and bulk rename.

**This is the H# rewrite of the original Go program.** It is built with
[Bit](https://github.com/HackerOS-Linux-System/bit) (`Bit.hk`) and uses the
[tui](libs/tui) library. The look and the behaviour follow the Go version
(bubbletea / bubbles / lipgloss); I compared them cell by cell in a pty.

## Build & run

```sh
bit build --release          # → cache/build/release/ngt
bit run                      # debug build + run
```

`build.hl` does the same for Hacker Lang users. `tui` is vendored in `libs/tui`
(`-> tui => path libs/tui` in `Bit.hk`), so the build needs no network.

Runtime needs: `find`, `git` (branch / status marks), `ssh` (SFTP), `podman` (container browser),
`unzip` / `tar` (archives) — all optional, only for the matching feature.

## Keys

| Key | Action |
|-----|--------|
| Tab | switch panel |
| ↑ ↓ / j k | move cursor |
| Enter / l / → | run the command typed in the command line |
| Backspace / h / ← | `cd ..` |
| r | refresh panel |
| F1 | help |
| F5 / F6 | copy / move to the other panel |
| F8 | delete (with confirmation) |
| Ctrl+P | fuzzy search |
| Ctrl+R | bulk rename (regex) |
| Ctrl+O | sub-shell |
| Ctrl+Z | suspend |
| Ctrl+D | podman container browser |
| q / Ctrl+C | quit |

Commands: `cd`, `mkdir`, `touch`, `cp`, `mv`, `rm`, `hedit`, `sftp`, `podman`, `podmanls`;
anything else is run as a system command in the active panel's directory.
`sftp` takes `sftp://user:pass@host[:port]`.

## Source layout (`src/`)

| File | Go original | What it is |
|------|-------------|------------|
| `main.h#` | `main.go` | terminal setup, main loop, blink, resize, sub-shell, suspend |
| `model.h#` | `model.go`, `keys.go` | state: panels, modes, selection |
| `theme.h#` | `styles.go` | colours and every style |
| `update.h#` | `update.go` | key handling for each mode |
| `ops.h#` | `commands.go` | command dispatcher, cp / mv / rm, editor, sftp, podman |
| `panel.h#` | `panel_ops.go` | refresh, sorting, git, fuzzy search, bulk rename |
| `vfs.h#` | `vfs.go` | local / sftp / podman / tar / zip file systems |
| `view.h#` | `view.go` | every screen, progress bar (Luv gradient like bubbles) |
| `flist.h#` | bubbles `list` | the two-line list: pagination, filter, help |
| `tinput.h#` | bubbles `textinput` | command line / fuzzy / filter input |
| `editor.h#` | bubbles `textarea` | `hedit` text area |
| `fuzzy.h#` | sahilm/fuzzy | the matcher behind the list filter |
| `lg.h#` | lipgloss | Style.Render, borders, JoinHorizontal/Vertical, Place |
| `text.h#` | reflow, go-runewidth | width, truncate, word-wrap, UTF-8 |
| `screen.h#` | bubbletea renderer | frame output (top lines are cut when the frame is taller than the terminal — as in the original) |

## Notes on the port

Kept exactly as in the Go program, even where it looks odd: the command line is always focused, so
`j k h l q r`, Enter, Backspace and Space are intercepted before they can be typed; F5 / F6 operate on
`.`; Esc quits; the file preview is computed in Go but never drawn.

Changed on purpose:

* **Podman listing** — the Go parser needed 9 `ls` fields (real output has 8), so the panel was always empty.
* **Archives (tar / zip)** — they listed nothing in Go; now they list and navigate.
* **Ctrl+Z** — in Go it re-sent SIGTSTP to itself in a busy loop; now it really suspends.
* **Ctrl+O** — the shell gets the real terminal.
* **SFTP** — port defaults to 22; transfers stream over `ssh … cat` through one shared connection.
* **cp / mv / rm** run in the main loop and redraw the progress bar after each file (Go used goroutines).
* Syntax-highlighted preview (chroma) was not ported — it was never visible.
* Colours assume a dark terminal background.

## tui changes

`libs/tui` is the tui library, extended for this program (see `libs/tui/CHANGES-ngt.md`):
a buffered key reader (`input_new` / `input_poll`), bracketed-paste markers, `alt+enter`,
`term_size_fast`.

## Tests

`tests/smoke.py` drives the built binary in a pty (needs `pip install pyte`).
