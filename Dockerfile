# Multi-toy image for the portal-toys service farm (tetris, simple-chat,
# youtube-chat, doom, paint). One image, one container per toy; toys register
# with public relays themselves (outbound), so only the local dashboard port
# is published on loopback. run/<toy> is mounted at /data so the identity
# (re-saved at every start) and any future state survive restarts.
#
#   docker build -t portal-toys:local .
#   docker run -d --name toys-tetris --restart unless-stopped \
#     --user 1002:1002 -e TZ=Asia/Seoul \
#     -p 127.0.0.1:3101:3101 \
#     -v "$PWD/run/tetris:/data" -w /data \
#     portal-toys:local \
#     tetris --port 3101 --server-url <relays> --discovery=false \
#            --identity-path /data/identity.json
FROM golang:1.27 AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /out/ ./tetris ./simple-chat ./youtube-chat ./doom ./paint

FROM alpine:3
RUN apk add --no-cache ca-certificates tzdata
COPY --from=build /out/ /usr/local/bin/
ENV TZ=Asia/Seoul
