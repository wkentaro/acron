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
