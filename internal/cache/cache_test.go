package cache

import (
	"strings"
	"testing"
)

func baseInput() KeyInput {
	return KeyInput{
		PrevDigest:  "sha256:aaaa",
		Instruction: "RUN echo hi",
		Workdir:     "/app",
		Env:         []string{"A=1", "B=2"},
		SrcHashes:   []string{"sha256:1 a.txt", "sha256:2 b.txt"},
	}
}

func TestComputeKeyIsDeterministic(t *testing.T) {
	if ComputeKey(baseInput()) != ComputeKey(baseInput()) {
		t.Fatal("same input produced different cache keys")
	}
	if !strings.HasPrefix(ComputeKey(baseInput()), "sha256:") {
		t.Fatal("cache key should be sha256-prefixed")
	}
}

func TestComputeKeyIgnoresEnvAndSourceOrder(t *testing.T) {
	a := baseInput()
	b := baseInput()
	b.Env = []string{"B=2", "A=1"}
	b.SrcHashes = []string{"sha256:2 b.txt", "sha256:1 a.txt"}
	if ComputeKey(a) != ComputeKey(b) {
		t.Fatal("ENV / COPY source ordering must not change the cache key")
	}
}

// Every input that can change a layer's contents must change the key;
// otherwise the cache would serve a stale layer.
func TestComputeKeyChangesWithEachInput(t *testing.T) {
	base := ComputeKey(baseInput())
	mutations := map[string]func(*KeyInput){
		"prev digest": func(k *KeyInput) { k.PrevDigest = "sha256:bbbb" },
		"instruction": func(k *KeyInput) { k.Instruction = "RUN echo bye" },
		"workdir":     func(k *KeyInput) { k.Workdir = "/srv" },
		"env value":   func(k *KeyInput) { k.Env = []string{"A=1", "B=3"} },
		"source file": func(k *KeyInput) { k.SrcHashes = []string{"sha256:1 a.txt", "sha256:9 b.txt"} },
	}
	for name, mutate := range mutations {
		in := baseInput()
		mutate(&in)
		if ComputeKey(in) == base {
			t.Errorf("changing %s did not change the cache key", name)
		}
	}
}

func TestComputeKeyDoesNotReorderCallerSlices(t *testing.T) {
	in := baseInput()
	in.SrcHashes = []string{"z", "a"}
	in.Env = []string{"Z=1", "A=1"}
	ComputeKey(in)
	if in.SrcHashes[0] != "z" || in.Env[0] != "Z=1" {
		t.Fatal("ComputeKey must not mutate the caller's slices")
	}
}

func TestStoreAndLookupRoundTrip(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	if _, ok := Lookup("missing"); ok {
		t.Fatal("empty index should miss")
	}
	if err := Store("k1", "sha256:layer1"); err != nil {
		t.Fatal(err)
	}
	got, ok := Lookup("k1")
	if !ok || got != "sha256:layer1" {
		t.Fatalf("lookup after store: got %q, %v", got, ok)
	}
}
