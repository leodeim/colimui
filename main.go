package main

import (
	"os"

	"github.com/leodeim/colimui/internal/app"
)

func main() {
	os.Exit(app.Run(os.Args[1:]))
}
