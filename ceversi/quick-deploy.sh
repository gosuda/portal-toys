#!/bin/bash
set -e

echo "=== Ceversi Quick Deploy ==="

echo "[1/4] Preparing SSL Keys..."
chmod +x keygen.sh
./keygen.sh

echo "[2/4] Preparing Library Dependencies..."
chmod +x setup_libs.sh
./setup_libs.sh

echo "[3/4] Building and Starting Docker Container..."
# Check if docker is available
if ! command -v docker &> /dev/null; then
    echo "Error: Docker is not installed."
    exit 1
fi

if ! command -v docker-compose &> /dev/null; then
    echo "Error: docker-compose is not installed."
    exit 1
fi

# Ensure database file exists (touch it) so Docker mounts it as a file not dir
touch othello.db

docker compose up -d --build

echo "[4/4] Deployment Complete!"
echo "Server is running at: https://localhost:31744"
echo "Note: Accept the self-signed certificate warning in your browser."
