package cli

import (
	"testing"
)

func TestResolveVersionPrefersLdflagsValue(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })

	version = "v1.2.3"
	if got := resolveVersion(); got != "v1.2.3" {
		t.Errorf("resolveVersion() = %q, want %q", got, "v1.2.3")
	}
}

func TestResolveVersionFallsBackToDevPlaceholder(t *testing.T) {
	orig := version
	t.Cleanup(func() { version = orig })

	// Under `go test` the build info main version is "(devel)", so an unset
	// ldflags version resolves back to the dev placeholder rather than a tag.
	version = "0.0.0-dev"
	if got := resolveVersion(); got != "0.0.0-dev" {
		t.Errorf("resolveVersion() = %q, want %q", got, "0.0.0-dev")
	}
}
