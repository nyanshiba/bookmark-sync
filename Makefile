.PHONY: all build install clean

# ビルドターゲット
BINDIR := $(HOME)/bin
GOFLAGS := -ldflags="-s -w"

all: build

build:
	cd cmd/import      && go build $(GOFLAGS) -o ../../build/bookmark-sync-import .
	cd cmd/restore-tabs && go build $(GOFLAGS) -o ../../build/bookmark-sync-restore-tabs .
	cd cmd/sync        && go build $(GOFLAGS) -o ../../build/bookmark-sync-sync .
	cd cmd/expand-titles && go build $(GOFLAGS) -o ../../build/bookmark-sync-expand-titles .
	cd cmd/classify-tags && go build $(GOFLAGS) -o ../../build/bookmark-sync-classify-tags .
	@echo "built: build/bookmark-sync-import, build/bookmark-sync-restore-tabs, build/bookmark-sync-sync, build/bookmark-sync-expand-titles, build/bookmark-sync-classify-tags"

install: build
	install -m 755 build/bookmark-sync-import      $(BINDIR)/
	install -m 755 build/bookmark-sync-restore-tabs $(BINDIR)/
	install -m 755 build/bookmark-sync-sync        $(BINDIR)/
	install -m 755 build/bookmark-sync-expand-titles $(BINDIR)/
	install -m 755 build/bookmark-sync-classify-tags $(BINDIR)/
	@echo "installed to $(BINDIR)/"

clean:
	rm -rf build