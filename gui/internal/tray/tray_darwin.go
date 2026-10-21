//go:build darwin && cgo

package tray

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Cocoa
#include <stdlib.h>
int gotun_tray_start(const void *icon, int length);
void gotun_tray_update(const char *json);
void gotun_tray_close(void);
*/
import "C"

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
	"unsafe"
)

//go:embed icon.png
var icon []byte
var callbackMu sync.RWMutex
var callback func(string)

type native struct{}

func New() Native { return &native{} }
func (n *native) Start(handler func(string)) error {
	callbackMu.Lock()
	callback = handler
	callbackMu.Unlock()
	if C.gotun_tray_start(unsafe.Pointer(&icon[0]), C.int(len(icon))) == 0 {
		return fmt.Errorf("could not create macOS status item")
	}
	return nil
}
func (n *native) Update(items []Item) {
	data, err := json.Marshal(items)
	if err != nil {
		return
	}
	value := C.CString(string(data))
	defer C.free(unsafe.Pointer(value))
	C.gotun_tray_update(value)
}
func (n *native) Close() {
	callbackMu.Lock()
	callback = nil
	callbackMu.Unlock()
	C.gotun_tray_close()
}

//export gotunTraySelected
func gotunTraySelected(id *C.char) {
	value := C.GoString(id)
	callbackMu.RLock()
	handler := callback
	callbackMu.RUnlock()
	if handler != nil {
		go handler(value)
	}
}
