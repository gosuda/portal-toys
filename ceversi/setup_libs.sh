#!/bin/bash
# Prepares local libraries for Docker build

mkdir -p libs/include
mkdir -p libs/lib

echo "Copying local cwist library..."
# Try standard location
if [ -d "/usr/local/include/cwist" ]; then
    cp -r /usr/local/include/cwist libs/include/
else
    echo "Warning: /usr/local/include/cwist not found."
fi

if [ -f "/usr/local/lib/libcwist.a" ]; then
    cp /usr/local/lib/libcwist.a libs/lib/
else
    echo "Warning: /usr/local/lib/libcwist.a not found."
fi
