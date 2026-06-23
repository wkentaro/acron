package cli

import (
	"os"
	"strings"
	"testing"
)

func TestRunConfigShowPrintsConfig(t *testing.T) {
	dir, path := setupEditTest(t)
	content := validConfig(dir)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	out, err := captureStdout(t, runConfigShow)
	if err != nil {
		t.Fatalf("runConfigShow: %v", err)
	}
	if out != content {
		t.Errorf("output = %q, want %q", out, content)
	}
}

func TestRunConfigShowErrorsWhenMissing(t *testing.T) {
	_, path := setupEditTest(t)

	out, err := captureStdout(t, runConfigShow)
	if err == nil {
		t.Fatal("expected error for missing config, got nil")
	}
	if out != "" {
		t.Errorf("expected no output, got %q", out)
	}
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error %q should mention path %q", err, path)
	}
	if !strings.Contains(err.Error(), "acron config edit") {
		t.Errorf("error %q should hint at \"acron config edit\"", err)
	}
}
