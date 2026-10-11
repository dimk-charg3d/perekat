GUI_TAGS := desktop,production

.PHONY: all cli gui windows test clean

all: cli gui

cli:
	go build -o bin/perekat ./cmd/perekat

gui:
	go build -tags $(GUI_TAGS),webkit2_41 -o bin/perekat-gui ./cmd/perekat-gui

windows:
	GOOS=windows GOARCH=amd64 go build -o bin/perekat.exe ./cmd/perekat
	GOOS=windows GOARCH=amd64 go build -tags $(GUI_TAGS) -ldflags "-H windowsgui" -o bin/perekat-gui.exe ./cmd/perekat-gui

test:
	go vet ./internal/... ./cmd/perekat
	go test ./...

clean:
	rm -rf bin
