//go:build !linux

package power

import "context"

func WatchResumeAndNetwork(_ context.Context, _ func(string)) {}
