.PHONY: build linux test fmt vet

build:
	go build -o bin/cloudphoto .

linux:
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -o bin/linux-amd64/cloudphoto .

test:
	go test -race ./...

fmt:
	gofmt -w *.go

vet:
	go vet ./...
