# Docksmith

A simplified Docker-like build and container runtime built from scratch in Go.
Implements content-addressed layer storage, a deterministic build cache, and
Linux namespace-based process isolation — no Docker, no runc, no containerd.

## Features

- **6-instruction build language** — `FROM`, `COPY`, `RUN`, `WORKDIR`, `ENV`, `CMD`
- **Content-addressed layers** — every layer stored as a SHA-256-named tar file
- **Delta layers** — COPY and RUN store only changed files, not full snapshots
- **Deterministic build cache** — cache keys derived from instruction text, env state, workdir, and file hashes
- **Reproducible builds** — tar entries sorted, timestamps and uid/gid zeroed for byte-for-byte identical digests
- **Glob support** — COPY supports both `*` and `**` patterns
- **Linux namespace isolation** — `chroot` + `CLONE_NEWPID` + `CLONE_NEWNS` + `CLONE_NEWUTS`
- **Same isolation for build and run** — `RUN` during build uses identical primitives as `docksmith run`
- **Verified isolation** — files written inside a container never appear on the host

## Requirements

- Linux (Ubuntu 22.04+ recommended)
- Go 1.22+
- Root / sudo (for namespace creation)

## Installation

```bash
git clone https://github.com/kushalkumarcs372/Docksmith
cd Docksmith
go build -o docksmith ./cmd/docksmith/
sudo cp docksmith /usr/local/bin/docksmith
```

### One-time base image setup

```bash
mkdir -p ~/.docksmith/{images,layers,cache}

# Download Alpine base image
curl -OL https://dl-cdn.alpinelinux.org/alpine/v3.18/releases/x86_64/alpine-minirootfs-3.18.0-x86_64.tar.gz

# Copy to layers store
DIGEST="sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1"
cp alpine-minirootfs-3.18.0-x86_64.tar.gz ~/.docksmith/layers/$DIGEST

# Write manifest
cat > ~/.docksmith/images/alpine:3.18.json << 'MANIFEST'
{
  "name": "alpine",
  "tag": "3.18",
  "digest": "",
  "created": "2024-01-01T00:00:00Z",
  "config": { "Env": [], "Cmd": ["/bin/sh"], "WorkingDir": "" },
  "layers": [{ "digest": "sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1", "size": 3276800, "createdBy": "alpine:3.18 base layer" }]
}
MANIFEST

# Copy to root for sudo usage
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

## Docksmithfile Syntax

```dockerfile
FROM alpine:3.18
WORKDIR /app
ENV GREETING=Hello
ENV AUTHOR=Docksmith
COPY app.sh /app/
RUN chmod +x /app/app.sh
CMD ["/bin/sh", "/app/app.sh"]
```

## Build Cache Behaviour

| Situation | Result |
|---|---|
| Same instruction + same files | `[CACHE HIT]` — layer reused instantly |
| Source file changed | `[CACHE MISS]` — step and all below re-executed |
| Instruction text changed | `[CACHE MISS]` — step and all below re-executed |
| WORKDIR or ENV changed | `[CACHE MISS]` — step and all below re-executed |
| Layer file missing from disk | `[CACHE MISS]` — treated as miss, cascade applies |
| `--no-cache` flag | All steps are misses |

## Cache Key Formula

```
SHA256 of:
  previous layer digest
  + instruction text
  + current WORKDIR
  + all ENV pairs (sorted lexicographically)
  + file hashes (COPY only, sorted by path)
```

## Project Structure

```
docksmith/
├── cmd/docksmith/main.go    ← CLI entry point
├── internal/
│   ├── builder/builder.go   ← Docksmithfile parser + build engine
│   ├── cache/cache.go       ← cache key computation + index
│   ├── image/image.go       ← manifest format + read/write
│   └── runtime/runtime.go  ← namespace isolation + container execution
├── sample-app/
│   ├── Docksmithfile        ← demo build recipe
│   └── app.sh              ← demo application
├── README.md
└── final.md                 ← end-to-end guide + concepts + panel Q&A
```

## Full Demo

```bash
# Before demo — restore Alpine layer
sudo cp /home/kushal/.docksmith/layers/sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1 \
  /root/.docksmith/layers/sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1

# Cold build — all CACHE MISS
sudo docksmith build -t myapp:latest ./sample-app

# Warm build — all CACHE HIT, near-instant
sudo docksmith build -t myapp:latest ./sample-app

# Edit a file — partial CACHE MISS from changed step down
echo "# changed" >> sample-app/app.sh
sudo docksmith build -t myapp:latest ./sample-app

# List images
sudo docksmith images

# Run container
sudo docksmith run myapp:latest

# Env override
sudo docksmith run -e GREETING=Howdy myapp:latest

# Isolation test — file must NOT appear on host
sudo docksmith run myapp:latest /bin/sh -c "echo secret > /tmp/hostleak.txt"
ls /tmp/hostleak.txt  # No such file or directory ✓

# Remove image
sudo docksmith rmi myapp:latest
```

## Design Decisions

- **Re-exec pattern** — the binary re-executes itself with `__runtime__` as argv[1] to enter the
  namespace context before chroot. Same pattern used by runc and Docker's containerd-shim.
- **Delta layers** — `RUN` snapshots the filesystem before and after execution, storing only
  changed files in the layer tar. Mirrors how real OCI layers work.
- **Sorted tar entries + zeroed metadata** — entries sorted alphabetically, timestamps and
  uid/gid/uname/gname zeroed. Guarantees byte-for-byte identical digests across rebuilds.
- **Cache cascade** — once any step misses, all subsequent steps are forced to miss, preventing
  stale layer reuse downstream.
- **Offline operation** — base images imported once at setup. Zero network calls during build or run.

## Requirements Compliance

| PDF Section | Requirement | Status |
|---|---|---|
| Build Language | All 6 instructions implemented | ✅ |
| Build Language | Unrecognised instruction fails with line number | ✅ |
| Build Language | COPY supports `*` and `**` globs | ✅ |
| Build Language | RUN executes inside image filesystem | ✅ |
| Image Format | Manifest with all required fields | ✅ |
| Image Format | Delta layers — not full snapshots | ✅ |
| Image Format | Reproducible digests — sorted tar, zeroed metadata | ✅ |
| Build Cache | Correct cache key formula | ✅ |
| Build Cache | Hit/miss reporting with timing | ✅ |
| Build Cache | Cascade invalidation | ✅ |
| Build Cache | --no-cache flag | ✅ |
| Runtime | Linux namespace isolation | ✅ |
| Runtime | Same isolation for RUN and docksmith run | ✅ |
| Runtime | Verified isolation — files don't escape container | ✅ |
| CLI | All 4 commands implemented | ✅ |
| Constraints | No Docker/runc/containerd | ✅ |
| Constraints | No network during build or run | ✅ |

## License

MIT
