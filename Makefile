IMG ?= ghcr.io/example/modeswitch-controller:dev

.PHONY: tools generate build test docker
tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@v1.35.2
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@v1.5.1

generate:
	mkdir -p gen
	protoc -I proto \
	  --go_out=gen --go_opt=paths=source_relative \
	  --go-grpc_out=gen --go-grpc_opt=paths=source_relative \
	  proto/modeswitch/v1/mode.proto

build: generate
	go mod tidy
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/manager ./cmd/controller

test:
	go test ./...

docker:
	docker build -t $(IMG) .
