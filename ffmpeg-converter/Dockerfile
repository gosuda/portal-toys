## Multi-stage build: Go binary + ffmpeg runtime

FROM golang:1.25 AS build
WORKDIR /src

# Copy Go module files and download deps first (better caching)
COPY ../go.mod ../go.sum ./
RUN go mod download

# Copy the rest of the repo (for go:embed and local packages)
COPY . .

# Build static binary for linux
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build -o /out/ffmpeg-converter .

# Final image with ffmpeg installed
FROM jrottenberg/ffmpeg:6.0-ubuntu
COPY --from=build /out/ffmpeg-converter /usr/local/bin/ffmpeg-converter

EXPOSE 8098
ENTRYPOINT ["/usr/local/bin/ffmpeg-converter"]
CMD ["--port","8098"]