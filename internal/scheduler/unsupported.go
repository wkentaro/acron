//go:build !darwin && !linux

package scheduler

import (
	"errors"

	"github.com/wkentaro/acron/internal/config"
)

var errUnsupported = errors.New("acron supports macOS (launchd) and Linux (systemd --user) only")

func Apply(_ *config.Config, _ bool) (*Plan, error) {
	return nil, errUnsupported
}

func Destroy() (*Plan, error) {
	return nil, errUnsupported
}

func ApplyStates(_ *config.Config) ([]JobState, error) {
	return nil, errUnsupported
}

func Show(_ *config.Config, _ string) (*JobUnits, error) {
	return nil, errUnsupported
}

func Trigger(_ string) error {
	return errUnsupported
}
