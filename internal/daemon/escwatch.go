package daemon

import (
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

const (
	escByte         = 27
	doubleEscWindow = 500 * time.Millisecond
)

type escWatcher struct {
	mu       sync.Mutex
	cancel   func()
	watching bool
}

func newEscWatcher(cancelFn func()) *escWatcher {
	return &escWatcher{cancel: cancelFn}
}

func (w *escWatcher) Start() {
	w.mu.Lock()
	if w.watching {
		w.mu.Unlock()
		return
	}
	w.watching = true
	w.mu.Unlock()

	go w.watch()
}

func (w *escWatcher) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.watching = false
}

func (w *escWatcher) watch() {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return
	}

	// Save and modify termios: disable ICANON+ECHO for char-by-char input
	// but keep OPOST so output newlines still produce \r\n.
	oldTermios, err := unix.IoctlGetTermios(fd, unix.TIOCGETA)
	if err != nil {
		return
	}
	raw := *oldTermios
	raw.Lflag &^= unix.ICANON | unix.ECHO
	raw.Cc[unix.VMIN] = 0
	raw.Cc[unix.VTIME] = 1
	if err := unix.IoctlSetTermios(fd, unix.TIOCSETA, &raw); err != nil {
		return
	}
	defer unix.IoctlSetTermios(fd, unix.TIOCSETA, oldTermios)

	var lastEsc time.Time
	buf := make([]byte, 1)

	for {
		w.mu.Lock()
		active := w.watching
		w.mu.Unlock()
		if !active {
			return
		}

		n, err := os.Stdin.Read(buf)
		if n == 0 || err != nil {
			time.Sleep(50 * time.Millisecond)
			continue
		}

		if buf[0] != escByte {
			lastEsc = time.Time{}
			continue
		}

		now := time.Now()
		if !lastEsc.IsZero() && now.Sub(lastEsc) < doubleEscWindow {
			w.cancel()
			return
		}
		lastEsc = now
	}
}
