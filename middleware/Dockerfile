FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o middleware .

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/middleware .
COPY routes.json .

EXPOSE 8500
CMD ["./middleware"]
