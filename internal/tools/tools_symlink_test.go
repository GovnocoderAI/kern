package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestSymlinkEscape verifies that a symlink pointing outside the workspace is
// rejected for writes. On POSIX this uses os.Symlink; on Windows it is
// skipped (junctions require elevated privileges).
func TestSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink escape test requires POSIX")
	}
	dir := t.TempDir()
	outside := t.TempDir()
	link := filepath.Join(dir, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("cannot create symlink: %v", err)
	}

	r := NewRegistry(dir, "")
	ctx := context.Background()

	// Writing through the symlink to a new file outside must be rejected.
	out, err := r.Run(ctx, "write", json.RawMessage(`{"path":"link/evil.txt","content":"x"}`))
	t.Logf("write through symlink: out=%q err=%v", out, err)
	if err == nil {
		t.Fatal("write through symlink pointing outside should fail")
	}

	// A read of a file that exists at the symlink target should also be
	// rejected, because the resolved path leaves the workspace.
	target := filepath.Join(outside, "secret.txt")
	if werr := os.WriteFile(target, []byte("top secret\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	out, err = r.Run(ctx, "read", json.RawMessage(`{"path":"link/secret.txt"}`))
	t.Logf("read through symlink: out=%q err=%v", out, err)
	if err == nil {
		t.Fatal("read through symlink pointing outside should fail")
	}

	// A path that stays inside the workspace must still work.
	inner := filepath.Join(dir, "ok.txt")
	if werr := os.WriteFile(inner, []byte("fine\n"), 0o644); werr != nil {
		t.Fatal(werr)
	}
	out, err = r.Run(ctx, "read", json.RawMessage(`{"path":"ok.txt"}`))
	if err != nil {
		t.Fatalf("read of an in-workspace file: %v", err)
	}
	if out != "fine\n" {
		t.Fatalf("read = %q", out)
	}
}
