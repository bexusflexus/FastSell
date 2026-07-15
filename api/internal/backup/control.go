package backup

import (
	"context"
	"errors"
	"net/http"
	"os"
	"sync"
	"syscall"
)

var ErrOperationConflict = errors.New("another backup, restore, or media operation is already running")

type OperationLock struct {
	mu        sync.Mutex
	path      string
	operation string
	file      *os.File
}

func NewOperationLock(path string) *OperationLock { return &OperationLock{path: path} }

func (l *OperationLock) TryAcquire(operation string) (func(), error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.operation != "" || l.file != nil {
		return nil, ErrOperationConflict
	}
	file, err := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, errors.New("failed to open shared backup operation lock")
	}
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		_ = file.Close()
		return nil, errors.New("shared backup operation lock is not a regular file")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, ErrOperationConflict
		}
		return nil, errors.New("failed to acquire shared backup operation lock")
	}
	if err := file.Chmod(0600); err != nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		_ = file.Close()
		return nil, errors.New("failed to secure shared backup operation lock")
	}
	l.operation = operation
	l.file = file
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			file := l.file
			l.file = nil
			l.operation = ""
			l.mu.Unlock()
			if file != nil {
				_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
				_ = file.Close()
			}
		})
	}, nil
}

func (l *OperationLock) Current() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.operation
}

type MaintenanceGate struct {
	mu           sync.Mutex
	maintenance  bool
	activeWrites int
	zero         chan struct{}
}

func NewMaintenanceGate() *MaintenanceGate {
	zero := make(chan struct{})
	close(zero)
	return &MaintenanceGate{zero: zero}
}

func (g *MaintenanceGate) BeginWrite() (func(), bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.maintenance {
		return nil, false
	}
	if g.activeWrites == 0 {
		g.zero = make(chan struct{})
	}
	g.activeWrites++
	return func() {
		g.mu.Lock()
		g.activeWrites--
		if g.activeWrites == 0 {
			close(g.zero)
		}
		g.mu.Unlock()
	}, true
}

func (g *MaintenanceGate) EnterAndWait(ctx context.Context) error {
	g.mu.Lock()
	g.maintenance = true
	zero := g.zero
	g.mu.Unlock()
	select {
	case <-zero:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *MaintenanceGate) Exit() {
	g.mu.Lock()
	g.maintenance = false
	g.mu.Unlock()
}

func (g *MaintenanceGate) Active() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.maintenance
}

func (g *MaintenanceGate) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		done, ok := g.BeginWrite()
		if !ok {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"maintenance_mode","message":"FastSell is in maintenance mode for database restore."}`))
			return
		}
		defer done()
		next.ServeHTTP(w, r)
	})
}
