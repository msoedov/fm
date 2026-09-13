package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/gdamore/tcell/v2"
)

const previewLimit = 256 * 1024

type span struct {
	text  string
	style tcell.Style
}
type preview struct {
	lines [][]span
	note  string
}

func safeText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return '�'
		}
		return r
	}, s)
}

func tokenStyle(t chroma.TokenType) tcell.Style {
	switch {
	case t.InCategory(chroma.Comment):
		return normal.Foreground(tcell.ColorGray)
	case t.InCategory(chroma.Keyword):
		return normal.Foreground(tcell.ColorMediumPurple)
	case t.InSubCategory(chroma.LiteralString):
		return normal.Foreground(tcell.ColorLightGreen)
	case t.InSubCategory(chroma.LiteralNumber):
		return normal.Foreground(tcell.ColorOrange)
	case t.InSubCategory(chroma.NameFunction):
		return normal.Foreground(tcell.ColorLightSkyBlue)
	default:
		return normal
	}
}

func loadPreview(path string, hidden bool) preview {
	info, err := os.Stat(path)
	if err != nil {
		return preview{note: err.Error()}
	}
	if info.IsDir() {
		entries, err := readDir(path, hidden)
		if err != nil {
			return preview{note: err.Error()}
		}
		p := preview{note: fmt.Sprintf("%d entries", len(entries))}
		for _, e := range entries {
			p.lines = append(p.lines, []span{{safeText(e.label()), entryStyle(e)}})
		}
		return p
	}
	if !info.Mode().IsRegular() {
		return preview{note: "Special file — no preview"}
	}
	f, err := os.Open(path)
	if err != nil {
		return preview{note: err.Error()}
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, previewLimit+1))
	if err != nil {
		return preview{note: err.Error()}
	}
	p := preview{note: "Read only"}
	if len(data) > previewLimit {
		data = data[:previewLimit]
		// A byte limit may fall in the middle of a UTF-8 character.
		for len(data) > 0 && !utf8.Valid(data) && len(data) > previewLimit-4 {
			data = data[:len(data)-1]
		}
		p.note = "Read only · preview limited to 256 KiB"
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return preview{note: fmt.Sprintf("Binary file · %d bytes", info.Size())}
	}
	source := strings.ReplaceAll(string(data), "\r\n", "\n")
	lexer := lexers.Match(path)
	if lexer == nil {
		lexer = lexers.Fallback
	}
	iterator, err := chroma.Coalesce(lexer).Tokenise(nil, source)
	if err != nil {
		iterator = chroma.Literator(chroma.Token{Type: chroma.Text, Value: source})
	}
	p.lines = [][]span{{}}
	for token := iterator(); token != chroma.EOF; token = iterator() {
		parts := strings.Split(token.Value, "\n")
		for i, part := range parts {
			if i > 0 {
				p.lines = append(p.lines, []span{})
			}
			part = safeText(strings.ReplaceAll(part, "\t", "    "))
			p.lines[len(p.lines)-1] = append(p.lines[len(p.lines)-1], span{part, tokenStyle(token.Type)})
		}
	}
	return p
}
