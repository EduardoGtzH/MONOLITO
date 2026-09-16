FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o loadbalancer .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/loadbalancer .

EXPOSE 8000
CMD ["./loadbalancer"]
