//go:build darwin || linux

package scheduler

import (
	"reflect"
	"testing"
)

func TestMergeEnv(t *testing.T) {
	base := map[string]string{"PATH": "/usr/bin", "HOME": "/home/x"}
	extra := map[string]string{"PATH": "/custom/bin", "FOO": "bar"}

	merged := mergeEnv(base, extra)

	want := map[string]string{"PATH": "/custom/bin", "HOME": "/home/x", "FOO": "bar"}
	if !reflect.DeepEqual(merged, want) {
		t.Errorf("mergeEnv = %v, want %v", merged, want)
	}
}

func TestMergeEnvDoesNotMutateInputs(t *testing.T) {
	base := map[string]string{"PATH": "/usr/bin"}
	extra := map[string]string{"PATH": "/custom/bin"}

	mergeEnv(base, extra)

	if base["PATH"] != "/usr/bin" {
		t.Errorf("base mutated: PATH = %q, want /usr/bin", base["PATH"])
	}
	if extra["PATH"] != "/custom/bin" {
		t.Errorf("extra mutated: PATH = %q, want /custom/bin", extra["PATH"])
	}
}
