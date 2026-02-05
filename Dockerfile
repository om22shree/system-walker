# Step 1: Build - Updated to 1.25 to match your go.mod
FROM golang:1.25-alpine AS builder
WORKDIR /app

# Copy the source code
COPY . .

# Fetch dependencies and build
RUN go mod tidy
RUN go build -o system-walker main.go

# Step 2: Runtime
FROM alpine:latest
# Install ca-certificates just in case you fetch external data later
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/system-walker .
COPY --from=builder /app/static ./static
EXPOSE 8080

CMD ["./system-walker"]