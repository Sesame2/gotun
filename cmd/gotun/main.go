package main

import (
	"github.com/Sesame2/gotun/cmd/gotun/cli"
)

var (
	Version = "dev" // 由构建时的ldflags填充
)

func main() {
	cli.Execute(Version)
}
