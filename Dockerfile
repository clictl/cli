FROM golang:1.23-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -ldflags "-X github.com/clictl/cli/internal/command.Version=docker" -o /clictl ./cmd/clictl

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /clictl /usr/local/bin/clictl
ENTRYPOINT ["clictl"]
