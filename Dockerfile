FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG GOROUTER_VERSION=unknown
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/kimnt93/gorouter/pkg/updatecheck.Version=${GOROUTER_VERSION}" -o /out/gorouter ./cmd/gorouter

# Devin is an implementation detail of its provider adapter, bundled into
# the same standard image as every other provider (amd64 and arm64).
FROM alpine:3.22 AS devin-download
ARG TARGETARCH
RUN apk add --no-cache ca-certificates curl
COPY scripts/install-devin-cli.sh /install-devin-cli.sh
RUN sh /install-devin-cli.sh "$TARGETARCH" /out

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/gorouter /usr/local/bin/gorouter
COPY --from=devin-download /out/devin /usr/local/bin/devin
RUN mkdir -p /var/lib/gorouter/provider-runtimes && chown -R 65532:65532 /var/lib/gorouter
WORKDIR /var/lib/gorouter
EXPOSE 8090
USER 65532:65532
# Fail the build if the bundled binary cannot run for this architecture/user.
RUN devin --version
ENTRYPOINT ["/usr/local/bin/gorouter"]
