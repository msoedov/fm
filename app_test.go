package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestReadDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "text")
	writeFile(t, filepath.Join(dir, ".hidden"), "secret")
	if err := os.Mkdir(filepath.Join(dir, "z-dir"), 0700); err != nil {
		t.Fatal(err)
	}
	entries, err := readDir(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 || entries[0].name != "z-dir" || entries[1].name != "a.txt" {
		t.Fatalf("unexpected listing: %+v", entries)
	}
	entries, err = readDir(dir, true)
	if err != nil || len(entries) != 3 {
		t.Fatalf("hidden listing: %v, %v", entries, err)
	}
}

func TestDeletion(t *testing.T) {
	for _, kind := range []string{"file", "directory", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, "target")
			outside := t.TempDir()
			writeFile(t, filepath.Join(outside, "keep"), "safe")
			switch kind {
			case "file":
				writeFile(t, target, "delete")
			case "directory":
				if err := os.Mkdir(target, 0700); err != nil {
					t.Fatal(err)
				}
				writeFile(t, filepath.Join(target, "child"), "delete")
			case "symlink":
				if err := os.Symlink(outside, target); err != nil {
					t.Fatal(err)
				}
			}
			entries, err := readDir(dir, true)
			if err != nil {
				t.Fatal(err)
			}
			if err := deleteEntry(dir, entries[0]); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(target); !os.IsNotExist(err) {
				t.Fatalf("target still exists: %v", err)
			}
			if _, err := os.Stat(filepath.Join(outside, "keep")); err != nil {
				t.Fatalf("symlink target damaged: %v", err)
			}
		})
	}
}

func TestDeleteRejectsReplacementAndTraversal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	writeFile(t, path, "old")
	entries, err := readDir(dir, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path, filepath.Join(dir, "old")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, "replacement")
	if err := deleteEntry(dir, entries[0]); err == nil {
		t.Fatal("accepted replacement")
	}
	for _, name := range []string{"", ".", "..", "../outside", "/tmp"} {
		if err := deleteEntry(dir, entry{name: name}); err == nil {
			t.Fatalf("accepted %q", name)
		}
	}
}

func TestPreview(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	writeFile(t, path, "package main\n\nfunc main() { println(\"hello\") }\n")
	p := loadPreview(path, false)
	if len(p.lines) < 3 {
		t.Fatalf("missing lines: %+v", p)
	}
	highlighted := false
	for _, line := range p.lines {
		for _, s := range line {
			if s.style != normal {
				highlighted = true
			}
		}
	}
	if !highlighted {
		t.Fatal("no syntax highlighting")
	}
	writeFile(t, path, "\x00\x01binary")
	if p := loadPreview(path, false); !strings.Contains(p.note, "Binary") || len(p.lines) != 0 {
		t.Fatalf("binary preview: %+v", p)
	}
	writeFile(t, path, strings.Repeat("plain text\n", previewLimit/5))
	if p := loadPreview(path, false); !strings.Contains(p.note, "limited") {
		t.Fatal("missing truncation note")
	}
	if got := safeText("abc\x1b\n\u202e"); got != "abc���" {
		t.Fatalf("unsafe display: %q", got)
	}
}

func TestNavigationViewerAndConfirmation(t *testing.T) {
	dir := t.TempDir()
	child := filepath.Join(dir, "child")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(child, "file.txt")
	writeFile(t, file, "hello\nworld")
	s := tcell.NewSimulationScreen("UTF-8")
	if err := s.Init(); err != nil {
		t.Fatal(err)
	}
	defer s.Fini()
	s.SetSize(100, 24)
	a := &app{screen: s, cwd: dir}
	if err := a.reload(""); err != nil {
		t.Fatal(err)
	}
	key := func(r rune) { a.key(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)); a.draw() }
	key('l')
	if a.cwd != child {
		t.Fatal("did not enter directory")
	}
	key('l')
	if !a.viewer {
		t.Fatal("did not open viewer")
	}
	key('d')
	if a.pending != nil || a.deletePrefix != nil {
		t.Fatal("viewer allows deletion")
	}
	key('q')
	if a.viewer {
		t.Fatal("viewer did not close")
	}
	key('d')
	key('n')
	if _, err := os.Stat(file); err != nil {
		t.Fatal("cancel deleted file")
	}
	key('d')
	if _, err := os.Stat(file); err != nil {
		t.Fatal("single d deleted file")
	}
	if a.pending != nil {
		t.Fatal("d opened confirmation")
	}
	key('d')
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("dd deletion failed")
	}
	writeFile(t, file, "again")
	a.refresh()
	a.key(tcell.NewEventKey(tcell.KeyDelete, 0, tcell.ModNone))
	if a.pending == nil {
		t.Fatal("Delete key should request confirmation")
	}
	key('y')
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("confirmed deletion failed")
	}
	key('h')
	if a.cwd != dir || a.current().name != "child" {
		t.Fatal("parent selection not restored")
	}
	for _, size := range [][2]int{{40, 8}, {60, 15}, {120, 35}, {15, 3}} {
		s.SetSize(size[0], size[1])
		a.draw()
	}
}
