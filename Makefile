GUI_TAGS := desktop,production

PREFIX ?= $(HOME)/.local
BINDIR := $(PREFIX)/bin
APPDIR := $(PREFIX)/share/applications
ICONDIR := $(PREFIX)/share/icons/hicolor/scalable/apps

RELAY_DIR := internal/deploy/bin

.PHONY: all cli gui relay windows android test clean install uninstall

all: cli gui

# The relay server is embedded into perekat, which installs it on a remote
# machine, so it is built first, for the two common server architectures.
relay:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o $(RELAY_DIR)/perekat-relay-linux-amd64 ./cmd/perekat-relay
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o $(RELAY_DIR)/perekat-relay-linux-arm64 ./cmd/perekat-relay

cli: relay
	go build -o bin/perekat ./cmd/perekat

gui: relay
	go build -tags $(GUI_TAGS),webkit2_41 -o bin/perekat-gui ./cmd/perekat-gui

windows: relay
	GOOS=windows GOARCH=amd64 go build -o bin/perekat.exe ./cmd/perekat
	GOOS=windows GOARCH=amd64 go build -tags $(GUI_TAGS) -ldflags "-H windowsgui" -o bin/perekat-gui.exe ./cmd/perekat-gui

# The Android application; needs the Android SDK (build-tools and a platform).
android:
	android/build.sh

test: relay
	go vet ./internal/... ./cmd/perekat
	go test ./...

clean:
	rm -rf bin

# install puts perekat into the user's application menu.
install: all
	install -Dm755 bin/perekat $(BINDIR)/perekat
	install -Dm755 bin/perekat-gui $(BINDIR)/perekat-gui
	install -Dm644 packaging/perekat.svg $(ICONDIR)/perekat.svg
	install -d $(APPDIR)
	sed 's|@BINDIR@|$(BINDIR)|' packaging/perekat.desktop > $(APPDIR)/perekat.desktop
	-update-desktop-database $(APPDIR) 2>/dev/null

uninstall:
	rm -f $(BINDIR)/perekat $(BINDIR)/perekat-gui $(ICONDIR)/perekat.svg $(APPDIR)/perekat.desktop
