package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveEditor(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := resolveEditor(); got != "vi" {
		t.Errorf("default = %q, want vi", got)
	}
	t.Setenv("EDITOR", "nano")
	if got := resolveEditor(); got != "nano" {
		t.Errorf("EDITOR = %q, want nano", got)
	}
	t.Setenv("VISUAL", "code --wait")
	if got := resolveEditor(); got != "code --wait" {
		t.Errorf("VISUAL precedence = %q, want code --wait", got)
	}
}

func validConfig(cwd string) string {
	return "[[job]]\n" +
		"name = \"x\"\n" +
		"schedule = \"0 2 * * *\"\n" +
		"agent = [\"claude\"]\n" +
		"prompt = \"hi\"\n" +
		"cwd = \"" + cwd + "\"\n"
}

// editorWriting points $EDITOR at `cp <source>`, so opening a file copies
// source over it, simulating a save.
func editorWriting(t *testing.T, content string) string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "edit.toml")
	if err := os.WriteFile(src, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return "cp " + src
}

func withStdin(t *testing.T, input string) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = f
	t.Cleanup(func() {
		os.Stdin = old
		_ = f.Close()
	})
}

func setupEditTest(t *testing.T) (dir, path string) {
	t.Helper()
	dir = t.TempDir()
	path = filepath.Join(dir, "config.toml")
	t.Setenv("ACRON_CONFIG", path)
	return dir, path
}

func TestRunEditWritesValidConfig(t *testing.T) {
	dir, path := setupEditTest(t)

	want := validConfig(dir)
	t.Setenv("EDITOR", editorWriting(t, want))

	if err := runEdit(); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("config = %q, want %q", got, want)
	}
}

func TestRunEditNoChangesKeepsFile(t *testing.T) {
	dir, path := setupEditTest(t)

	original := validConfig(dir)
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", "true")

	if err := runEdit(); err != nil {
		t.Fatalf("runEdit: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("config = %q, want unchanged %q", got, original)
	}
}

func TestRunEditAbortKeepsOriginal(t *testing.T) {
	dir, path := setupEditTest(t)

	original := validConfig(dir)
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editorWriting(t, "schedule = bogus"))
	withStdin(t, "n\n")

	if err := runEdit(); err == nil {
		t.Fatal("expected abort error, got nil")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("config = %q, want original preserved %q", got, original)
	}
}

func TestRunEditRetriesThenSave(t *testing.T) {
	// The retry prompt must be answerable repeatedly from one stdin stream; a
	// fresh scanner per prompt would drop buffered input on the second answer.
	for _, tc := range []struct {
		name    string
		retries int
		stdin   string
	}{
		{"one retry", 1, "y\n"},
		{"two retries", 2, "y\ny\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir, path := setupEditTest(t)

			valid := validConfig(dir)
			validSrc := filepath.Join(t.TempDir(), "valid.toml")
			if err := os.WriteFile(validSrc, []byte(valid), 0o644); err != nil {
				t.Fatal(err)
			}
			invalidSrc := filepath.Join(t.TempDir(), "invalid.toml")
			if err := os.WriteFile(invalidSrc, []byte("name = oops"), 0o644); err != nil {
				t.Fatal(err)
			}

			// A counting editor: invalid for the first tc.retries opens, valid
			// after, so the retry prompt is reached exactly tc.retries times.
			editor := filepath.Join(t.TempDir(), "editor.sh")
			script := fmt.Sprintf("#!/bin/sh\n"+
				"target=\"$1\"; count=\"$target.count\"\n"+
				"n=$(cat \"$count\" 2>/dev/null || echo 0); n=$((n + 1)); echo \"$n\" > \"$count\"\n"+
				"if [ \"$n\" -gt %d ]; then cp %q \"$target\"\n"+
				"else cp %q \"$target\"\n"+
				"fi\n", tc.retries, validSrc, invalidSrc)
			if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
			t.Setenv("EDITOR", editor)
			withStdin(t, tc.stdin)

			if err := runEdit(); err != nil {
				t.Fatalf("runEdit: %v", err)
			}
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != valid {
				t.Errorf("config = %q, want valid %q", got, valid)
			}
		})
	}
}

func TestRunEditSeedsTemplateWhenNoConfig(t *testing.T) {
	_, path := setupEditTest(t)

	// An editor that copies the buffer out so we can inspect what was seeded,
	// then leaves it unchanged.
	seen := filepath.Join(t.TempDir(), "seen.toml")
	editor := filepath.Join(t.TempDir(), "capture.sh")
	if err := os.WriteFile(editor, []byte("#!/bin/sh\ncp \"$1\" \""+seen+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)

	if err := runEdit(); err != nil {
		t.Fatalf("runEdit: %v", err)
	}

	buf, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(buf), "[[job]]") {
		t.Errorf("buffer not seeded with template: %q", buf)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("unchanged template should write no config, stat err = %v", err)
	}
}

func TestRunEditDoesNotEvalEditorAsShellCode(t *testing.T) {
	dir, path := setupEditTest(t)
	original := validConfig(dir)
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// If the editor value were interpolated into the shell command, the
	// "; touch <canary>" would run. The safe invocation must not evaluate it.
	canary := filepath.Join(t.TempDir(), "canary")
	t.Setenv("EDITOR", "true; touch "+canary)

	_ = runEdit()

	if _, err := os.Stat(canary); err == nil {
		t.Fatal("editor value was evaluated as shell code (injection)")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Errorf("config modified: %q", got)
	}
}
