FROM golang:1.26-alpine AS builder
RUN go install -ldflags="-s -w" \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.2

FROM alpine:3.20
ARG SERVICE
RUN apk add --no-cache ca-certificates
COPY --from=builder /go/bin/migrate /usr/local/bin/migrate
COPY migrations/$SERVICE /migrations
ENTRYPOINT ["migrate", "-path", "/migrations", "-database"]
