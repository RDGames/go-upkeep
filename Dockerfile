FROM golang:alpine AS builder

RUN apk add --no-cache gcc musl-dev

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ENV CGO_ENABLED=1
RUN go build -ldflags="-s -w" -o go-upkeep ./cmd/goupkeep/main.go

FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache ca-certificates openssh-client

RUN mkdir /data

COPY --from=builder /app/go-upkeep .

EXPOSE 23234

CMD ["./go-upkeep", "--db", "/data/upkeep.db", "--keys", "/data/authorized_keys"]