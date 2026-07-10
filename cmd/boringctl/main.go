package main

import (
	"fmt"
	"os"

	"github.com/boring-dragon/boringctl/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
