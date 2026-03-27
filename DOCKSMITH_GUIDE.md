# 🛠️ Docksmith — Complete Demo & Learning Guide

> A Docker-like build and container runtime built from scratch in Go.
> Written by Kushal | GitHub: https://github.com/kushalkumarcs372/Docksmith

---

## 📌 What is Docksmith?

Docksmith is a simplified version of Docker — built entirely from scratch in Go.
It does three things that Docker does internally:

| What | How |
|---|---|
| Reads a **Docksmithfile** (like Dockerfile) | Parses 6 instructions |
| **Builds layers** for each step | Stores them as SHA-256 named tar files |
| **Runs containers** in isolation | Uses Linux namespaces — no Docker, no runc |

**No Docker. No runc. No containerd. Pure Go + Linux system calls.**

---

## 🧱 Project Structure

```
docksmith/
├── cmd/docksmith/main.go        ← CLI entry point (reads your commands)
├── internal/
│   ├── image/image.go           ← reads/writes image manifests (JSON)
│   ├── cache/cache.go           ← remembers what was already built
│   ├── builder/builder.go       ← executes Docksmithfile instructions
│   └── runtime/runtime.go      ← isolates and runs containers
├── sample-app/
│   ├── Docksmithfile            ← the build recipe
│   └── app.sh                  ← the sample application
└── README.md
```

---

## ⚙️ One-Time Setup (Before Demo)

Run this every time before your demo to make sure Alpine base layer is ready:

```bash
sudo cp /home/kushal/.docksmith/layers/sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1 \
  /root/.docksmith/layers/sha256:cb107eb5a1ab71aa2ae788a9c014480e003272ef2e7f76a2936ce9acca4218f1 2>/dev/null
echo "✅ Alpine layer ready"
```

---

## 🎬 Full Demo — Run These In Order

### Step 1 — Show the project structure
```bash
cd ~/docksmith && tree .
```
> **What's happening:** Shows all 4 Go packages and the sample app.

---

### Step 2 — Show the Docksmithfile (the recipe)
```bash
cat ~/docksmith/sample-app/Docksmithfile
```
> **What's happening:** This is the build recipe — like a Dockerfile.
> It uses all 6 instructions: FROM, WORKDIR, ENV, COPY, RUN, CMD.

Expected output:
```
FROM alpine:3.18
WORKDIR /app
ENV GREETING=Hello
ENV AUTHOR=Docksmith
COPY app.sh /app/
RUN chmod +x /app/app.sh
CMD ["/bin/sh", "/app/app.sh"]
```

---

### Step 3 — Show only Alpine exists (nothing built yet)
```bash
sudo docksmith images
```
> **What's happening:** Reads all JSON files in `~/.docksmith/images/` and lists them.
> Only Alpine exists — we haven't built myapp yet.

---

### Step 4 — Cold Build (all CACHE MISS)
```bash
cd ~/docksmith/sample-app && sudo docksmith build -t myapp:latest .
```
> **What's happening:**
> - FROM → loads Alpine base image from disk
> - WORKDIR, ENV, CMD → stored in memory only, no layer created
> - COPY → hashes app.sh, checks cache → MISS → creates tar layer
> - RUN → assembles filesystem, runs chmod inside isolated container → MISS → saves delta layer
> - Writes final manifest to `~/.docksmith/images/myapp:latest.json`

Expected output:
```
Step 1/7 : FROM alpine:3.18
Step 2/7 : WORKDIR /app
Step 3/7 : ENV GREETING=Hello
Step 4/7 : ENV AUTHOR=Docksmith
Step 5/7 : COPY app.sh /app/
  [CACHE MISS] 0.01s
Step 6/7 : RUN chmod +x /app/app.sh
  [CACHE MISS] 0.38s
Step 7/7 : CMD ["/bin/sh", "/app/app.sh"]
Successfully built sha256:XXXXXXXX myapp:latest
Total build time: 0.43s
```

---

### Step 5 — Warm Build (all CACHE HIT)
```bash
sudo docksmith build -t myapp:latest .
```
> **What's happening:**
> - Nothing changed → cache keys match → layers reused from disk
> - No commands executed → near-instant build

