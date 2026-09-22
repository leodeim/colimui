//go:build !darwin || !cgo

package main

import "errors"

func runMenubar(Backend) error {
	return errors.New("the menubar requires a cgo-enabled macOS build")
}
