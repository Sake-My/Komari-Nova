package public

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
)

type distArchiveEntry struct {
	name     string
	body     string
	typeflag byte
}

func buildTestDistArchive(t *testing.T, entries ...distArchiveEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := tar.NewWriter(&buffer)
	for _, entry := range entries {
		typeflag := entry.typeflag
		if typeflag == 0 {
			typeflag = tar.TypeReg
		}
		header := &tar.Header{Name: entry.name, Mode: 0o644, Typeflag: typeflag}
		if typeflag == tar.TypeReg {
			header.Size = int64(len(entry.body))
		}
		if err := writer.WriteHeader(header); err != nil {
			t.Fatalf("write tar header: %v", err)
		}
		if header.Size > 0 {
			if _, err := writer.Write([]byte(entry.body)); err != nil {
				t.Fatalf("write tar body: %v", err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	encoder, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1))
	if err != nil {
		t.Fatalf("create encoder: %v", err)
	}
	defer encoder.Close()
	return encoder.EncodeAll(buffer.Bytes(), nil)
}

func TestExtractDistArchiveReplacesOnlyDistCache(t *testing.T) {
	cacheDir := t.TempDir()
	targetDir := filepath.Join(cacheDir, "dist")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"dist/stale.js", "keep.txt"} {
		if err := os.WriteFile(filepath.Join(cacheDir, filepath.FromSlash(name)), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	archive := buildTestDistArchive(t,
		distArchiveEntry{name: "./", typeflag: tar.TypeDir},
		distArchiveEntry{name: "./index.html", body: "index"},
		distArchiveEntry{name: "./assets/app.js", body: "app"},
	)
	if err := extractDistArchive(archive, targetDir); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"dist/index.html": "index", "dist/assets/app.js": "app", "keep.txt": "keep"} {
		got, err := os.ReadFile(filepath.Join(cacheDir, filepath.FromSlash(name)))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v, want %q", name, got, err, want)
		}
	}
	if _, err := os.Stat(filepath.Join(targetDir, "stale.js")); !os.IsNotExist(err) {
		t.Fatalf("stale asset was not removed: %v", err)
	}
}

func TestExtractDistArchiveRejectsInvalidEntries(t *testing.T) {
	tests := map[string][]distArchiveEntry{
		"parent traversal": {{name: "../outside.txt", body: "escape"}},
		"absolute path":    {{name: "/outside.txt", body: "escape"}},
		"windows drive":    {{name: "C:/outside.txt", body: "escape"}},
		"windows stream":   {{name: "index.html:stream", body: "escape"}},
		"backslash escape": {{name: "..\\outside.txt", body: "escape"}},
		"symlink":          {{name: "link", typeflag: tar.TypeSymlink}},
		"hardlink":         {{name: "link", typeflag: tar.TypeLink}},
		"duplicate":        {{name: "index.html", body: "first"}, {name: "./index.html", body: "second"}},
		"missing index":    {{name: "assets/app.js", body: "app"}},
		"directory index":  {{name: "index.html", typeflag: tar.TypeDir}},
	}
	for name, entries := range tests {
		t.Run(name, func(t *testing.T) {
			cacheDir := t.TempDir()
			sentinel := filepath.Join(cacheDir, "outside.txt")
			if err := os.WriteFile(sentinel, []byte("untouched"), 0o644); err != nil {
				t.Fatal(err)
			}
			err := extractDistArchive(buildTestDistArchive(t, entries...), filepath.Join(cacheDir, "dist"))
			if err == nil {
				t.Fatal("invalid archive was accepted")
			}
			got, readErr := os.ReadFile(sentinel)
			if readErr != nil || string(got) != "untouched" {
				t.Fatalf("outside file changed: %q, %v", got, readErr)
			}
		})
	}
}

func TestExtractDistArchiveRejectsUnsafeCacheDirectory(t *testing.T) {
	cacheDir := t.TempDir()
	sentinel := filepath.Join(cacheDir, "keep.txt")
	if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	archive := buildTestDistArchive(t, distArchiveEntry{name: "index.html", body: "index"})
	if err := extractDistArchive(archive, cacheDir); err == nil || !strings.Contains(err.Error(), "invalid embedded dist cache directory") {
		t.Fatalf("unexpected result for non-dist directory: %v", err)
	}
	if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep" {
		t.Fatalf("cache parent was modified: %q, %v", got, err)
	}
}

func TestExtractDistArchiveRejectsSymlinkCache(t *testing.T) {
	for _, linkParent := range []bool{false, true} {
		name := "target"
		if linkParent {
			name = "parent"
		}
		t.Run(name, func(t *testing.T) {
			rootDir := t.TempDir()
			externalDir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(externalDir, "dist"), 0o755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(externalDir, "dist", "keep.txt")
			if err := os.WriteFile(sentinel, []byte("keep"), 0o644); err != nil {
				t.Fatal(err)
			}
			linkPath := filepath.Join(rootDir, "dist")
			linkTarget := filepath.Join(externalDir, "dist")
			targetDir := linkPath
			if linkParent {
				linkPath = filepath.Join(rootDir, "cache")
				linkTarget = externalDir
				targetDir = filepath.Join(linkPath, "dist")
			}
			if err := os.Symlink(linkTarget, linkPath); err != nil {
				t.Skipf("symbolic links are unavailable: %v", err)
			}
			archive := buildTestDistArchive(t, distArchiveEntry{name: "index.html", body: "index"})
			if err := extractDistArchive(archive, targetDir); err == nil {
				t.Fatal("symlink cache was accepted")
			}
			if got, err := os.ReadFile(sentinel); err != nil || string(got) != "keep" {
				t.Fatalf("external cache was modified: %q, %v", got, err)
			}
		})
	}
}
