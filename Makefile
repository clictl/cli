VERSION ?= dev
LDFLAGS = -ldflags "-X github.com/clictl/cli/internal/command.Version=$(VERSION)"

build:
	go build $(LDFLAGS) -o clictl ./cmd/clictl

test:
	go test ./...

clean:
	rm -f clictl

e2e:
	go test ./e2e/ -v -count=1

e2e-network:
	go test -tags e2e_network ./e2e/ -v -count=1

e2e-all:
	go test -tags "e2e_network e2e_auth" ./e2e/ -v -count=1

.PHONY: build test clean e2e e2e-network e2e-all
