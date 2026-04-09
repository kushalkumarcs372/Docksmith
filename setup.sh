#!/bin/bash
# Docksmith one-time setup — imports Alpine base image into local store

set -e

DOCKSMITH_DIR="$HOME/.docksmith"
ALPINE_URL="https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/x86_64/alpine-minirootfs-3.18.0-x86_64.tar.gz"
ALPINE_DIGEST="sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1"
ALPINE_TAR="/tmp/alpine-minirootfs-3.18.0.tar.gz"

echo "Creating Docksmith state directories..."
mkdir -p "$DOCKSMITH_DIR"/{images,layers,cache}

echo "Downloading Alpine 3.18 base image..."
curl -L "$ALPINE_URL" -o "$ALPINE_TAR"

echo "Verifying digest..."
ACTUAL=$(sha256sum "$ALPINE_TAR" | awk '{print "sha256:"$1}')
if [ "$ACTUAL" != "$ALPINE_DIGEST" ]; then
    echo "ERROR: digest mismatch"
    echo "  expected: $ALPINE_DIGEST"
    echo "  got:      $ACTUAL"
    exit 1
fi

echo "Importing layer..."
cp "$ALPINE_TAR" "$DOCKSMITH_DIR/layers/$ALPINE_DIGEST"

echo "Writing image manifest..."
cat > "$DOCKSMITH_DIR/images/alpine:3.18.json" << MANIFEST
{
  "name": "alpine",
  "tag": "3.18",
  "digest": "",
  "created": "2024-01-01T00:00:00Z",
  "config": { "Env": [], "Cmd": ["/bin/sh"], "WorkingDir": "" },
  "layers": [{ "digest": "$ALPINE_DIGEST", "size": 3276800, "createdBy": "alpine:3.18 base layer" }]
}
MANIFEST

echo "Copying to root store for sudo usage..."
sudo cp -r "$DOCKSMITH_DIR" /root/.docksmith

echo ""
echo "Setup complete. Alpine 3.18 is ready."
echo "Run: sudo docksmith build -t myapp:latest ./sample-app"
