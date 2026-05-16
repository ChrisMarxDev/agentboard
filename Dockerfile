# syntax=docker/dockerfile:1.7

# ---- Stage 1: build the Go binary ----
# Pure Go all the way down — no Node, no React, no MDX compiler. The
# dashboard ships as a server-rendered HTML surface over a git workspace
# (see internal/html/ and internal/gitserver/), so there's nothing to
# compile outside Go itself.
FROM golang:1.25-alpine AS gobuild
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# modernc.org/sqlite is pure Go → CGO stays off → static binary.
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -ldflags="-s -w" -o /out/agentboard ./cmd/agentboard

# ---- Stage 2: minimal runtime ----
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=gobuild /out/agentboard /agentboard
EXPOSE 3000
ENTRYPOINT ["/agentboard", "serve", "--no-open", "--port", "3000"]
