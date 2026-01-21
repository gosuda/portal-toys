#!/bin/bash
# Safe SSL Key Generator

KEY_FILE="server.key"
CRT_FILE="server.crt"

# Check if keys exist
if [ -f "$KEY_FILE" ] && [ -f "$CRT_FILE" ]; then
    echo "SSL Keys already exist."
else
    echo "Generating new SSL keys..."
    openssl req -x509 -newkey rsa:4096 -keyout "$KEY_FILE" -out "$CRT_FILE" -days 365 -nodes -subj "/CN=localhost" 2>/dev/null
    chmod 600 "$KEY_FILE"
    echo "Keys generated."
fi
