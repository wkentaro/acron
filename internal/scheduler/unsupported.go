//go:build !darwin

package scheduler

import (
	"errors"

	"github.com/wkentaro/acron/internal/config"
)

var errUnsupported = errors.New("acron currently supports macOS (launchd) only; systemd support is not implemented yet")

func Apply(_ *config.Config, _ bool) (*Plan, error) {
	return nil, errUnsupported
}

func Destroy() (*Plan, error) {
	return nil, errUnsupported
}
