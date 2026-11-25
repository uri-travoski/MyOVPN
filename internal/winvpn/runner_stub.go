//go:build !windows

package winvpn

import (
	"context"
	"errors"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/core"
)

type Runner struct{}

func NewRunner(_ func(string)) *Runner { return &Runner{} }

func FindOpenVPN() (string, error) { return "", errors.New("not windows") }

func (r *Runner) StartOpenVPN(_ context.Context, _ core.Server, _ time.Duration) error {
	return errors.New("winvpn is only supported on Windows")
}

func (r *Runner) StopOpenVPN() {}
