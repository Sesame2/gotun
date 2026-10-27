package tray

import (
	_ "embed"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/getlantern/systray"
)

//go:embed icon.ico
var icon []byte

type native struct {
	mu      sync.Mutex
	slots   []*menuSlot
	handler func(string)
	done    chan struct{}
	once    sync.Once
}
type menuSlot struct {
	item     *systray.MenuItem
	action   string
	children []*menuSlot
}

func New() Native { return &native{done: make(chan struct{})} }
func (n *native) Start(handler func(string)) error {
	n.handler = handler
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		systray.Run(func() { systray.SetIcon(icon); systray.SetTooltip("GoTun"); close(ready) }, nil)
	}()
	select {
	case <-ready:
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("Windows tray initialization timed out")
	}
}

// Slots are reused by position, including deleted/renamed profile entries.
// This avoids retaining a goroutine and native menu for every configuration edit.
func (n *native) apply(slots []*menuSlot, items []Item, parent *systray.MenuItem) []*menuSlot {
	for i, entry := range items {
		if i >= len(slots) {
			var item *systray.MenuItem
			if parent == nil {
				item = systray.AddMenuItem(entry.Label, "")
			} else {
				item = parent.AddSubMenuItem(entry.Label, "")
			}
			slot := &menuSlot{item: item}
			slots = append(slots, slot)
			go func() {
				for {
					select {
					case <-n.done:
						return
					case <-slot.item.ClickedCh:
						n.mu.Lock()
						action := slot.action
						n.mu.Unlock()
						if action != "" {
							n.handler(action)
						}
					}
				}
			}()
		}
		slot := slots[i]
		slot.action = entry.ID
		slot.item.SetTitle(entry.Label)
		slot.item.Show()
		if entry.Disabled || entry.Separator {
			slot.item.Disable()
		} else {
			slot.item.Enable()
		}
		if entry.Checked {
			slot.item.Check()
		} else {
			slot.item.Uncheck()
		}
		slot.children = n.apply(slot.children, entry.Children, slot.item)
	}
	for _, slot := range slots[len(items):] {
		slot.action = ""
		slot.item.Hide()
	}
	return slots
}
func (n *native) Update(items []Item) {
	n.mu.Lock()
	defer n.mu.Unlock()
	select {
	case <-n.done:
		return
	default:
	}
	n.slots = n.apply(n.slots, items, nil)
}
func (n *native) Close() { n.once.Do(func() { close(n.done); systray.Quit() }) }
