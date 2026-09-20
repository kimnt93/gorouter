FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GOROUTER_VERSION=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/kimnt93/gorouter/pkg/updatecheck.Version=${GOROUTER_VERSION}" -o /out/gorouter ./cmd/gorouter

FROM alpine:3.22 AS runtime
RUN apk add --no-cache ca-certificates
COPY --from=build /out/gorouter /usr/local/bin/gorouter
RUN mkdir -p /var/lib/gorouter && chown 65532:65532 /var/lib/gorouter
WORKDIR /var/lib/gorouter
EXPOSE 8090
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/gorouter"]

# Optional, reproducible image with the external CLI. Default target below
# stays lightweight. Both targets run the identical non-root GoRouter binary.
FROM alpine:3.22 AS devin-download
ARG TARGETARCH
RUN apk add --no-cache ca-certificates curl
COPY scripts/install-devin-cli.sh /install-devin-cli.sh
RUN sh /install-devin-cli.sh "$TARGETARCH" /out

FROM runtime AS devin-cli
COPY --from=devin-download /out/devin /usr/local/bin/devin
RUN devin --version

FROM runtime AS default
