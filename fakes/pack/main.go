package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintf(os.Stdout, "Pack output on stdout\n")
	fmt.Fprintf(os.Stderr, "Pack output on stderr\n")
	fmt.Printf("Arguments: %v\n", os.Args)

	workingDirectory, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	fmt.Printf("PWD: %s\n", workingDirectory)
}
