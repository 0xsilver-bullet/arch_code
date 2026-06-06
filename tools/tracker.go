package tools

import (
	"path/filepath"
	"sync"
	"time"
)

var (
	readTimesMu sync.RWMutex
	readTimes   = map[string]time.Time{}
)

func canonPath(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func RecordRead(path string) {
	readTimesMu.Lock()
	defer readTimesMu.Unlock()
	readTimes[canonPath(path)] = time.Now()
}

func LastReadTime(path string) time.Time {
	readTimesMu.RLock()
	defer readTimesMu.RUnlock()
	return readTimes[canonPath(path)]
}
