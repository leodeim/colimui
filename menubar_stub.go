//go:build !darwin || !cgo

package main

import "errors"

// menubarSupported gates the menu bar toggle to builds that can run it.
const menubarSupported = false

func runMenubar(Backend) error {
	return errors.New("the menubar requires a cgo-enabled macOS build")
}
