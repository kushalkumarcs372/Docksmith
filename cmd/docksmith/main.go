// Command docksmith is the CLI entry point for the Docksmith build engine
// and container runtime.
//
// It also doubles as the re-exec target for namespace isolation: when
// invoked as `docksmith __runtime__ <rootDir> <workdir> <command> [env...]`
// (see runtime.RunNamespaced, which launches /proc/self/exe with these
// args), it hands off directly to runtime.ExecuteInNamespace instead of
// going through normal CLI parsing. This mirrors the re-exec pattern used
// by runc and containerd-shim.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kushal/docksmith/internal/builder"
	"github.com/kushal/docksmith/internal/image"
	"github.com/kushal/docksmith/internal/runtime"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	// Re-exec entry point: runtime.RunNamespaced calls
	// /proc/self/exe __runtime__ <rootDir> <workdir> <command> [env...]
	// inside a fresh CLONE_NEWPID|CLONE_NEWNS|CLONE_NEWUTS namespace.
	if os.Args[1] == "__runtime__" {
		runtime.ExecuteInNamespace(os.Args[2:])
		return
	}

	switch os.Args[1] {
	case "build":
		cmdBuild(os.Args[2:])
	case "images":
		cmdImages()
	case "run":
		cmdRun(os.Args[2:])
	case "rmi":
		cmdRmi(os.Args[2:])
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "docksmith: unknown command %q\n\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `Docksmith - a Docker-like build and container runtime built from scratch in Go

Usage:
  docksmith build -t <name:tag> <context> [--no-cache]
  docksmith images
  docksmith run [-e KEY=VALUE ...] <name:tag> [cmd...]
  docksmith rmi <name:tag>

Examples:
  sudo docksmith build -t myapp:latest ./sample-app
  sudo docksmith build -t myapp:latest ./sample-app --no-cache
  sudo docksmith images
  sudo docksmith run myapp:latest
  sudo docksmith run -e GREETING=Howdy myapp:latest
  sudo docksmith run myapp:latest /bin/sh -c "echo hello"
  sudo docksmith rmi myapp:latest`)
}

// ---- build ----
//
// Parsed by hand (not the stdlib "flag" package) because --no-cache needs
// to work whether it appears before or after the context directory, e.g.:
//   docksmith build -t myapp:latest ./sample-app --no-cache
// stdlib flag.Parse stops at the first non-flag argument, which would
// silently ignore --no-cache in that ordering.
func cmdBuild(args []string) {
	var tag, context string
	noCache := false

	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-t" || a == "--tag":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "build: -t requires a value")
				os.Exit(1)
			}
			tag = args[i]
		case strings.HasPrefix(a, "-t="):
			tag = strings.TrimPrefix(a, "-t=")
		case strings.HasPrefix(a, "--tag="):
			tag = strings.TrimPrefix(a, "--tag=")
		case a == "--no-cache":
			noCache = true
		default:
			if context == "" {
				context = a
			}
		}
	}

	if tag == "" || context == "" {
		fmt.Fprintln(os.Stderr, "usage: docksmith build -t <name:tag> <context> [--no-cache]")
		os.Exit(1)
	}

	if err := builder.Build(builder.BuildOptions{
		Tag:     tag,
		Context: context,
		NoCache: noCache,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "build failed: %v\n", err)
		os.Exit(1)
	}
}

// ---- images ----

func cmdImages() {
	entries, err := os.ReadDir(image.ImagesDir())
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("REPOSITORY   TAG      IMAGE ID       CREATED                    SIZE")
			return
		}
		fmt.Fprintf(os.Stderr, "images: %v\n", err)
		os.Exit(1)
	}

	type row struct {
		repo, tag, id, created string
		size                   int64
	}
	var rows []row

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(image.ImagesDir(), e.Name()))
		if err != nil {
			continue
		}
		var m image.Manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		var total int64
		for _, l := range m.Layers {
			total += l.Size
		}
		id := m.Digest
		if strings.HasPrefix(id, "sha256:") {
			id = id[len("sha256:"):]
		}
		if len(id) > 12 {
			id = id[:12]
		}
		rows = append(rows, row{m.Name, m.Tag, id, m.Created, total})
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].repo != rows[j].repo {
			return rows[i].repo < rows[j].repo
		}
		return rows[i].tag < rows[j].tag
	})

	fmt.Println("REPOSITORY   TAG      IMAGE ID       CREATED                    SIZE")
	for _, r := range rows {
		fmt.Printf("%-12s %-8s %-14s %-26s %s\n", r.repo, r.tag, r.id, r.created, humanSize(r.size))
	}
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

// ---- run ----
//
// Also hand-parsed: any number of "-e KEY=VALUE" pairs may precede the
// image reference; everything from the image reference onward (including
// any further tokens that look like flags, e.g. "/bin/sh -c ...") is the
// container command and must NOT be interpreted as docksmith flags.
func cmdRun(args []string) {
	var envs []string
	var ref string
	var cmdArgs []string

	i := 0
	for i < len(args) {
		a := args[i]
		if a == "-e" {
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, "run: -e requires KEY=VALUE")
				os.Exit(1)
			}
			envs = append(envs, args[i])
			i++
			continue
		}
		if strings.HasPrefix(a, "-e=") {
			envs = append(envs, strings.TrimPrefix(a, "-e="))
			i++
			continue
		}
		// First remaining token is the image reference; everything after
		// it is the container command, untouched.
		ref = a
		cmdArgs = args[i+1:]
		break
	}

	if ref == "" {
		fmt.Fprintln(os.Stderr, "usage: docksmith run [-e KEY=VALUE ...] <name:tag> [cmd...]")
		os.Exit(1)
	}

	parts := strings.SplitN(ref, ":", 2)
	name := parts[0]
	tag := "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}

	if err := runtime.Run(runtime.RunOptions{
		Name:         name,
		Tag:          tag,
		Cmd:          cmdArgs,
		EnvOverrides: envs,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "run failed: %v\n", err)
		os.Exit(1)
	}
}

// ---- rmi ----

func cmdRmi(args []string) {
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "usage: docksmith rmi <name:tag>")
		os.Exit(1)
	}
	ref := args[0]
	parts := strings.SplitN(ref, ":", 2)
	name := parts[0]
	tag := "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}

	path := image.ManifestPath(name, tag)
	if _, err := os.Stat(path); err != nil {
		fmt.Fprintf(os.Stderr, "rmi: image %s:%s not found\n", name, tag)
		os.Exit(1)
	}
	if err := os.Remove(path); err != nil {
		fmt.Fprintf(os.Stderr, "rmi: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Deleted: %s:%s\n", name, tag)
}
