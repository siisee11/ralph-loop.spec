package main

import (
	"os"

	"github.com/your-org/your-repo/internal/ralphloop"
)

func main() {
	cwd, err := os.Getwd()
	if err != nil {
		_, _ = os.Stderr.WriteString(err.Error() + "\n")
		os.Exit(1)
	}
	os.Exit(ralphloop.Run(os.Args[1:], cwd, os.Stdout, os.Stderr))
}
