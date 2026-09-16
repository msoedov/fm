package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/uniseg"
)

var (
	normal        = tcell.StyleDefault.Foreground(tcell.ColorWhite).Background(tcell.ColorDefault)
	muted         = normal.Foreground(tcell.ColorGray)
	accent        = normal.Foreground(tcell.ColorLightSkyBlue).Bold(true)
	selectedStyle = normal.Background(tcell.ColorDarkSlateGray).Bold(true)
)

const copyLimit = 64 << 20

// Native tools first; OSC 52 through tcell is the fallback for terminals that support it.
var clipboardCommands = [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}, {"xsel", "--clipboard", "--input"}}

var opener = func() string {
	if runtime.GOOS == "darwin" {
		return "open"
	}
	return "xdg-open"
}()

type app struct {
	screen                    tcell.Screen
	cwd                       string
	entries, parent           []entry
	selected                  int
	hidden, viewer, help      bool
	preview                   preview
	previewPath               string
	previewOffset, horizontal int
	message                   string
	pending                   *entry
	deletePrefix              *entry
	copyPrefix                string
}

func (a *app) reload(selectName string) error {
	entries, err := readDir(a.cwd, a.hidden)
	if err != nil {
		return err
	}
	a.entries = entries
	a.parent, _ = readDir(filepath.Dir(a.cwd), a.hidden)
	for i, e := range entries {
		if e.name == selectName {
			a.selected = i
			break
		}
	}
	a.selected = max(0, min(a.selected, len(entries)-1))
	a.previewPath = ""
	a.updatePreview()
	return nil
}

func (a *app) current() *entry {
	if len(a.entries) == 0 {
		return nil
	}
	return &a.entries[a.selected]
}

func (a *app) updatePreview() {
	e := a.current()
	if e == nil {
		a.preview = preview{note: "Empty directory"}
		a.previewPath = ""
		return
	}
	path := filepath.Join(a.cwd, e.name)
	if path == a.previewPath {
		return
	}
	a.previewPath = path
	a.previewOffset, a.horizontal = 0, 0
	a.preview = loadPreview(path, a.hidden)
}

func (a *app) move(delta int) {
	a.selected = max(0, min(a.selected+delta, len(a.entries)-1))
	a.updatePreview()
}

func (a *app) navigate(path, selectName string) {
	old, oldSelected := a.cwd, a.selected
	a.cwd, a.selected = path, 0
	if err := a.reload(selectName); err != nil {
		a.cwd, a.selected = old, oldSelected
		a.message = err.Error()
	}
}

func (a *app) scroll(delta int) {
	_, h := a.screen.Size()
	a.previewOffset = max(0, min(a.previewOffset+delta, max(0, len(a.preview.lines)-max(1, h-5))))
}

