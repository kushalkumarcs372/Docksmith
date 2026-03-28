package builder

import (
	"archive/tar"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/kushal/docksmith/internal/cache"
	"github.com/kushal/docksmith/internal/runtime"
	"github.com/kushal/docksmith/internal/image"
)

type Instruction struct {
	Line int
	Op   string
	Args string
}

func parseFile(docksmithfile string) ([]Instruction, error) {
	data, err := os.ReadFile(docksmithfile)
	if err != nil {
		return nil, err
	}
	var instructions []Instruction
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimFunc(raw, unicode.IsSpace)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		op := strings.ToUpper(parts[0])
		args := ""
		if len(parts) == 2 {
			args = strings.TrimSpace(parts[1])
		}
		valid := map[string]bool{"FROM": true, "COPY": true, "RUN": true, "WORKDIR": true, "ENV": true, "CMD": true}
		if !valid[op] {
			return nil, fmt.Errorf("line %d: unrecognised instruction %q", i+1, op)
		}
		instructions = append(instructions, Instruction{Line: i + 1, Op: op, Args: args})
	}
	return instructions, nil
}

func hashFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", h.Sum(nil)), nil
}

func createLayer(srcDir string, files []string) (string, int64, error) {
	h := sha256.New()
	tmp, err := os.CreateTemp("", "docksmith-layer-*.tar")
	if err != nil {
		return "", 0, err
	}
	defer tmp.Close()

	mw := io.MultiWriter(tmp, h)
	tw := tar.NewWriter(mw)

	sort.Strings(files)
	for _, rel := range files {
		abs := filepath.Join(srcDir, rel)
		info, err := os.Lstat(abs)
		if err != nil {
			return "", 0, err
		}
		hdr, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return "", 0, err
		}
		hdr.Name = rel
		hdr.ModTime = time.Time{}
		hdr.AccessTime = time.Time{}
		hdr.ChangeTime = time.Time{}
			hdr.Uid = 0
			hdr.Gid = 0
			hdr.Uname = ""
			hdr.Gname = ""
			hdr.Uid = 0
			hdr.Gid = 0
			hdr.Uname = ""
			hdr.Gname = ""
		if err := tw.WriteHeader(hdr); err != nil {
			return "", 0, err
		}
		if !info.IsDir() {
			f, err := os.Open(abs)
			if err != nil {
				return "", 0, err
			}
			_, err = io.Copy(tw, f)
			f.Close()
			if err != nil {
				return "", 0, err
			}
		}
	}
	tw.Close()

	digest := fmt.Sprintf("sha256:%x", h.Sum(nil))
	size := int64(0)
	if info, err := tmp.Stat(); err == nil {
		size = info.Size()
	}

	dest := image.LayerPath(digest)
	if err := os.Rename(tmp.Name(), dest); err != nil {
		data, _ := os.ReadFile(tmp.Name())
		os.WriteFile(dest, data, 0644)
		os.Remove(tmp.Name())
	}
	return digest, size, nil
}



func matchesPattern(pattern, rel string) bool {
	if strings.Contains(pattern, "**") {
		prefix := strings.TrimSuffix(strings.Split(pattern, "**")[0], "/")
		suffix := strings.TrimPrefix(strings.Split(pattern, "**")[1], "/")
		if prefix != "" && !strings.HasPrefix(rel, prefix) {
			return false
		}
		if suffix != "" {
			return strings.HasSuffix(rel, suffix)
		}
		return true
	}
	if m, _ := filepath.Match(pattern, rel); m {
		return true
	}
	if m, _ := filepath.Match(pattern, filepath.Base(rel)); m {
		return true
	}
	return false
}

func globFiles(contextDir, pattern string) ([]string, error) {
	var matches []string
	err := filepath.WalkDir(contextDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(contextDir, path)
		if rel == "." || rel == "Docksmithfile" {
			return nil
		}
		if !d.IsDir() && matchesPattern(pattern, rel) {
			matches = append(matches, rel)
		}
		return nil
	})
	return matches, err
}


type BuildOptions struct {
	Tag       string
	Context   string
	NoCache   bool
}

