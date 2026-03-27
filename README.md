# Docksmith

A simplified Docker-like build and container runtime built from scratch in Go.
Implements content-addressed layer storage, a deterministic build cache, and
Linux namespace-based process isolation — no Docker, no runc, no containerd.

## Features

- **6-instruction build language** — `FROM`, `COPY`, `RUN`, `WORKDIR`, `ENV`, `CMD`
- **Content-addressed layers** — every layer stored as a SHA-256-named tar file
- **Deterministic build cache** — cache keys derived from instruction text, env state, workdir, and file hashes
- **Linux namespace isolation** — `chroot` + `CLONE_NEWPID` + `CLONE_NEWNS` + `CLONE_NEWUTS`
- **Same isolation for build and run** — `RUN` during build uses identical primitives as `docksmith run`
- **Verified isolation** — files written inside a container never appear on the host

## Requirements

- Linux (Ubuntu 22.04+ recommended)
- Go 1.22+
- Root / sudo (for namespace creation)

## Installation
```bash
git clone https://github.com/YOUR_USERNAME/docksmith
cd docksmith
go build -o docksmith ./cmd/docksmith/
sudo cp docksmith /usr/local/bin/docksmith
```

### One-time base image setup
```bash
mkdir -p ~/.docksmith/{images,layers,cache}
curl -OL https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/x86_64/alpine-minirootfs-3.18.0-x86_64.tar.gz
DIGEST=$(sha256sum alpine-minirootfs-3.18.0-x86_64.tar.gz | awk '{print "sha256:"$1}')
cp alpine-minirootfs-3.18.0-x86_64.tar.gz ~/.docksmith/layers/$DIGEST
cat > ~/.docksmith/images/alpine:3.18.json << MANIFEST
{
  "name": "alpine",
  "tag": "3.18",
  "digest": "",
  "created": "2024-01-01T00:00:00Z",
  "config": { "Env": [], "Cmd": ["/bin/sh"], "WorkingDir": "" },
  "layers": [{ "digest": "$DIGEST", "size": 3276800, "createdBy": "alpine:3.18 base layer" }]
}
MANIFEST
# copy to root for sudo usage
sudo cp -r ~/.docksmith /root/.docksmith
```

## Usage

### Build an image
```bash
sudo docksmith build -t myapp:latest ./sample-app
sudo docksmith build -t myapp:latest ./sample-app --no-cache
```

### List images
```bash
sudo docksmith images
```

### Run a container
```bash
sudo docksmith run myapp:latest
sudo docksmith run -e GREETING=Howdy myapp:latest
sudo docksmith run myapp:latest /bin/sh -c "echo hello"
```

### Remove an image
```bash
sudo docksmith rmi myapp:latest
```

## Docksmithfile syntax
```dockerfile
FROM alpine:3.18
WORKDIR /app
ENV GREETING=Hello
ENV AUTHOR=Docksmith
COPY app.sh /app/
RUN chmod +x /app/app.sh
CMD ["/bin/sh", "/app/app.sh"]
```

## Build cache behaviour

| Situation | Result |
|---|---|
| Same instruction + same files | `[CACHE HIT]` — layer reused |
| Source file changed | `[CACHE MISS]` — step and all below re-run |
| Instruction text changed | `[CACHE MISS]` — step and all below re-run |
| `--no-cache` flag | All steps are misses |

## Project structure
```
docksmith/
├── cmd/docksmith/       # CLI entry point
├── internal/
│   ├── builder/         # Docksmithfile parser + build engine
│   ├── cache/           # Cache key computation + index
│   ├── image/           # Manifest format + read/write
│   └── runtime/         # Namespace isolation + container execution
├── sample-app/          # Demo app using all 6 instructions
└── README.md
```

## Demo
```bash
# Cold build — all CACHE MISS
sudo docksmith build -t myapp:latest ./sample-app

# Warm build — all CACHE HIT
sudo docksmith build -t myapp:latest ./sample-app

# Edit a file, rebuild — partial CACHE MISS from changed step down
echo "# changed" >> sample-app/app.sh
sudo docksmith build -t myapp:latest ./sample-app

# Run
sudo docksmith run myapp:latest

# Env override
sudo docksmith run -e GREETING=Howdy myapp:latest

# Isolation test — file must NOT appear on host
sudo docksmith run myapp:latest /bin/sh -c "echo secret > /tmp/hostleak.txt"
ls /tmp/hostleak.txt  # No such file or directory ✓

# Remove
sudo docksmith rmi myapp:latest
```

## Design decisions

- **Re-exec pattern** — the binary re-executes itself with `__runtime__` as argv[1] to enter the
  namespace context, identical to how runc and Docker handle namespace setup.
- **Delta layers** — `RUN` snapshots the filesystem before and after execution, storing only
  changed files in the layer tar. This mirrors how real OCI layers work.
- **Sorted tar entries + zeroed timestamps** — ensures byte-for-byte reproducible layer digests
  across rebuilds on the same machine.
- **Cache cascade** — once any step misses, all subsequent steps are forced to miss, preventing
  stale layer reuse downstream.

## License

MIT
