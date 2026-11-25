//go:build !linux

package netns

import (
	"context"
	"errors"
	"time"

	"github.com/uri-travoski/MyOVPN/internal/config"
	"github.com/uri-travoski/MyOVPN/internal/core"
)

type Runner struct{}

func NewRunner(_ func(string)) *Runner { return &Runner{} }

func Exists() bool { return false }

func (r *Runner) StartOpenVPN(_ context.Context, _ core.Server, _ time.Duration) error {
	return errors.New("per-app network namespaces are only supported on Linux")
}

func (r *Runner) StopOpenVPN() {}

func (r *Runner) Cleanup(_ context.Context) error { return nil }

func CleanupAll(_ context.Context) error { return nil }

func LaunchAppAsUser(_, _ uint32, _ []string, _ config.AppEntry) error {
	return errors.New("per-app app launching is only supported on Linux")
}
