//go:build !windows

package winservice

import (
	"context"
	"errors"
)

func IsServiceProcess() bool                { return false }
func Run(_ func(context.Context) error) error { return errors.New("not windows") }
func InstallAndStart(_ string) error        { return errors.New("not windows") }
func Uninstall() error                      { return errors.New("not windows") }
func ElevateAndRun(_ ...string) error       { return errors.New("not windows") }
func IsInstalled() bool                     { return false }
