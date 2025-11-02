package main

import (
	"runtime/debug"

	"github.com/Sesame2/gotun/cmd/gotun/cli"
)

func main() {
	version := "dev" // 默认值

	// 尝试从构建信息中读取版本
	if info, ok := debug.ReadBuildInfo(); ok {
		if info.Main.Version != "" && info.Main.Version != "(devel)" {
			version = info.Main.Version
		}
	}

	cli.Execute(version)
}
