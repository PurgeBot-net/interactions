FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -o /interactions ./cmd/

FROM alpine:latest
RUN adduser -D -g '' container
USER container
COPY --from=builder /interactions /interactions
ENTRYPOINT ["/interactions"]
