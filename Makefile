BINARY_NAME=rclone
GO=go

.PHONY: all build test lint clean docker

all: build

build:
	$(GO) build -o bin/$(BINARY_NAME) .

test:
	$(GO) test ./... -v

lint:
	golangci-lint run ./...

clean:
	rm -rf bin

docker: build
	docker build -t yourname/$(BINARY_NAME):latest .
