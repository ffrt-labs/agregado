# syntax=docker/dockerfile:1

# CMD picks the binary under cmd/ to build: agregado (the default) or
# bridge-admin (the Newsletters app). docker-compose.yml passes it per service.
ARG CMD=agregado

FROM golang:1.25 AS builder
ARG CMD

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . ./
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/app ./cmd/${CMD}

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y ca-certificates && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /bin/app .
CMD ["/app/app"]
EXPOSE 8080