Expected output:
```
Step 5/7 : COPY app.sh /app/
  [CACHE HIT]
Step 6/7 : RUN chmod +x /app/app.sh
  [CACHE HIT]
Successfully built sha256:XXXXXXXX myapp:latest
Total build time: 0.01s
```

---

### Step 6 — List images after build
```bash
sudo docksmith images
```
> **What's happening:** myapp:latest now appears with its 12-character digest ID and creation timestamp.

---

### Step 7 — Show what's stored on disk
```bash
sudo find /root/.docksmith -type f | sort
```
> **What's happening:** Shows every file Docksmith has written:
> - `images/` → JSON manifests (table of contents)
> - `layers/` → SHA-256 named tar files (the actual data)
> - `cache/index.json` → the cache lookup table

---

### Step 8 — Show the image manifest
```bash
sudo cat /root/.docksmith/images/myapp:latest.json
```
> **What's happening:** The manifest lists every layer digest, the ENV values,
> the WorkingDir, and the CMD. It's the complete description of the image.

---

### Step 9 — Show the cache index
```bash
sudo cat /root/.docksmith/cache/index.json
```
> **What's happening:** Maps cache keys (left) to layer digests (right).
> Cache key = SHA-256 of (previous layer + instruction + workdir + env + file hashes).

---

### Step 10 — Run the container
```bash
sudo docksmith run myapp:latest
```
> **What's happening:**
> - Extracts all 3 layers into a temp directory
> - Re-executes itself with `__runtime__` flag
> - Creates PID + mount + UTS Linux namespaces
> - chroot's into the temp directory
> - Runs `/bin/sh /app/app.sh` inside the isolated environment
> - Cleans up temp directory after exit

---

### Step 11 — Environment variable override
```bash
sudo docksmith run -e GREETING=Howdy myapp:latest
```
> **What's happening:** The `-e` flag overrides the image's ENV at runtime.
> Image said `GREETING=Hello` but container sees `GREETING=Howdy`.

---

### Step 12 — Isolation proof ✅ (most important)
```bash
sudo docksmith run myapp:latest /bin/sh -c "echo SECRET > /tmp/hostleak.txt"
```
```bash
ls /tmp/hostleak.txt
```
> **What's happening:**
> - Container wrote a file to `/tmp/hostleak.txt` inside its own filesystem
> - After container exits, temp directory is deleted
> - Host `/tmp` was never touched
> - `ls` confirms: file does NOT exist on host

Expected:
```
ls: cannot access '/tmp/hostleak.txt': No such file or directory
```

---

### Step 13 — Cache invalidation (cascade)
```bash
echo "# v2" >> ~/docksmith/sample-app/app.sh
sudo docksmith build -t myapp:latest .
```
> **What's happening:**
> - app.sh changed → its SHA-256 hash changed
> - COPY cache key changed → CACHE MISS
> - RUN cache key includes previous layer digest → also changed → CACHE MISS
> - This is called **cache cascade** — one change invalidates everything below it

---

### Step 14 — Remove the image
```bash
sudo docksmith rmi myapp:latest
sudo docksmith images
```
> **What's happening:** Removes the manifest JSON and all associated layer tar files.
> myapp disappears from the image list.

---

## 🧠 Key Concepts to Understand

### 1. Content-Addressed Storage
Every layer file is named by the **SHA-256 hash of its own contents**.

```
layer contents → SHA-256 → "sha256:44cdd435..."  ← filename
```

- Same files always produce the same hash → same filename → one file on disk
- This is identical to how **Git stores blobs and commits**
- This is identical to how **Docker/OCI stores layers**

---

### 2. Delta Layers
Each `COPY` or `RUN` stores only what **changed** — not the full filesystem.

```
Layer 1 (Alpine)  → full Linux OS       → 3.2 MB
Layer 2 (COPY)    → just app.sh         → 2 KB
Layer 3 (RUN)     → just chmod change   → 1 KB
```

At runtime all layers are stacked in order. Later layers overwrite earlier ones at the same path.

---

### 3. Build Cache
Before every `COPY` or `RUN`, Docksmith computes a **cache key**:

