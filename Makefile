GOPACKAGES=$(shell glide novendor | grep -v enginetest)
GOFILES=$(shell find . -type f -name '*.go' -not -path "./vendor/*")

.PHONY: all
all: deps fmt vet test

.PHONY: deps
deps:
	glide install
	go get github.com/mattn/goveralls

.PHONY: fmt
fmt:
	@if [ -n "$$(gofmt -l ${GOFILES})" ]; then echo 'The following files have errors. Please run gofmt -l -w on your code.' && gofmt -l ${GOFILES} && exit 1; fi

.PHONY: test
test:
	echo 'mode: atomic' > cover.out && glide novendor | grep -v enginetest | xargs -n1 -I{} sh -c 'go test -v -race -covermode=atomic -coverprofile=coverage.tmp {} && tail -n +2 coverage.tmp >> cover.out' && rm coverage.tmp
	go run v3enginetest/main.go

.PHONY: vet
vet:
	go vet ${GOPACKAGES}

.PHONY: dofmt
dofmt:
	gofmt -l -s -w ${GOFILES}