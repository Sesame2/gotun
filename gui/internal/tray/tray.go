// Package tray adapts a shared menu to the desktop notification area.
package tray

// Item is a native menu entry. IDs are stable action names, never credentials.
type Item struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Checked   bool   `json:"checked,omitempty"`
	Disabled  bool   `json:"disabled,omitempty"`
	Separator bool   `json:"separator,omitempty"`
	Children  []Item `json:"children,omitempty"`
}

type Native interface {
	Start(func(string)) error
	Update([]Item)
	Close()
}
