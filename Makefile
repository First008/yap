.PHONY: build test clean run test-tts test-stt

BINARY=yap
BUILD_DIR=.

build:
	go build -o $(BUILD_DIR)/$(BINARY) ./cmd/yap

test:
	go test ./...

clean:
	rm -f $(BUILD_DIR)/$(BINARY)
	rm -f .yap-state.json .yap.sock

run: build
	./$(BINARY)

test-tts: build
	./$(BINARY) --test-tts "Hello, this is a test of the text to speech system."

test-stt: build
	./$(BINARY) --test-ptt

fmt:
	go fmt ./...

lint:
	go vet ./...
