FROM golang:1.27.3-alpine AS build
WORKDIR /src
COPY gateway/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /gateway ./cmd/gateway
FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /gateway /usr/local/bin/remote-console-gateway
ENTRYPOINT ["/usr/local/bin/remote-console-gateway"]
