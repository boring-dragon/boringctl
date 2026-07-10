package main

import (
	"fmt"
	"os"

	"github.com/boring-labs/boringctl/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		if err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
