.PHONY: build clean test
.DEFAULT_GOAL := build
GOBIN ?= $(shell go env GOPATH)/bin

clean:
	go clean
	rm -f ${GOBIN}/sexton sexton

build:
	go install ./...

test:
	go test ./... -count=1
	go vet ./...

push:
	push vendor ${GOBIN}/sexton sexton
