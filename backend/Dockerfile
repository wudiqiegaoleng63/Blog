# syntax=docker/dockerfile:1.7

FROM golang:1.25.0-alpine3.22 AS builder

WORKDIR /src

RUN apk add --no-cache ca-certificates

ENV GOPROXY=https://goproxy.cn,direct

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/worker ./cmd/worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate

FROM alpine:3.22.1 AS runtime

RUN apk add --no-cache ca-certificates wget \
    && addgroup -S -g 10001 app \
    && adduser -S -D -H -u 10001 -G app app

WORKDIR /app

COPY --from=builder --chown=app:app /out/api /out/worker /out/migrate /app/
COPY --chown=app:app docker-entrypoint.sh /usr/local/bin/docker-entrypoint
RUN chmod 0555 /usr/local/bin/docker-entrypoint /app/api /app/worker /app/migrate

USER 10001:10001

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/docker-entrypoint"]
CMD ["api"]
