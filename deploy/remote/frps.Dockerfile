FROM golang:1.27.3-alpine AS build
RUN apk add --no-cache git
ARG FRP_VERSION=v0.72.0
RUN git clone --depth 1 --branch ${FRP_VERSION} https://github.com/fatedier/frp.git /src
WORKDIR /src
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -tags 'frps,noweb' -o /frps ./cmd/frps
FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /frps /usr/local/bin/frps
ENTRYPOINT ["frps", "-c", "/etc/frp/frps.toml"]
