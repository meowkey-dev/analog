//go:build windows

package main

import "errors"

func reexec(string) error {
	return errors.New("self-upgrade is unavailable on Windows")
}
