VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BINARY_NAME = gleand
BUILD_DIR = dist

LDFLAGS = -ldflags "-s -w -X main.version=$(VERSION)"

.PHONY: build clean test cross-build

build:
	go build $(LDFLAGS) -o $(BINARY_NAME) ./cmd/gleand/

test:
	go test ./...

clean:
	rm -rf $(BUILD_DIR) $(BINARY_NAME)

cross-build: cross-build-darwin cross-build-linux cross-build-windows

cross-build-darwin:
	GOOS=darwin GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-arm64 ./cmd/gleand/
	GOOS=darwin GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-darwin-amd64 ./cmd/gleand/

cross-build-linux:
	GOOS=linux GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-amd64 ./cmd/gleand/
	GOOS=linux GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-linux-arm64 ./cmd/gleand/

cross-build-windows:
	GOOS=windows GOARCH=amd64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-amd64.exe ./cmd/gleand/
	GOOS=windows GOARCH=arm64 go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-windows-arm64.exe ./cmd/gleand/

install: build
	cp $(BINARY_NAME) /usr/local/bin/$(BINARY_NAME)
