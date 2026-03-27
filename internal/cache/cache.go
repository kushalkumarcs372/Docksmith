package cache

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type Index map[string]string // cacheKey -> layerDigest

func indexPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".docksmith", "cache", "index.json")
}

func load() (Index, error) {
	data, err := os.ReadFile(indexPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Index{}, nil
		}
		return nil, err
	}
	var idx Index
	return idx, json.Unmarshal(data, &idx)
}

func save(idx Index) error {
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(indexPath(), data, 0644)
}

func Lookup(key string) (string, bool) {
	idx, err := load()
	if err != nil {
		return "", false
	}
	digest, ok := idx[key]
	return digest, ok
}

func Store(key, digest string) error {
	idx, err := load()
	if err != nil {
		return err
	}
	idx[key] = digest
	return save(idx)
}

type KeyInput struct {
	PrevDigest  string
	Instruction string
	Workdir     string
	Env         []string   // all accumulated key=value pairs
	SrcHashes   []string   // COPY only: sorted sha256:path entries
}

func ComputeKey(in KeyInput) string {
	h := sha256.New()

	h.Write([]byte(in.PrevDigest))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.Instruction))
	h.Write([]byte("\x00"))
	h.Write([]byte(in.Workdir))
	h.Write([]byte("\x00"))

	sorted := make([]string, len(in.Env))
	copy(sorted, in.Env)
	sort.Strings(sorted)
	h.Write([]byte(strings.Join(sorted, "\n")))
	h.Write([]byte("\x00"))

	sort.Strings(in.SrcHashes)
	h.Write([]byte(strings.Join(in.SrcHashes, "\n")))

	return fmt.Sprintf("sha256:%x", h.Sum(nil))
}
