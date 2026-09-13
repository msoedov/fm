GO ?= go
PREFIX ?= $(HOME)/.local
BINDIR ?= $(PREFIX)/bin

.PHONY: b t i

b:
	$(GO) build -o fm .

t:
	$(GO) test ./...
	$(GO) vet ./...

i: b
	install -d "$(BINDIR)"
	install -m 755 fm "$(BINDIR)/fm"
