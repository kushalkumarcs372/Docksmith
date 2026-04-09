# Docksmith

> A Docker-like build and container runtime built from scratch in Go.
> No Docker. No runc. No containerd. Pure Go + Linux system calls.

[![Go](https://img.shields.io/badge/Go-1.22-blue)](https://go.dev/)
[![Platform](https://img.shields.io/badge/Platform-Linux-yellow)](https://ubuntu.com/)
[![License](https://img.shields.io/badge/License-MIT-green)](LICENSE)

---

## What is Docksmith?

Docksmith implements three things Docker does internally:

| Subsystem | What it does |
|---|---|
| **Build Engine** | Parses a Docksmithfile, executes 6 instructions, writes delta layers as SHA-256 named tar files |
| **Build Cache** | Deterministic cache keys, hit/miss reporting, cascade invalidation, reproducible builds |
| **Container Runtime** | Linux namespace isolation — chroot + CLONE_NEWPID + CLONE_NEWNS + CLONE_NEWUTS |

---

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
- **Offline operation** — zero network calls during build or run

---

## Requirements

- Linux (Ubuntu 22.04+ recommended)
- Go 1.22+
- Root / sudo (for namespace creation)

---

## Installation

```bash
git clone https://github.com/kushalkumarcs372/Docksmith
cd Docksmith
go build -o docksmith ./cmd/docksmith/
sudo cp docksmith /usr/local/bin/docksmith
```

### One-time base image setup

Option A — use the setup script:
```bash
chmod +x setup.sh && ./setup.sh
```

Option B — manual setup:
```bash
mkdir -p ~/.docksmith/{images,layers,cache}

# Download Alpine 3.18
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

---

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

---

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

| Instruction | Produces Layer | Purpose |
|---|---|---|
| `FROM` | No | Sets base image, seeds cache key chain |
| `WORKDIR` | No | Sets working directory in config |
| `ENV` | No | Stores env var in config |
| `COPY` | **Yes** | Copies files, creates delta tar layer |
| `RUN` | **Yes** | Executes command in isolation, creates delta tar layer |
| `CMD` | No | Stores default command in config |

---

## Build Cache Behaviour

| Situation | Result |
|---|---|
| Same instruction + same files | `[CACHE HIT]` — layer reused instantly |
| Source file changed | `[CACHE MISS]` — step and all below re-executed |
| Instruction text changed | `[CACHE MISS]` — step and all below re-executed |
| WORKDIR or ENV changed | `[CACHE MISS]` — step and all below re-executed |
| Layer file missing from disk | `[CACHE MISS]` — treated as miss, cascade applies |
| `--no-cache` flag | All steps are misses |

### Cache Key Formula

```
SHA256 of:
  previous layer digest        ← "what came before me"
  + instruction text           ← "what I was told to do"
  + current WORKDIR            ← "where I am"
  + all ENV pairs (sorted A-Z) ← "what environment I have"
  + file hashes (COPY only, sorted by path)
```

---

## Project Structure

```
docksmith/
├── cmd/docksmith/main.go    ← CLI entry point + namespace re-exec handler
├── internal/
│   ├── builder/builder.go   ← Docksmithfile parser + build engine
│   ├── cache/cache.go       ← cache key computation + index
│   ├── image/image.go       ← manifest format + read/write
│   └── runtime/runtime.go  ← namespace isolation + container execution
├── sample-app/
│   ├── Docksmithfile        ← demo build recipe (uses all 6 instructions)
│   └── app.sh              ← demo application
├── setup.sh                 ← one-time Alpine base image import
├── README.md                ← this file
└── final.md                 ← end-to-end guide + concepts + panel Q&A
```

---

## State Directory Layout

```
~/.docksmith/
├── images/
│   ├── alpine:3.18.json     ← base image manifest
│   └── myapp:latest.json    ← built image manifest
├── layers/
│   └── sha256:<hash>        ← content-addressed tar files
└── cache/
    └── index.json           ← cacheKey → layerDigest map
```

---

## Full Demo Walkthrough

### Before demo — restore Alpine layer
```bash
sudo cp /home/kushal/.docksmith/layers/sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1 \
  /root/.docksmith/layers/sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1
```

### 1. Show the recipe
```bash
cat sample-app/Docksmithfile
```

### 2. Cold build — all CACHE MISS
```bash
sudo docksmith build -t myapp:latest ./sample-app
```

### 3. Warm build — all CACHE HIT
```bash
sudo docksmith build -t myapp:latest ./sample-app
```

### 4. List images
```bash
sudo docksmith images
```

### 5. Run container
```bash
sudo docksmith run myapp:latest
```

### 6. Env override
```bash
sudo docksmith run -e GREETING=Howdy myapp:latest
```

### 7. Isolation test — file must NOT appear on host
```bash
sudo docksmith run myapp:latest /bin/sh -c "echo secret > /tmp/hostleak.txt"
ls /tmp/hostleak.txt
# Expected: No such file or directory ✓
```

### 8. Cache invalidation cascade
```bash
echo "# modified" >> sample-app/app.sh
sudo docksmith build -t myapp:latest ./sample-app
# COPY → [CACHE MISS]  ← file changed
# RUN  → [CACHE MISS]  ← cascaded
```

### 9. Remove image
```bash
sudo docksmith rmi myapp:latest
sudo docksmith images
```

---

## Design Decisions

- **Re-exec pattern** — the binary re-executes itself with `__runtime__` as argv[1] to enter the
  namespace context before chroot. Same pattern used by runc and Docker's containerd-shim.
- **Delta layers** — `RUN` snapshots the filesystem before and after execution using content hashes,
  storing only changed files. Mirrors how real OCI layers work.
- **Sorted tar entries + zeroed metadata** — entries sorted alphabetically, timestamps and
  uid/gid/uname/gname zeroed. Guarantees byte-for-byte identical digests across rebuilds.
- **Cache cascade** — once any step misses, all subsequent steps are forced to miss, preventing
  stale layer reuse downstream.
- **Content-hash delta detection** — RUN uses SHA-256 content hashes (not file size) to detect
  changed files, correctly catching same-size file modifications.
- **Offline operation** — base images imported once at setup. Zero network calls during build or run.

## License

MIT
