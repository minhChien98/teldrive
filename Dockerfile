# Build stage
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /app

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Download UI assets
RUN go run scripts/extract.go -url https://github.com/tgdrive/teldrive-ui/releases/download/latest/teldrive-ui.zip -output ui/dist

# Generate API code
RUN go generate ./...

# Build the binary
ARG VERSION=dev
ARG COMMIT=unknown
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
    -ldflags="-s -w -X github.com/tgdrive/teldrive/internal/version.Version=${VERSION} -X github.com/tgdrive/teldrive/internal/version.CommitSHA=${COMMIT}" \
    -o /teldrive .

# Final stage
FROM scratch

COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /teldrive /teldrive

EXPOSE 8080

ENTRYPOINT ["/teldrive", "run"]
