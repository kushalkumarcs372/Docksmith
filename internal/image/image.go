package image

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type LayerMeta struct {
	Digest    string `json:"digest"`
	Size      int64  `json:"size"`
	CreatedBy string `json:"createdBy"`
}

type Config struct {
	Env        []string `json:"Env"`
	Cmd        []string `json:"Cmd"`
	WorkingDir string   `json:"WorkingDir"`
}

type Manifest struct {
	Name    string      `json:"name"`
	Tag     string      `json:"tag"`
	Digest  string      `json:"digest"`
	Created string      `json:"created"`
	Config  Config      `json:"config"`
	Layers  []LayerMeta `json:"layers"`
}

func DocksmithDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".docksmith")
}

func ImagesDir() string  { return filepath.Join(DocksmithDir(), "images") }
func LayersDir() string  { return filepath.Join(DocksmithDir(), "layers") }
func CacheDir() string   { return filepath.Join(DocksmithDir(), "cache") }

func ManifestPath(name, tag string) string {
	return filepath.Join(ImagesDir(), fmt.Sprintf("%s:%s.json", name, tag))
}

func LayerPath(digest string) string {
	return filepath.Join(LayersDir(), digest)
}

func Load(name, tag string) (*Manifest, error) {
	data, err := os.ReadFile(ManifestPath(name, tag))
	if err != nil {
		return nil, fmt.Errorf("image %s:%s not found in local store", name, tag)
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

func Save(m *Manifest) error {
	// Compute digest: serialize with digest="" then hash
	tmp := *m
	tmp.Digest = ""
	canonical, err := json.Marshal(tmp)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(canonical)
	m.Digest = fmt.Sprintf("sha256:%x", sum)

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(ManifestPath(m.Name, m.Tag), data, 0644)
}

func NewManifest(name, tag string) *Manifest {
	return &Manifest{
		Name:    name,
		Tag:     tag,
		Created: time.Now().UTC().Format(time.RFC3339),
		Config:  Config{Env: []string{}},
	}
}
