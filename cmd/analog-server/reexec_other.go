//go:build !linux && !darwin

package main

import "errors"

func reexec(string) error {
	return errors.New("self-upgrade is unavailable on Windows")
}
