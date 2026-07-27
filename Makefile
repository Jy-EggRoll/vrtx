VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -ldflags="-s -w -X main.version=$(VERSION)"

.PHONY: default generate clean

default: generate windows

generate:
	go generate ./...

windows: generate
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o vrtx.exe .

linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o vrtx .

run:
	go run .

clean:
	rm -f vrtx vrtx.exe resource.syso
