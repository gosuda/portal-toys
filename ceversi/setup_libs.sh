#!/bin/bash
# Prepares local libraries for Docker build

CWIST_URL="https://github.com/gg582/cwist"
CWIST_BUILD_DIR="cwist_build" # Temporary directory to clone and build cwist

echo "=== Preparing Libraries ===" 

# Ensure target directories exist
mkdir -p libs/include
mkdir -p libs/lib

# Check if cwist is already prepared in libs/
if [ -d "libs/include/cwist" ] && [ -f "libs/lib/libcwist.a" ]; then
    echo "cwist library already prepared in 'libs/' directory. Skipping build."
else
    echo "cwist library not found in 'libs/'. Attempting to clone and build..."

    # Clean up any previous build artifacts
    rm -rf ${CWIST_BUILD_DIR}
    rm -rf libs/include/cwist
    rm -f libs/lib/libcwist.a

    # Clone the cwist repository
    echo "Cloning cwist from ${CWIST_URL}..."
    git clone ${CWIST_URL} ${CWIST_BUILD_DIR}
    if [ $? -ne 0 ]; then
        echo "Error: Failed to clone cwist repository from ${CWIST_URL}"
        exit 1
    fi

    cd ${CWIST_BUILD_DIR}

    # Build cwist
    echo "Building cwist..."
    make
    if [ $? -ne 0 ]; then
        echo "Error: Failed to build cwist."
        cd ..
        rm -rf ${CWIST_BUILD_DIR} # Clean up on failure
        exit 1
    fi

    # Manually copy headers and static library to the project's libs directory
    echo "Copying cwist headers and library to 'libs/'..."
    mkdir -p ../libs/include/cwist
    cp -r include/cwist/. ../libs/include/cwist/
    if [ $? -ne 0 ]; then
        echo "Error: Failed to copy cwist headers."
        cd ..
        rm -rf ${CWIST_BUILD_DIR}
        exit 1
    fi

    cp libcwist.a ../libs/lib/
    if [ $? -ne 0 ]; then
        echo "Error: Failed to copy libcwist.a."
        cd ..
        rm -rf ${CWIST_BUILD_DIR}
        exit 1
    fi

    cd ..
    # Clean up build artifacts
    rm -rf ${CWIST_BUILD_DIR}
    echo "cwist library successfully prepared and copied to 'libs/'."
fi

echo "Library preparation complete."