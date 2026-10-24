//go:build windows

package main

import (
	"fmt"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/windows"
)

// writeFileAtomic replaces path in one step: data goes to a temp file in the
// same folder (unique per process, since several NexDesk windows share these
// files), is flushed, then renamed over the target. A crash or a concurrent
// writer can no longer leave a half-written settings file behind.
func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := fmt.Sprintf("%s.%d.%d.tmp", path, os.Getpid(), time.Now().UnixNano())
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		// Another window may hold the file open for a moment; retry briefly.
		for i := 0; i < 10; i++ {
			if err = os.Rename(tmp, path); err == nil {
				return nil
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	_ = os.Remove(tmp)
	return err
}

var (
	deviceMutexMu sync.Mutex
	deviceMutex   windows.Handle
)

// acquireDeviceRole makes sure only one NexDesk process on this Windows
// session announces the device id; a second window would otherwise keep
// evicting the first on the server.
func acquireDeviceRole() bool {
	deviceMutexMu.Lock()
	defer deviceMutexMu.Unlock()
	if deviceMutex != 0 {
		return true
	}
	name, _ := windows.UTF16PtrFromString(`Local\NexDesk-DeviceAccess`)
	h, err := windows.CreateMutex(nil, false, name)
	if h == 0 {
		return true // cannot tell; behave as before
	}
	if err == windows.ERROR_ALREADY_EXISTS {
		_ = windows.CloseHandle(h)
		return false
	}
	deviceMutex = h
	return true
}

func releaseDeviceRole() {
	deviceMutexMu.Lock()
	defer deviceMutexMu.Unlock()
	if deviceMutex != 0 {
		_ = windows.CloseHandle(deviceMutex)
		deviceMutex = 0
	}
}
