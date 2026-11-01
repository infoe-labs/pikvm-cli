package main

import (
	"github.com/infoe-labs/pikvm-cli/internal/entrypoint"
	"os"
)

var version = "dev"

func main() { os.Exit(entrypoint.Run(version)) }
