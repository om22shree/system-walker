FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go mod init system-walker && go mod tidy
RUN go build -o system-walker main.go

FROM alpine:latest
WORKDIR /root/
COPY --from=builder /app/system-walker .
COPY --from=builder /app/static ./static
EXPOSE 8080
CMD ["./system-walker"]