func Build(opts BuildOptions) error {
	parts := strings.SplitN(opts.Tag, ":", 2)
	name := parts[0]
	tag := "latest"
	if len(parts) == 2 {
		tag = parts[1]
	}

	docksmithfile := filepath.Join(opts.Context, "Docksmithfile")
	instructions, err := parseFile(docksmithfile)
	if err != nil {
		return err
	}

	total := len(instructions)
	var manifest *image.Manifest
	workdir := ""
	var envState []string
	prevDigest := ""
	cacheMissed := false
	stepNum := 0
	originalCreated := ""

	for _, inst := range instructions {
		stepNum++
		fmt.Printf("Step %d/%d : %s %s\n", stepNum, total, inst.Op, inst.Args)

		switch inst.Op {
		case "FROM":
			fparts := strings.SplitN(inst.Args, ":", 2)
			bname := fparts[0]
			btag := "latest"
			if len(fparts) == 2 {
				btag = fparts[1]
			}
			base, err := image.Load(bname, btag)
			if err != nil {
				return err
			}
			manifest = &image.Manifest{
				Name:   name,
				Tag:    tag,
				Config: base.Config,
				Layers: append([]image.LayerMeta{}, base.Layers...),
			}
			manifest.Config.Env = append([]string{}, base.Config.Env...)

			// use base manifest digest as prevDigest seed
			tmp := *base
			tmp.Digest = ""
			canonical, _ := json.Marshal(tmp)
			sum := sha256.Sum256(canonical)
			prevDigest = fmt.Sprintf("sha256:%x", sum)

			// check if existing image has a created timestamp to preserve
			if existing, err := image.Load(name, tag); err == nil {
				originalCreated = existing.Created
			}

		case "WORKDIR":
			workdir = inst.Args
			manifest.Config.WorkingDir = workdir

		case "ENV":
			eparts := strings.SplitN(inst.Args, "=", 2)
			if len(eparts) != 2 {
				return fmt.Errorf("line %d: ENV requires KEY=VALUE format", inst.Line)
			}
			key := strings.TrimSpace(eparts[0])
			val := strings.TrimSpace(eparts[1])
			kvp := key + "=" + val
			found := false
			for i, e := range envState {
				if strings.HasPrefix(e, key+"=") {
					envState[i] = kvp
					found = true
					break
				}
			}
			if !found {
				envState = append(envState, kvp)
			}
			manifest.Config.Env = append(manifest.Config.Env, kvp)

		case "CMD":
			var cmdArr []string
			if err := json.Unmarshal([]byte(inst.Args), &cmdArr); err != nil {
				return fmt.Errorf("line %d: CMD requires JSON array format, e.g. [\"sh\",\"-c\",\"echo hi\"]", inst.Line)
			}
			manifest.Config.Cmd = cmdArr

		case "COPY":
			cparts := strings.Fields(inst.Args)
			if len(cparts) < 2 {
				return fmt.Errorf("line %d: COPY requires <src> <dest>", inst.Line)
			}
			srcPattern := cparts[0]
			dest := cparts[1]

			srcFiles, err := globFiles(opts.Context, srcPattern)
			if err != nil || len(srcFiles) == 0 {
				return fmt.Errorf("line %d: COPY no files matched %q", inst.Line, srcPattern)
			}

			// compute src hashes for cache key
			var srcHashes []string
			for _, rel := range srcFiles {
				h, err := hashFile(filepath.Join(opts.Context, rel))
				if err != nil {
					return err
				}
				srcHashes = append(srcHashes, rel+":"+h)
			}

			keyIn := cache.KeyInput{
				PrevDigest:  prevDigest,
				Instruction: fmt.Sprintf("COPY %s", inst.Args),
				Workdir:     workdir,
				Env:         envState,
				SrcHashes:   srcHashes,
			}
			cacheKey := cache.ComputeKey(keyIn)

			if !opts.NoCache && !cacheMissed {
				if digest, ok := cache.Lookup(cacheKey); ok {
					if _, err := os.Stat(image.LayerPath(digest)); err == nil {
						fmt.Printf("  [CACHE HIT]\n")
						manifest.Layers = append(manifest.Layers, image.LayerMeta{
							Digest:    digest,
							Size:      layerSize(digest),
							CreatedBy: fmt.Sprintf("COPY %s", inst.Args),
						})
						prevDigest = digest
						continue
					}
				}
			}

			start := time.Now()
			// build layer: copy files into temp dir mirroring dest
			tmpDir, err := os.MkdirTemp("", "docksmith-copy-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(tmpDir)

			if workdir != "" {
				os.MkdirAll(filepath.Join(tmpDir, workdir), 0755)
			}

			var layerFiles []string
			for _, rel := range srcFiles {
				var finalDest string
				if strings.HasSuffix(dest, "/") || len(srcFiles) > 1 {
					finalDest = filepath.Join(dest, filepath.Base(rel))
				} else {
					finalDest = dest
				}
				fullDest := filepath.Join(tmpDir, finalDest)
				os.MkdirAll(filepath.Dir(fullDest), 0755)
				data, err := os.ReadFile(filepath.Join(opts.Context, rel))
				if err != nil {
					return err
				}
				info, _ := os.Stat(filepath.Join(opts.Context, rel))
				if err := os.WriteFile(fullDest, data, info.Mode()); err != nil {
					return err
				}
				relDest := strings.TrimPrefix(finalDest, "/")
				layerFiles = append(layerFiles, relDest)
			}

			digest, size, err := createLayer(tmpDir, layerFiles)
			if err != nil {
				return err
			}

			elapsed := time.Since(start)
			fmt.Printf("  [CACHE MISS] %.2fs\n", elapsed.Seconds())

			if !opts.NoCache {
				cache.Store(cacheKey, digest)
			}
			cacheMissed = true
			manifest.Layers = append(manifest.Layers, image.LayerMeta{
				Digest:    digest,
				Size:      size,
				CreatedBy: fmt.Sprintf("COPY %s", inst.Args),
			})
			prevDigest = digest

		case "RUN":
			keyIn := cache.KeyInput{
				PrevDigest:  prevDigest,
				Instruction: fmt.Sprintf("RUN %s", inst.Args),
				Workdir:     workdir,
				Env:         envState,
			}
			cacheKey := cache.ComputeKey(keyIn)

			if !opts.NoCache && !cacheMissed {
				if digest, ok := cache.Lookup(cacheKey); ok {
					if _, err := os.Stat(image.LayerPath(digest)); err == nil {
						fmt.Printf("  [CACHE HIT]\n")
						manifest.Layers = append(manifest.Layers, image.LayerMeta{
							Digest:    digest,
							Size:      layerSize(digest),
							CreatedBy: fmt.Sprintf("RUN %s", inst.Args),
						})
						prevDigest = digest
						continue
					}
				}
			}

			start := time.Now()

			// assemble full filesystem so far
			rootDir, err := os.MkdirTemp("", "docksmith-run-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(rootDir)

			if err := runtime.ExtractLayers(manifest.Layers, rootDir); err != nil {
				return err
			}
			if workdir != "" {
				os.MkdirAll(filepath.Join(rootDir, workdir), 0755)
			}

			// snapshot before
			beforeSnap, _ := snapshotDir(rootDir)

			// run command in isolation
			runEnv := append([]string{}, envState...)
			if err := runtime.RunNamespaced(rootDir, workdir, runEnv, inst.Args); err != nil {
				return fmt.Errorf("RUN failed: %w", err)
			}

			// snapshot after, compute delta
			afterSnap, _ := snapshotDir(rootDir)
			deltaFiles := diffSnapshot(beforeSnap, afterSnap)

			digest, size, err := createLayer(rootDir, deltaFiles)
			if err != nil {
				return err
			}

			elapsed := time.Since(start)
			fmt.Printf("  [CACHE MISS] %.2fs\n", elapsed.Seconds())

			if !opts.NoCache {
				cache.Store(cacheKey, digest)
			}
			cacheMissed = true
			manifest.Layers = append(manifest.Layers, image.LayerMeta{
				Digest:    digest,
				Size:      size,
				CreatedBy: fmt.Sprintf("RUN %s", inst.Args),
			})
			prevDigest = digest
		}
	}

	// preserve created timestamp if all cache hits
	if originalCreated != "" && !cacheMissed {
		manifest.Created = originalCreated
	} else if manifest.Created == "" {
		manifest.Created = time.Now().UTC().Format(time.RFC3339)
	}

	if err := image.Save(manifest); err != nil {
		return err
	}

	shortDigest := manifest.Digest
	if len(shortDigest) > 15 {
		shortDigest = shortDigest[:15]
	}
	fmt.Printf("Successfully built %s %s:%s\n", shortDigest, name, tag)
	return nil
}

func layerSize(digest string) int64 {
	info, err := os.Stat(image.LayerPath(digest))
	if err != nil {
		return 0
	}
	return info.Size()
}

func snapshotDir(dir string) (map[string]int64, error) {
	snap := make(map[string]int64)
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		info, _ := d.Info()
		if info != nil {
			snap[rel] = info.Size()
		}
		return nil
	})
	return snap, nil
}

func diffSnapshot(before, after map[string]int64) []string {
	var delta []string
	for path, size := range after {
		if beforeSize, ok := before[path]; !ok || beforeSize != size {
			delta = append(delta, path)
		}
	}
	sort.Strings(delta)
	return delta
}
