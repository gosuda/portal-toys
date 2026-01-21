# Build stage for C and Go
FROM golang:1.23-alpine AS builder

# Install build dependencies for C
RUN apk add --no-cache gcc musl-dev make sqlite-dev openssl-dev cJSON-dev uriparser-dev

WORKDIR /app

# Copy the entire ceversi directory from context
COPY ceversi/ ./ceversi/

# Build C server
WORKDIR /app/ceversi
# Ensure libs directory exists (it should be prepared by setup_libs.sh on host)
RUN make clean && make

# Build Go relay
RUN go build -o ceversi_relay main.go

# Run stage
FROM alpine:latest

# Install runtime dependencies for C server
RUN apk add --no-cache sqlite-libs openssl cJSON uriparser libgcc

WORKDIR /app

# Copy binaries
COPY --from=builder /app/ceversi/server ./ceversi-backend
COPY --from=builder /app/ceversi/ceversi_relay ./ceversi-relay

# Copy static files (needed by C server)
COPY --from=builder /app/ceversi/index.html .
COPY --from=builder /app/ceversi/style.css .
COPY --from=builder /app/ceversi/script.js .

# Expose the Go relay port
EXPOSE 31744

# Entrypoint runs the relay, which starts the backend
ENTRYPOINT ["./ceversi-relay"]
CMD ["--name", "ceversi", "--port", "31744", "--backend-port", "31745", "--c-server", "./ceversi-backend"]
