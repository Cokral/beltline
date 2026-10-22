package main

import (
	"os"

	"github.com/robbertmanders/beltline/daemon/internal/daemon"
)

func main() {
	os.Exit(daemon.Main(os.Args[1:]))
}
