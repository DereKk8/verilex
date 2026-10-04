package main

import (
	"os"

	"github.com/DereKk8/verilex/internal/cli"
)

func main() { os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr)) }
