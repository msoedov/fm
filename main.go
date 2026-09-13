package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gdamore/tcell/v2"
)

func run() error {
	flag.Usage = func() {
		fmt.Fprintln(flag.CommandLine.Output(), "Usage: fm [directory]\nA read-only terminal file browser. Press ? for keys.")
	}
	flag.Parse()
	if flag.NArg() > 1 {
		flag.Usage()
		return fmt.Errorf("expected at most one directory")
	}
	path := "."
	if flag.NArg() == 1 {
		path = flag.Arg(0)
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	a := &app{cwd: path}
	if err := a.reload(""); err != nil {
		return err
	}
	s, err := tcell.NewScreen()
	if err != nil {
		return err
	}
	if err := s.Init(); err != nil {
		return err
	}
	defer s.Fini()
	a.screen = s
	s.SetStyle(normal)
	for {
		a.draw()
		switch ev := s.PollEvent().(type) {
		case *tcell.EventResize:
			s.Sync()
		case *tcell.EventKey:
			if a.key(ev) {
				return nil
			}
		case nil:
			return nil
		}
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fm:", err)
		os.Exit(1)
	}
}
