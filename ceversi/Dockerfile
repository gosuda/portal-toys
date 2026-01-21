# Build stage for C and Go
FROM golang:1.25-alpine AS builder

# Install build dependencies for C
RUN apk add --no-cache gcc musl-dev make sqlite-dev openssl-dev cjson-dev uriparser-dev --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

WORKDIR /app

# Copy the entire directory from context
COPY . .

# Build C server
# The Makefile and src are now in the current WORKDIR
RUN make clean && make

# Build Go relay
# Ensure dependencies are consistent with Go 1.25
RUN go mod tidy
RUN CGO_ENABLED=1 go build -o ceversi_relay .

# Run stage
FROM alpine:latest

# Install runtime dependencies for C server
RUN apk add --no-cache sqlite-libs openssl cjson uriparser libgcc --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

WORKDIR /app

# Copy binaries
# Adjusted paths based on the simplified builder structure
COPY --from=builder /app/server ./ceversi-backend
COPY --from=builder /app/ceversi_relay ./ceversi-relay

# Copy static files (needed by C server)
COPY --from=builder /app/index.html .
COPY --from=builder /app/style.css .
COPY --from=builder /app/script.js .

# Expose the Go relay port
EXPOSE 31744

# Entrypoint runs the relay, which starts the backend
ENTRYPOINT ["./ceversi-relay"]
CMD ["--name", "ceversi", "--port", "31744", "--backend-port", "31745", "--c-server", "./ceversi-backend"]
