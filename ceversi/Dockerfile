# --- C Build Stage ---
FROM alpine:latest AS c_builder

# Install build dependencies from edge community for latest library support
RUN apk update && apk add --no-cache \
    tcc clang lld musl-dev make sqlite-dev openssl-dev cjson-dev uriparser-dev git libc-utils linux-headers cmake \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

WORKDIR /app

# Build libttak (external dependency)
ARG LIBTTAK_REPO="https://github.com/religiya-serdtsa/libttak.git"
ARG LIBTTAK_REF="main"
RUN git clone --depth 1 --branch "${LIBTTAK_REF}" "${LIBTTAK_REPO}" /app/libttak
WORKDIR /app/libttak
RUN make clean && LDFLAGS="${LDFLAGS} -Wl,--no-eh-frame-hdr -fuse-ld=lld" make -j$(nproc) && \
    make install

# Build cwist (core framework)
ARG CWIST_REPO="https://github.com/religiya-serdtsa/cwist.git"
ARG CWIST_REF="main"
RUN git clone --depth 1 --branch "${CWIST_REF}" "${CWIST_REPO}" /app/cwist
RUN git -C /app/cwist submodule update --init --recursive

WORKDIR /app/cwist

# Define a compiler wrapper to enforce gnu17 standard during library build
RUN printf '#!/bin/sh\n/usr/bin/gcc -std=gnu17 "$@"' > /usr/local/bin/gcc-std && \
    chmod +x /usr/local/bin/gcc-std

# Build cwist with linked libttak
RUN rm -rf lib/libttak && mkdir -p lib && \
    cp -r /app/libttak lib/libttak && \
    env LDFLAGS="${LDFLAGS} -fuse-ld=lld -Wl,--no-eh-frame-hdr" CC=/usr/local/bin/gcc-std make -j$(nproc) && \
    env CC=/usr/local/bin/gcc-std make install

# Build Main Server
WORKDIR /app
COPY . .
RUN make clean && make -j$(nproc)


# --- Final Run Stage ---
FROM alpine:latest

# Install runtime shared libraries
RUN apk add --no-cache \
    sqlite-libs openssl cjson uriparser libgcc \
    --repository=https://dl-cdn.alpinelinux.org/alpine/edge/community

WORKDIR /app

# Copy binary and assets from builder
COPY --from=c_builder /app/server ./ceversi
COPY public/ ./public/
COPY index.html.tmpl .
COPY style.css .
COPY script.js .

# Ensure database file existence for initialization
RUN touch othello.db

EXPOSE 31744

ENTRYPOINT ["./ceversi", "--no-certs"]