```
cache key = SHA256 of:
  previous layer digest    ← "what came before"
  + instruction text       ← "what I'm doing"
  + current WORKDIR        ← "where I am"
  + all ENV values (sorted)← "what environment"
  + file hashes (COPY only)← "what files"
```

- **HIT** → reuse stored layer, skip execution
- **MISS** → execute, store result, cascade all steps below to MISS

---

### 4. Linux Namespace Isolation
When running a container, Docksmith creates 3 Linux namespaces:

| Namespace | What it isolates |
|---|---|
| `CLONE_NEWPID` | Container has its own process tree starting at PID 1 |
| `CLONE_NEWNS` | Container has its own filesystem mount view |
| `CLONE_NEWUTS` | Container has its own hostname |

Then it calls **`chroot`** — changes the root filesystem (`/`) to the assembled layer directory.
The process cannot see or touch anything outside that directory.

---

### 5. The Re-exec Pattern
This is the clever trick Docksmith uses (same as real runtimes like runc):

```
Step 1: docksmith run myapp        ← parent process
Step 2: parent calls /proc/self/exe __runtime__ <args>
        with CLONE_NEWPID + CLONE_NEWNS flags
Step 3: child process starts INSIDE new namespaces
Step 4: child calls chroot() → now isolated
Step 5: child exec's the actual command
```

`/proc/self/exe` is a Linux special file that points to the currently running binary.
So the binary re-executes itself — but this time inside the namespace.

---

### 6. The 6 Docksmithfile Instructions

| Instruction | Produces Layer? | What it does |
|---|---|---|
| `FROM <image>` | No | Sets base image, seeds cache key chain |
| `WORKDIR <path>` | No | Sets working directory in config + future cache keys |
| `ENV KEY=VALUE` | No | Stores env var in config + future cache keys |
| `COPY <src> <dest>` | **Yes** | Copies files, creates delta tar layer |
| `RUN <command>` | **Yes** | Executes command in isolation, creates delta tar layer |
| `CMD ["cmd","arg"]` | No | Stores default command in config |

---

## ❓ Panel Questions & Answers

**Q: How is this different from Docker?**
> Docker has a daemon, supports networking, image registries, and resource limits.
> Docksmith implements the core concepts — layered builds, content-addressed storage,
> and namespace isolation — as a single binary with no daemon.

**Q: What is a Linux namespace?**
> A kernel feature that gives a process its own isolated view of system resources.
> PID namespace = own process tree. Mount namespace = own filesystem. UTS = own hostname.

**Q: What is content-addressed storage?**
> Files are named by their SHA-256 hash. Identical content = identical name = stored once.
> Same concept as Git objects. Enables deduplication and integrity verification.

**Q: What is cache cascade?**
> Once any build step is a cache miss, all steps below it are forced to be misses too.
> This prevents a new layer from being built on top of a stale cached layer.

**Q: Why use /proc/self/exe for isolation?**
> It's the re-exec pattern. The binary runs itself again but with namespace clone flags,
> so the child process starts already inside the new namespaces before chroot is called.
> This is the same pattern used by runc and Docker's containerd-shim.

**Q: Are builds reproducible?**
> Yes. Tar entries are added in sorted order with timestamps zeroed.
> Same files + same instructions = identical SHA-256 layer digests every time.

---

## 📁 State Directory Layout

```
~/.docksmith/
├── images/
│   ├── alpine:3.18.json      ← base image manifest (manually imported)
│   └── myapp:latest.json     ← built image manifest
├── layers/
│   ├── sha256:cb107eb5...    ← Alpine Linux filesystem (3.2MB tar)
│   ├── sha256:44cdd435...    ← COPY layer (just app.sh)
│   └── sha256:5f70bf18...    ← RUN layer (just chmod change)
└── cache/
    └── index.json            ← cacheKey → layerDigest lookup table
```

---

## 🔗 Resources to Learn More

- **Linux Namespaces:** `man 7 namespaces`
- **OCI Image Spec:** https://github.com/opencontainers/image-spec
- **How Docker works internally:** https://docs.docker.com/get-started/overview/
- **chroot man page:** `man 2 chroot`
- **Go systems programming:** https://pkg.go.dev/syscall

---

*Built with Go 1.22 on Ubuntu 22.04 | No external dependencies*
