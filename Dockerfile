FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o ararauna ./cmd

FROM scratch
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=builder /build/ararauna /ararauna
COPY config.yml /config.yml
WORKDIR /app
VOLUME ["/app/data"]
EXPOSE 6379
ENTRYPOINT ["/ararauna", "--config", "/config.yml"]
