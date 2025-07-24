#!/bin/bash

# Define output directory
OUTPUT_DIR="build"
mkdir -p "$OUTPUT_DIR"

# Define application name
APP_NAME="SirServer"

# Define platforms and architectures
PLATFORMS=(
    "windows/amd64"
    "windows/arm64"
    "linux/amd64"
    "linux/arm64"
    "darwin/amd64"
    "darwin/arm64"
)

echo "Building Go application for multiple platforms..."

for PLATFORM in "${PLATFORMS[@]}"; do
    IFS='/' read -r GOOS GOARCH <<< "$PLATFORM"

    # Determine executable extension
    EXT=""
    if [ "$GOOS" = "windows" ]; then
        EXT=".exe"
    fi

    # Define output path
    OUTPUT_PATH="$OUTPUT_DIR/$APP_NAME-$GOOS-$GOARCH$EXT"

    echo "Building $APP_NAME for $GOOS/$GOARCH to $OUTPUT_PATH"
    GOOS=$GOOS GOARCH=$GOARCH go build -o "$OUTPUT_PATH" ./main.go

    if [ $? -ne 0 ]; then
        echo "Error building for $GOOS/$GOARCH. Aborting."
        exit 1
    fi
done

echo "Build process completed. Binaries are in the '$OUTPUT_DIR' directory."
