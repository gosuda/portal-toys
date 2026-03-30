# syntax=docker/dockerfile:1

FROM golang:1.22 AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/manager ./manager
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /out/worker ./worker

FROM alpine:3.19 AS runtime-base
RUN apk add --no-cache ca-certificates bash
COPY target_binary.sh /opt/target_binary.sh
RUN chmod +x /opt/target_binary.sh

FROM runtime-base AS manager
COPY --from=builder /out/manager /usr/local/bin/manager
ENV MANAGER_ADDR=:8080
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/manager"]

FROM runtime-base AS worker
COPY --from=builder /out/worker /usr/local/bin/worker
ENV TARGET_BINARY=/opt/target_binary.sh \
    WORKER_PORT=8081
EXPOSE 8081
ENTRYPOINT ["/usr/local/bin/worker"]
