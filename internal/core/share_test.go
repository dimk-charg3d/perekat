package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSafeNameStaysInsideDownloads(t *testing.T) {
	cases := map[string]string{
		"photo.jpg":              "photo.jpg",
		"../../.ssh/id_rsa":      "id_rsa",
		"/etc/passwd":            "passwd",
		`..\..\Windows\evil.exe`: "evil.exe",
		".bashrc":                "bashrc",
		"..":                     "file",
		"":                       "file",
		"a:b?.txt":               "a_b_.txt",
	}
	for in, want := range cases {
		if got := safeName(in); got != want {
			t.Errorf("safeName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClaimNumbersDuplicates(t *testing.T) {
	dir := t.TempDir()
	var got []string
	for range 3 {
		part := filepath.Join(dir, ".part")
		if err := os.WriteFile(part, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		path, err := claim(part, dir, "note.txt")
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, filepath.Base(path))
	}
	want := []string{"note.txt", "note (1).txt", "note (2).txt"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
