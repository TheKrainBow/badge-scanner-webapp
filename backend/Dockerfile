FROM golang:1.25-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Pure Go (modernc.org/sqlite has no cgo dependency, unlike the reader-agent's
# PC/SC binding) — CGO_ENABLED=0 gives a static binary for a minimal final image.
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/server ./cmd/server

FROM alpine:3.20
# ca-certificates: needed for the outbound TLS calls to the 42 API (the CA's
# self-signed cert is pinned separately, embedded in the binary).
RUN apk add --no-cache ca-certificates && \
    adduser -D -u 10001 appuser && \
    mkdir -p /data && chown appuser:appuser /data
COPY --from=builder /out/server /usr/local/bin/server
USER appuser
VOLUME /data
ENV DB_PATH=/data/badgescanner.db
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]
