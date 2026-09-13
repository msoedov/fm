package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type entry struct {
	name string
	info os.FileInfo
	dir  bool
}

func readDir(path string, hidden bool) ([]entry, error) {
	items, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	result := make([]entry, 0, len(items))
	for _, item := range items {
		if !hidden && strings.HasPrefix(item.Name(), ".") {
			continue
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		dir := info.IsDir()
		if info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Stat(filepath.Join(path, item.Name())); err == nil {
				dir = target.IsDir()
			}
		}
		result = append(result, entry{item.Name(), info, dir})
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].dir != result[j].dir {
			return result[i].dir
		}
		a, b := strings.ToLower(result[i].name), strings.ToLower(result[j].name)
		if a == b {
			return result[i].name < result[j].name
		}
		return a < b
	})
	return result, nil
}

// Delete only the selected direct child, and reject a replacement made since
// confirmation was requested. RemoveAll removes symlinks, never their targets.
func deleteEntry(dir string, selected entry) error {
	if selected.name == "." || selected.name == ".." || filepath.Base(selected.name) != selected.name || selected.name == "" {
		return fmt.Errorf("invalid deletion target")
	}
	path := filepath.Join(dir, selected.name)
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(selected.info, info) {
		return fmt.Errorf("entry changed; refresh and try again")
	}
	return os.RemoveAll(path)
}

func (e entry) label() string {
	suffix := ""
	if e.dir {
		suffix = "/"
	}
	if e.info.Mode()&os.ModeSymlink != 0 {
		suffix += " ->"
	}
	return e.name + suffix
}
