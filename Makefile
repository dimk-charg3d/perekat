GUI_TAGS := desktop,production
# VERSION is stamped into every binary; `make dist VERSION=1.2.3` makes a release.
VERSION ?= dev
STAMP := -X github.com/charg3d/perekat/internal/version.Version=$(VERSION)
DIST := dist/perekat-$(VERSION)

PREFIX ?= $(HOME)/.local
BINDIR := $(PREFIX)/bin
APPDIR := $(PREFIX)/share/applications
ICONDIR := $(PREFIX)/share/icons/hicolor/scalable/apps

RELAY_DIR := internal/deploy/bin

.PHONY: all cli gui relay windows android test clean install uninstall dist downloads

all: cli gui

# The relay server is embedded into perekat, which installs it on a remote
# machine, so it is built first, for the two common server architectures.
relay:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags "-s -w" -o $(RELAY_DIR)/perekat-relay-linux-amd64 ./cmd/perekat-relay
	CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags "-s -w" -o $(RELAY_DIR)/perekat-relay-linux-arm64 ./cmd/perekat-relay

cli: relay
	go build -ldflags "$(STAMP)" -o bin/perekat ./cmd/perekat

gui: relay
	go build -tags $(GUI_TAGS),webkit2_41 -ldflags "$(STAMP)" -o bin/perekat-gui ./cmd/perekat-gui

windows: relay
	GOOS=windows GOARCH=amd64 go build -ldflags "$(STAMP)" -o bin/perekat.exe ./cmd/perekat
	GOOS=windows GOARCH=amd64 go build -tags $(GUI_TAGS) -ldflags "-H windowsgui $(STAMP)" -o bin/perekat-gui.exe ./cmd/perekat-gui

# The Android application; needs the Android SDK (build-tools and a platform).
android:
	VERSION=$(VERSION) android/build.sh

test: relay
	go vet ./internal/... ./cmd/perekat
	go test ./...

clean:
	rm -rf bin dist

# dist builds everything and packs what a release consists of.
dist: test all windows android
	rm -rf dist && mkdir -p $(DIST)-linux-amd64 $(DIST)-windows-amd64
	cp bin/perekat bin/perekat-gui packaging/perekat.desktop packaging/perekat.svg packaging/install.sh LICENSE README.md THIRD_PARTY.md $(DIST)-linux-amd64/
	cp bin/perekat.exe bin/perekat-gui.exe LICENSE README.md THIRD_PARTY.md $(DIST)-windows-amd64/
	tar -C dist -czf $(DIST)-linux-amd64.tar.gz perekat-$(VERSION)-linux-amd64
	cd dist && python3 -m zipfile -c perekat-$(VERSION)-windows-amd64.zip perekat-$(VERSION)-windows-amd64
	cp bin/perekat.apk $(DIST)-android-arm64.apk
	cp $(RELAY_DIR)/perekat-relay-linux-amd64 dist/perekat-relay-$(VERSION)-linux-amd64
	cp $(RELAY_DIR)/perekat-relay-linux-arm64 dist/perekat-relay-$(VERSION)-linux-arm64
	rm -rf $(DIST)-linux-amd64 $(DIST)-windows-amd64
	cd dist && sha256sum * > SHA256SUMS

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

# downloads refreshes the ready-made builds kept in the repository, for
# getting perekat onto a new machine straight from GitHub.
downloads: all windows android
	rm -rf downloads/.stage && mkdir -p downloads/.stage/perekat-linux downloads/.stage/perekat-windows
	cp bin/perekat bin/perekat-gui packaging/perekat.desktop packaging/perekat.svg packaging/install.sh LICENSE downloads/.stage/perekat-linux/
	cp bin/perekat.exe bin/perekat-gui.exe LICENSE downloads/.stage/perekat-windows/
	tar -C downloads/.stage -czf downloads/perekat-linux.tar.gz perekat-linux
	cd downloads/.stage && python3 -m zipfile -c ../perekat-windows.zip perekat-windows
	cp bin/perekat.apk downloads/perekat-android.apk
	rm -rf downloads/.stage