func (a *app) key(ev *tcell.EventKey) bool {
	if ev.Key() == tcell.KeyCtrlC {
		return true
	}
	if a.deletePrefix != nil {
		target := *a.deletePrefix
		a.deletePrefix = nil
		if ev.Key() == tcell.KeyRune && ev.Rune() == 'd' {
			a.remove(target)
			return false
		}
	}
	if a.copyPrefix != "" {
		path := a.copyPrefix
		a.copyPrefix = ""
		if ev.Key() == tcell.KeyRune && ev.Rune() == 'c' {
			a.copyContent(path)
			return false
		}
	}
	if a.pending != nil {
		if ev.Key() == tcell.KeyRune && ev.Rune() == 'y' {
			target := *a.pending
			a.pending = nil
			a.remove(target)
		} else {
			a.pending = nil
			a.message = "Deletion cancelled"
		}
		return false
	}
	if a.help {
		a.help = false
		return false
	}
	a.message = ""
	_, height := a.screen.Size()
	page := max(1, height-5)
	if a.viewer {
		switch ev.Key() {
		case tcell.KeyEscape:
			a.viewer = false
		case tcell.KeyUp:
			a.scroll(-1)
		case tcell.KeyDown:
			a.scroll(1)
		case tcell.KeyPgUp:
			a.scroll(-page)
		case tcell.KeyPgDn:
			a.scroll(page)
		case tcell.KeyHome:
			a.previewOffset = 0
		case tcell.KeyEnd:
			a.scroll(len(a.preview.lines))
		case tcell.KeyLeft:
			a.horizontal = max(0, a.horizontal-4)
		case tcell.KeyRight:
			a.horizontal += 4
		case tcell.KeyRune:
			switch ev.Rune() {
			case 'q':
				a.viewer = false
			case 'j':
				a.scroll(1)
			case 'k':
				a.scroll(-1)
			case 'h':
				a.horizontal = max(0, a.horizontal-4)
			case 'l':
				a.horizontal += 4
			case 'g':
				a.previewOffset = 0
			case 'G':
				a.scroll(len(a.preview.lines))
			case 'c':
				a.copyPath()
			case 'e':
				a.edit()
			case 'o':
				a.openExternal()
			case '?':
				a.help = true
			}
		}
		return false
	}
	switch ev.Key() {
	case tcell.KeyUp:
		a.move(-1)
	case tcell.KeyDown:
		a.move(1)
	case tcell.KeyLeft, tcell.KeyBackspace, tcell.KeyBackspace2:
		a.navigate(filepath.Dir(a.cwd), filepath.Base(a.cwd))
	case tcell.KeyRight, tcell.KeyEnter:
		a.open()
	case tcell.KeyPgUp:
		a.move(-page)
	case tcell.KeyPgDn:
		a.move(page)
	case tcell.KeyHome:
		a.move(-len(a.entries))
	case tcell.KeyEnd:
		a.move(len(a.entries))
	case tcell.KeyDelete:
		a.requestDelete()
	case tcell.KeyCtrlD:
		a.scroll(page / 2)
	case tcell.KeyCtrlU:
		a.scroll(-max(1, page/2))
	case tcell.KeyCtrlL:
		a.refresh()
		a.screen.Sync()
	case tcell.KeyRune:
		switch ev.Rune() {
		case 'q':
			return true
		case 'j':
			a.move(1)
		case 'k':
			a.move(-1)
		case 'h':
			a.navigate(filepath.Dir(a.cwd), filepath.Base(a.cwd))
		case 'l':
			a.open()
		case 'g':
			a.move(-len(a.entries))
		case 'G':
			a.move(len(a.entries))
		case 'J':
			a.scroll(1)
		case 'K':
			a.scroll(-1)
		case '.':
			a.hidden = !a.hidden
			a.refresh()
		case 'r':
			a.refresh()
		case 'd':
			if e := a.current(); e != nil {
				copy := *e
				a.deletePrefix = &copy
			}
		case 'c':
			a.copyPath()
		case 'e':
			a.edit()
		case 'o':
			a.openExternal()
		case '?':
			a.help = true
		case '~':
			if home, err := os.UserHomeDir(); err == nil {
				a.navigate(home, "")
			}
		}
	}
	return false
}

func (a *app) refresh() {
	name := ""
	if e := a.current(); e != nil {
		name = e.name
	}
	if err := a.reload(name); err != nil {
		a.message = err.Error()
	}
}

func (a *app) open() {
	if e := a.current(); e != nil {
		if e.dir {
			a.navigate(filepath.Join(a.cwd, e.name), "")
		} else {
			a.viewer = true
		}
	}
}

// The shell expands $VISUAL/$EDITOR so values with arguments like "code -w" work.
func (a *app) edit() {
	e := a.current()
	if e == nil {
		return
	}
	cmd := exec.Command("sh", "-c", `${VISUAL:-${EDITOR:-vi}} "$1"`, "sh", filepath.Join(a.cwd, e.name))
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := a.screen.Suspend(); err != nil {
		a.message = "Editor failed: " + err.Error()
		return
	}
	err := cmd.Run()
	if resumeErr := a.screen.Resume(); err == nil {
		err = resumeErr
	}
	a.screen.Sync()
	a.refresh()
	if err != nil {
		a.message = "Editor failed: " + err.Error()
	}
}

