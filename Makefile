.PHONY: build test fmt vet clean install

BIN := pluckr
PKG := ./cmd/pluckr

build:
	go build -o bin/$(BIN) $(PKG)

install:
	go install $(PKG)

test:
	go test ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -rf bin dist
