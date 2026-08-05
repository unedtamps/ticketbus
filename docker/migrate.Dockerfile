FROM golang:1.26-alpine AS builder
RUN go install -ldflags="-s -w" \
    github.com/golang-migrate/migrate/v4/cmd/migrate@v4.18.2

FROM alpine:3.20
RUN apk add --no-cache ca-certificates
COPY --from=builder /go/bin/migrate /usr/local/bin/migrate
COPY migrations/auth /migrations/auth
COPY migrations/ticketing /migrations/ticketing
COPY migrations/payment /migrations/payment
RUN echo '#!/bin/sh' > /usr/local/bin/migrate-run.sh && \
    echo 'SERVICE=$1' >> /usr/local/bin/migrate-run.sh && \
    echo 'if [ -z "$SERVICE" ]; then' >> /usr/local/bin/migrate-run.sh && \
    echo '  echo "Usage: $0 <service> <database_url> <command>"' >> /usr/local/bin/migrate-run.sh && \
     echo '  echo "Service: auth, ticketing, payment"' >> /usr/local/bin/migrate-run.sh && \
    echo '  exit 1' >> /usr/local/bin/migrate-run.sh && \
    echo 'fi' >> /usr/local/bin/migrate-run.sh && \
    echo 'shift' >> /usr/local/bin/migrate-run.sh && \
    echo 'exec migrate -path "/migrations/$SERVICE" -database "$@"' >> /usr/local/bin/migrate-run.sh && \
    chmod +x /usr/local/bin/migrate-run.sh
ENTRYPOINT ["/usr/local/bin/migrate-run.sh"]
