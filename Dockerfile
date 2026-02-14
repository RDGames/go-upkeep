# --- Stage 1: Builder ---
FROM golang:alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ENV CGO_ENABLED=1
RUN go build -ldflags="-s -w" -o go-upkeep ./cmd/goupkeep/main.go

# --- Stage 2: Runner ---
FROM alpine:latest
WORKDIR /app
RUN apk add --no-cache ca-certificates openssh-client
RUN mkdir /data

COPY --from=builder /app/go-upkeep .

# Set Default Configuration via ENV
# Docker users can override these in docker-compose.yml
ENV LIPGLOSS_RENDERER_HAS_DARK_BACKGROUND=true
ENV UPKEEP_DB_TYPE=sqlite
ENV UPKEEP_DB_DSN=/data/upkeep.db
ENV UPKEEP_KEYS=/data/authorized_keys
ENV UPKEEP_PORT=23234

EXPOSE 23234
CMD ["./go-upkeep"]