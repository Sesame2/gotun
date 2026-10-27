package main

import (
	"runtime"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

// Wails routes both a window close and its default Quit menu through OnBeforeClose.
// An explicit Quit callback must mark exit intent before that close policy runs.
func (a *App) applicationMenu() *menu.Menu {
	result := menu.NewMenu()
	appMenu := result.AddSubmenu("GoTun")
	show, hide, quit := "显示主界面", "隐藏主界面", "退出 GoTun"
	if a.GetSettings().Language == "en-US" {
		show, hide, quit = "Show main window", "Hide main window", "Quit GoTun"
	}
	appMenu.AddText(show, nil, func(*menu.CallbackData) { a.ShowWindow() })
	appMenu.AddText(hide, keys.CmdOrCtrl("h"), func(*menu.CallbackData) { a.HideWindow() })
	appMenu.AddSeparator()
	appMenu.AddText(quit, keys.CmdOrCtrl("q"), func(*menu.CallbackData) { a.QuitApp() })
	if runtime.GOOS == "darwin" {
		result.Append(menu.EditMenu())
		result.Append(menu.WindowMenu())
	}
	return result
}
