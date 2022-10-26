package main

import (
	"fmt"
	"os"

	"github.com/timflannagan/deppy/cmd/root"
)

func main() {
	rootCmd := root.NewRootCmd()
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
