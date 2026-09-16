# Stage 1: build the static binary
FROM golang:1.22-alpine AS builder
WORKDIR /app

COPY go.mod ./
COPY . .
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux go build -o servicio .

# Stage 2: lightweight runtime image
FROM alpine:3.19
WORKDIR /app
COPY --from=builder /app/servicio .

EXPOSE 8080
CMD ["./servicio"]
