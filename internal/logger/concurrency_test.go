package logger

import (
	"path/filepath"
	"sync"
	"testing"
)

func TestConcurrentLoggerConfiguration(t *testing.T) {
	log := NewLogger(true)
	defer log.Close()
	path := filepath.Join(t.TempDir(), "audit.log")
	var workers sync.WaitGroup
	for worker := 0; worker < 3; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for i := 0; i < 20; i++ {
				switch worker {
				case 0:
					log.Debugf("entry %d", i)
				case 1:
					log.SetLevel(LevelInfo)
					log.SetCallback(func(string, string) { _ = log.GetCallback() })
				case 2:
					if err := log.SetLogFile(path); err != nil {
						t.Error(err)
					}
				}
			}
		}(worker)
	}
	workers.Wait()
}
