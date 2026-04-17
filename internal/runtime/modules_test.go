package runtime

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveModulePathSupportsVibePath(t *testing.T) {
	root := t.TempDir()
	libraryRoot := filepath.Join(root, "vibe-lib")
	modulePath := filepath.Join(libraryRoot, "std", "collections.vibe")
	if err := os.MkdirAll(filepath.Dir(modulePath), 0o755); err != nil {
		t.Fatalf("MkdirAll returned error: %v", err)
	}
	if err := os.WriteFile(modulePath, []byte("name = \"collections\"\n"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	t.Setenv("VIBE_PATH", libraryRoot)

	resolved, err := resolveModulePath(filepath.Join(root, "project"), "std/collections")
	if err != nil {
		t.Fatalf("resolveModulePath returned error: %v", err)
	}

	want := modulePath
	if resolved != want {
		t.Fatalf("unexpected resolved path\nwant: %q\ngot:  %q", want, resolved)
	}
}

func TestResolveModulePathSupportsGitHubModules(t *testing.T) {
	resolved, err := resolveModulePath("/workspace", "github.com/example/vibes/std/collections@main")
	if err != nil {
		t.Fatalf("resolveModulePath returned error: %v", err)
	}

	want := "https://raw.githubusercontent.com/example/vibes/main/std/collections.vibe"
	if resolved != want {
		t.Fatalf("unexpected resolved path\nwant: %q\ngot:  %q", want, resolved)
	}
}
