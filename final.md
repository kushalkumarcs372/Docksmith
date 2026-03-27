# Docksmith — End-to-End Guide

## What was built

Docksmith is a from-scratch implementation of a Docker-like container system in Go.
It has three major subsystems:

1. **Build engine** — parses a Docksmithfile, executes 6 instructions, writes content-addressed layer tars
2. **Build cache** — deterministic SHA-256 cache keys, hit/miss reporting, cascade invalidation
3. **Container runtime** — Linux namespace isolation (PID, mount, UTS) via chroot + clone flags

## How isolation works

When `docksmith run` or `RUN` during build executes a command, the binary
re-executes itself (`/proc/self/exe`) with `CLONE_NEWPID`, `CLONE_NEWNS`, and
`CLONE_NEWUTS` flags. The child process then calls `chroot` into the assembled
layer filesystem and `exec`s the target command. The host filesystem is
completely unreachable from inside the container.

## How the cache works

Before every `COPY` or `RUN`, a cache key is computed as SHA-256 of:
- Previous layer digest (or base image manifest digest for the first step)
- Full instruction text
- Current WORKDIR value
- All accumulated ENV key=value pairs (sorted)
- For COPY: SHA-256 of each source file (sorted by path)

A hit reuses the stored layer digest. A miss executes, stores the new layer,
and cascades all downstream steps to misses.

## How layers work

Each `COPY` or `RUN` produces a tar archive containing only the files added or
changed by that step (a delta). The tar is named by its SHA-256 digest and
stored in `~/.docksmith/layers/`. At runtime, all layers are extracted in order
into a temporary directory, with later layers overwriting earlier ones.

## Full end-to-end walkthrough
```bash
# 1. Install
git clone https://github.com/YOUR_USERNAME/docksmith
cd docksmith
go build -o docksmith ./cmd/docksmith/
sudo cp docksmith /usr/local/bin/docksmith

# 2. Import base image (one time only)
mkdir -p ~/.docksmith/{images,layers,cache}
# (follow README setup steps)

# 3. Cold build
cd sample-app
sudo docksmith build -t myapp:latest .
# Step 1/7 : FROM alpine:3.18
# Step 5/7 : COPY app.sh /app/   [CACHE MISS] 0.00s
# Step 6/7 : RUN chmod ...        [CACHE MISS] 0.33s
# Successfully built sha256:XXXX myapp:latest

# 4. Warm build
sudo docksmith build -t myapp:latest .
# Step 5/7 : COPY   [CACHE HIT]
# Step 6/7 : RUN    [CACHE HIT]
# Total build time: 0.01s

# 5. Partial invalidation
echo "# changed" >> app.sh
sudo docksmith build -t myapp:latest .
# Step 5/7 : COPY   [CACHE MISS]  ← file changed
# Step 6/7 : RUN    [CACHE MISS]  ← cascaded

# 6. List images
sudo docksmith images
# NAME    TAG     ID             CREATED
# myapp   latest  fec9f93a5c25   2026-03-27T19:50:06Z

# 7. Run
sudo docksmith run myapp:latest
# Greeting : Hello

# 8. Env override
sudo docksmith run -e GREETING=Howdy myapp:latest
# Greeting : Howdy

# 9. Isolation test
sudo docksmith run myapp:latest /bin/sh -c "echo secret > /tmp/hostleak.txt"
ls /tmp/hostleak.txt
# ls: cannot access '/tmp/hostleak.txt': No such file or directory  ✓

# 10. Remove
sudo docksmith rmi myapp:latest
```

## State directory layout
```
~/.docksmith/
├── images/
│   ├── alpine:3.18.json     # base image manifest
│   └── myapp:latest.json    # built image manifest
├── layers/
│   └── sha256:<hash>        # content-addressed tar files
└── cache/
    └── index.json           # cacheKey -> layerDigest map
```

## What makes this resume-worthy

- Implements OS-level process isolation without any container runtime dependency
- Content-addressed storage identical in concept to Git objects and OCI image spec
- Deterministic, reproducible builds via sorted tars and zeroed timestamps
- Re-exec namespace pattern used by production runtimes like runc