func (a *app) openExternal() {
	e := a.current()
	if e == nil {
		return
	}
	cmd := exec.Command(opener, filepath.Join(a.cwd, e.name))
	if err := cmd.Start(); err != nil {
		a.message = "Open failed: " + err.Error()
		return
	}
	go cmd.Wait()
	a.message = "Opened " + e.name
}

func (a *app) copyPath() {
	e := a.current()
	if e == nil {
		return
	}
	path := filepath.Join(a.cwd, e.name)
	a.copyPrefix = path
	a.copyToClipboard([]byte(path))
	a.message = "Copied path " + path
}

func (a *app) copyContent(path string) {
	info, err := os.Stat(path)
	if err == nil && !info.Mode().IsRegular() {
		err = fmt.Errorf("not a regular file")
	} else if err == nil && info.Size() > copyLimit {
		err = fmt.Errorf("file larger than %d MiB", copyLimit>>20)
	}
	var data []byte
	if err == nil {
		data, err = os.ReadFile(path)
	}
	if err != nil {
		a.message = "Copy failed: " + err.Error()
		return
	}
	a.copyToClipboard(data)
	a.message = fmt.Sprintf("Copied %d B from %s", len(data), filepath.Base(path))
}

func (a *app) copyToClipboard(data []byte) {
	for _, c := range clipboardCommands {
		if _, err := exec.LookPath(c[0]); err != nil {
			continue
		}
		cmd := exec.Command(c[0], c[1:]...)
		cmd.Stdin = bytes.NewReader(data)
		if cmd.Run() == nil {
			return
		}
	}
	a.screen.SetClipboard(data)
}

func (a *app) requestDelete() {
	if e := a.current(); e != nil {
		copy := *e
		a.pending = &copy
	}
}

func (a *app) remove(target entry) {
	if err := deleteEntry(a.cwd, target); err != nil {
		a.message = "Delete failed: " + err.Error()
	} else {
		a.message = "Deleted " + target.name
	}
	if err := a.reload(""); err != nil {
		a.message = err.Error()
	}
}

func entryStyle(e entry) tcell.Style {
	if e.info.Mode()&os.ModeSymlink != 0 {
		return normal.Foreground(tcell.ColorTurquoise)
	}
	if e.dir {
		return accent
	}
	if e.info.Mode()&0111 != 0 {
		return normal.Foreground(tcell.ColorLightGreen)
	}
	return normal
}

func (a *app) text(x, y, width int, text string, style tcell.Style) {
	a.spans(x, y, width, 0, []span{{safeText(text), style}})
}

func (a *app) spans(x, y, width, skip int, spans []span) {
	pos := 0
	for _, span := range spans {
		g := uniseg.NewGraphemes(span.text)
		for g.Next() {
			w := g.Width()
			if pos >= skip && pos+w <= skip+width && w > 0 {
				a.screen.Put(x+pos-skip, y, g.Str(), span.style)
			}
			pos += w
			if pos >= skip+width {
				return
			}
		}
	}
}

func (a *app) list(x, width, height int, entries []entry, selected int, active bool) {
	start := max(0, selected-height/2)
	start = min(start, max(0, len(entries)-height))
	if len(entries) == 0 {
		a.text(x+1, 2, width-2, "(empty)", muted)
	}
	for row, i := 0, start; row < height && i < len(entries); row, i = row+1, i+1 {
		e := entries[i]
		style := entryStyle(e)
		if i == selected {
			style = style.Background(tcell.ColorDarkSlateGray)
			if active {
				style = selectedStyle
			}
			for c := 0; c < width; c++ {
				a.screen.Put(x+c, row+2, " ", style)
			}
		}
		a.text(x+1, row+2, width-2, e.label(), style)
	}
}

func (a *app) drawPreview(x, width, height int) {
	for row := 0; row < height && row+a.previewOffset < len(a.preview.lines); row++ {
		n := row + a.previewOffset
		gutter := min(7, width/4)
		a.text(x, row+2, gutter, fmt.Sprintf("%*d ", gutter-1, n+1), muted)
		a.spans(x+gutter, row+2, width-gutter-1, a.horizontal, a.preview.lines[n])
	}
	if len(a.preview.lines) == 0 {
		a.text(x+1, 2, width-2, a.preview.note, muted)
	}
}

