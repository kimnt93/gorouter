FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/gorouter ./cmd/gorouter

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/gorouter /usr/local/bin/gorouter
RUN mkdir -p /var/lib/gorouter && chown 65532:65532 /var/lib/gorouter
EXPOSE 8090
USER 65532:65532
ENTRYPOINT ["/usr/local/bin/gorouter"]
