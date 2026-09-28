//go:build linux || darwin

package main

import (
	"os"
	"syscall"
)

func reexec(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}
