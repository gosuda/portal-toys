# --- C build stage ---
FROM alpine:latest AS c_builder
RUN apk add --no-cache \
      gcc musl-dev make sqlite-dev openssl-dev cjson-dev uriparser-dev \
      --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

WORKDIR /app
COPY src/ src/
COPY libs/ libs/
COPY Makefile .

RUN make clean && make


# --- Go build stage ---
FROM golang:1.25-alpine AS go_builder
WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY main.go ./
RUN CGO_ENABLED=0 go build -o ceversi_relay .

# --- Run stage ---
FROM alpine:latest
RUN apk add --no-cache \
      sqlite-libs openssl cjson uriparser libgcc \
      --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

WORKDIR /app

COPY --from=c_builder /app/server ./ceversi-backend
COPY --from=go_builder /app/ceversi_relay ./ceversi-relay

COPY index.html .
COPY style.css .
COPY script.js .
COPY othello.db .

EXPOSE 31744
ENTRYPOINT ["./ceversi-relay"]
CMD ["--name", "ceversi", "--port", "31744", "--backend-port", "31745", "--c-server", "./ceversi-backend"]
