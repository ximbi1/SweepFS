BINARY_NAME ?= sweepfs
OUTPUT_DIR ?= dist

.PHONY: build clean fmt release

build:
	go build -o $(BINARY_NAME) ./cmd/sweepfs

fmt:
	go fmt ./...

clean:
	rm -rf $(OUTPUT_DIR)

release: clean
	mkdir -p $(OUTPUT_DIR)
	GOOS=linux GOARCH=amd64 go build -o $(OUTPUT_DIR)/$(BINARY_NAME)-linux ./cmd/sweepfs
	GOOS=darwin GOARCH=arm64 go build -o $(OUTPUT_DIR)/$(BINARY_NAME)-macos ./cmd/sweepfs
	GOOS=windows GOARCH=amd64 go build -o $(OUTPUT_DIR)/$(BINARY_NAME).exe ./cmd/sweepfs
