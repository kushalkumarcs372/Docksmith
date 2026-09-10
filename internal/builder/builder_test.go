package builder

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/kushal/docksmith/internal/image"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestParseFileSkipsCommentsAndBlankLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Docksmithfile")
	writeFile(t, p, "# comment\n\nFROM alpine:3.18\nworkdir /app\nCMD [\"/bin/sh\"]\n")
	got, err := parseFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 || got[0].Op != "FROM" || got[1].Op != "WORKDIR" || got[1].Args != "/app" {
		t.Fatalf("unexpected parse result: %+v", got)
	}
}

func TestParseFileRejectsUnknownInstruction(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "Docksmithfile")
	writeFile(t, p, "FROM alpine:3.18\nEXPOSE 80\n")
	if _, err := parseFile(p); err == nil {
		t.Fatal("expected an error for unsupported instruction EXPOSE")
	}
}

func TestMatchesPattern(t *testing.T) {
	cases := []struct {
		pattern, rel string
		want         bool
	}{
		{"app.sh", "app.sh", true},
		{"*.sh", "app.sh", true},
		{"*.sh", "scripts/run.sh", true}, // matched against the base name
		{"*.sh", "app.py", false},
		{"src/**", "src/a/b.go", true},
		{"src/**", "lib/b.go", false},
		{"**/*.go", "cmd/main.go", true},
		{"**/*.go", "README.md", false},
	}
	for _, c := range cases {
		if got := matchesPattern(c.pattern, c.rel); got != c.want {
			t.Errorf("matchesPattern(%q, %q) = %v, want %v", c.pattern, c.rel, got, c.want)
		}
	}
}

func TestDiffSnapshotReturnsAddedAndChangedFilesSorted(t *testing.T) {
	before := map[string]int64{"a": 1, "b": 2, "gone": 3}
	after := map[string]int64{"a": 1, "b": 99, "c": 4}
	got := diffSnapshot(before, after)
	want := []string{"b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("diffSnapshot = %v, want %v", got, want)
	}
}

// Two layers built from identical file contents must have identical digests
// even when file modification times differ -- that is what makes builds
// reproducible and cache hits possible across machines.
func TestCreateLayerIsReproducibleAcrossMtimes(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if err := os.MkdirAll(image.LayersDir(), 0755); err != nil {
		t.Fatal(err)
	}

	build := func(mtime time.Time) string {
		src := t.TempDir()
		for name, body := range map[string]string{"app.sh": "echo hi\n", "conf/x.cfg": "k=v\n"} {
			p := filepath.Join(src, name)
			writeFile(t, p, body)
			if err := os.Chtimes(p, mtime, mtime); err != nil {
				t.Fatal(err)
			}
		}
		digest, _, err := createLayer(src, []string{"conf/x.cfg", "app.sh"})
		if err != nil {
			t.Fatal(err)
		}
		return digest
	}

	d1 := build(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC))
	d2 := build(time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC))
	if d1 != d2 {
		t.Fatalf("layer digests differ across mtimes: %s vs %s", d1, d2)
	}
	if _, err := os.Stat(image.LayerPath(d1)); err != nil {
		t.Fatalf("layer file not written to the content-addressed store: %v", err)
	}
}
