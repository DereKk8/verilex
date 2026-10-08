package main

import (
	"os"

	"github.com/DereKk8/verilex/agent/internal/cli"
	"github.com/DereKk8/verilex/agent/internal/run"
)

func main() { os.Exit(cli.Main(os.Args[1:], os.Stdout, os.Stderr, run.WallClock)) }
