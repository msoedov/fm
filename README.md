# fm

A small Go terminal file manager inspired by [ranger](https://github.com/ranger/ranger). Parent, current directory, and preview panes; directories sort first. On narrower terminals the parent pane is hidden.

## Run

Requires Go 1.26 or newer and an interactive terminal on macOS or Linux.

```sh
make b               # build ./fm
./fm                 # current directory
./fm ~/Downloads     # chosen directory
```

Use `make t` to run tests and vet, and `make i` to install to
`~/.local/bin/fm`. Add `~/.local/bin` to your `PATH` if needed.
Override the install location with `make i PREFIX=/usr/local` or
`make i BINDIR=/your/bin`.

Without Make, build with `go build -o fm .`.

## Keys

| Key | Action |
| --- | --- |
| `j` / `k`, arrows | Move selection |
| `h` / Left / Backspace | Parent directory |
| `l` / Right / Enter | Enter directory or open read-only viewer |
| `g` / `G`, Home / End | First / last entry |
| Page Up / Down | Page through entries or viewer |
| `J` / `K`, Ctrl-D / Ctrl-U | Scroll preview |
| `h` / `l`, Left / Right in viewer | Scroll horizontally |
| `.` | Show / hide dotfiles |
| `~` | Home directory |
| `r`, Ctrl-L | Refresh directory and preview |
| `dd` | Delete immediately without confirmation |
| Delete | Request deletion; `y` confirms, any other key cancels |
| `?` | Help |
| `q` / Esc in viewer | Back to browser |
| `q` in browser, Ctrl-C anywhere | Quit |

Text previews use Chroma syntax highlighting and never open an editor or execute files. Previews read at most 256 KiB, mark truncation, and identify binary and special files without displaying their contents. Tabs display as four spaces. Directory previews list children. File content changes appear after refresh.

Deletion is permanent, including all contents of non-empty directories. Deleting a symlink removes the link, not its target. The first `d` captures the selected entry; a second consecutive `d` deletes it without confirmation. Any other key cancels the sequence and performs its usual action. Replacements detected before deletion are rejected.

Uses tcell for terminal input/rendering and Chroma for highlighting. No external preview commands are required.

## Checks

```sh
go test ./...
go vet ./...
```

## License

[MIT](LICENSE) © 2026 Alexander Myasoedov.
