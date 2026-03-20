package daemon

import (
	"fmt"
	"sync"
	"time"
)

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinner struct {
	mu      sync.Mutex
	running bool
	done    chan struct{}
}

func newSpinner() *spinner {
	return &spinner{done: make(chan struct{})}
}

func (s *spinner) Start(label string) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.done = make(chan struct{})
	s.mu.Unlock()

	go func() {
		i := 0
		for {
			select {
			case <-s.done:
				fmt.Print("\r\033[K")
				return
			default:
				fmt.Printf("\r\033[2m%s %s\033[0m", spinnerFrames[i%len(spinnerFrames)], label)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

func (s *spinner) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	s.running = false
	close(s.done)
}