func (a *app) draw() {
	s := a.screen
	s.Clear()
	s.HideCursor()
	w, h := s.Size()
	if w < 40 || h < 8 {
		a.text(0, 0, w, "Resize terminal to at least 40 x 8", accent)
		s.Show()
		return
	}
	a.text(1, 0, w-2, "fm  "+a.cwd, accent)
	height := h - 5
	if a.help {
		lines := []string{"Keys", "j/k or ↑/↓       Select file", "h/←              Parent directory", "l/→/Enter        Enter directory / read-only viewer", "g/G, Home/End    First / last entry", "PgUp/PgDn        Page through entries or viewer", "J/K, Ctrl-D/U    Scroll preview", "Viewer h/l, ←/→  Scroll horizontally", ".                Toggle hidden files", "~                Home directory", "r / Ctrl-L       Refresh", "dd               Delete immediately (Delete key confirms)", "c / cc           Copy path / file content", "e                Edit in $VISUAL / $EDITOR", "o                Open with system app", "q / Esc          Close viewer (q quits browser)", "?                Help; any key returns"}
		for i, line := range lines {
			if i+2 < h-2 {
				a.text(2, i+2, w-4, line, normal)
			}
		}
	} else if a.viewer {
		a.text(1, 1, w-2, "PREVIEW  "+filepath.Base(a.previewPath), accent)
		a.drawPreview(0, w, height)
	} else {
		parentWidth, currentWidth := w/5, w*3/10
		if w < 80 {
			parentWidth, currentWidth = 0, w*2/5
		}
		if parentWidth > 0 {
			a.text(1, 1, parentWidth-2, "PARENT", muted)
			index := -1
			for i, e := range a.parent {
				if e.name == filepath.Base(a.cwd) {
					index = i
				}
			}
			a.list(0, parentWidth-1, height, a.parent, index, false)
		}
		a.text(parentWidth+1, 1, currentWidth-2, "FILES", accent)
		a.list(parentWidth, currentWidth-1, height, a.entries, a.selected, true)
		x := parentWidth + currentWidth
		a.text(x+1, 1, w-x-2, "PREVIEW · read only", muted)
		a.drawPreview(x, w-x, height)
		for _, col := range []int{parentWidth - 1, x - 1} {
			if col >= 0 {
				for y := 1; y < h-3; y++ {
					s.Put(col, y, "│", muted)
				}
			}
		}
	}
	status := a.preview.note
	if e := a.current(); e != nil {
		status = fmt.Sprintf("%s  %d B  %s  ·  %s", e.info.Mode(), e.info.Size(), e.info.ModTime().Format("2006-01-02 15:04"), status)
	}
	a.text(1, h-3, w-2, status, muted)
	if a.pending != nil {
		// %q makes control characters and embedded newlines unambiguous.
		prompt := fmt.Sprintf("Permanently delete %q", a.pending.name)
		if a.pending.info.IsDir() {
			prompt += " and ALL contents"
		}
		a.text(1, h-2, w-2, prompt, normal.Foreground(tcell.ColorOrange))
		a.text(1, h-1, w-2, "y: confirm deletion · any other key: cancel", normal.Foreground(tcell.ColorOrange))
	} else {
		a.text(1, h-2, w-2, a.message, normal.Foreground(tcell.ColorOrange))
		keys := "hjkl/↑↓←→ navigate · Enter preview · e edit · o open · dd delete · c/cc copy · . hidden · ? help · q quit"
		if a.viewer {
			keys = "READ ONLY · j/k scroll · h/l pan · e edit · o open · c/cc copy · q/Esc back"
		}
		if a.hidden {
			keys = strings.ReplaceAll(keys, ". hidden", ". hide dotfiles")
		}
		a.text(1, h-1, w-2, keys, muted)
	}
	s.Show()
}
