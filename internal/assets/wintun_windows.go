//go:build windows

package assets

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

//go:embed dll/*.dll
var wintunFS embed.FS

var setupOnce sync.Once

// SetupWintun 提取并释放 wintun.dll 到当前目录
func SetupWintun() error {
	var err error
	setupOnce.Do(func() {
		// 1. 确定目标 DLL 文件名 (原始文件名)
		var dllName string
		switch runtime.GOARCH {
		case "amd64":
			dllName = "wintun_amd64.dll"
		case "arm64":
			dllName = "wintun_arm64.dll"
		default:
			// 不支持架构，直接返回 (让 wireguard-go 自行决定是否报错)
			return
		}

		// 2. 确定目标释放路径 (当前可执行文件目录)
		exePath, e := os.Executable()
		if e != nil {
			err = fmt.Errorf("无法获取可执行文件路径: %v", e)
			return
		}
		exeDir := filepath.Dir(exePath)
		targetPath := filepath.Join(exeDir, "wintun.dll") // 统一重命名为 wintun.dll

		// 3. 检查文件是否已存在 (如果存在则跳过，避免覆盖)
		if _, e := os.Stat(targetPath); e == nil {
			return
		}

		// 4. 从 embed FS 读取
		srcPath := "dll/" + dllName
		data, e := wintunFS.ReadFile(srcPath)
		if e != nil {
			err = fmt.Errorf("读取嵌入资源失败 %s: %v", srcPath, e)
			return
		}

		// 5. 写入磁盘
		if e := os.WriteFile(targetPath, data, 0755); e != nil {
			err = fmt.Errorf("释放 wintun.dll 失败: %v", e)
			return
		}
	})
	return err
}
