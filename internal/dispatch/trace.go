package dispatch

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

var dispatchTraceWarnings = struct {
	mu     sync.Mutex
	writer io.Writer
	warned map[string]struct{}
}{
	writer: os.Stderr,
	warned: map[string]struct{}{},
}

func tracef(format string, args ...any) {
	path := strings.TrimSpace(os.Getenv("GC_WORKFLOW_TRACE"))
	if path == "" {
		path = strings.TrimSpace(os.Getenv("GC_SLING_TRACE"))
	}
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		dispatchTraceWarnOpenFailure(path, err)
		return
	}
	defer f.Close()                                                                                    //nolint:errcheck // best-effort trace log
	fmt.Fprintf(f, "%s %s\n", time.Now().UTC().Format(time.RFC3339Nano), fmt.Sprintf(format, args...)) //nolint:errcheck
}

func dispatchTraceWarnOpenFailure(path string, err error) {
	if strings.TrimSpace(path) == "" || err == nil {
		return
	}
	dispatchTraceWarnings.mu.Lock()
	defer dispatchTraceWarnings.mu.Unlock()
	if dispatchTraceWarnings.writer == nil {
		return
	}
	if _, warned := dispatchTraceWarnings.warned[path]; warned {
		return
	}
	dispatchTraceWarnings.warned[path] = struct{}{}
	fmt.Fprintf(dispatchTraceWarnings.writer, "gc dispatch: warning: opening workflow trace %q: %v\n", path, err) //nolint:errcheck // best-effort stderr
}

func useDispatchTraceWarnings(writer io.Writer) func() {
	dispatchTraceWarnings.mu.Lock()
	prevWriter := dispatchTraceWarnings.writer
	dispatchTraceWarnings.writer = writer
	dispatchTraceWarnings.warned = map[string]struct{}{}
	dispatchTraceWarnings.mu.Unlock()
	return func() {
		dispatchTraceWarnings.mu.Lock()
		dispatchTraceWarnings.writer = prevWriter
		dispatchTraceWarnings.warned = map[string]struct{}{}
		dispatchTraceWarnings.mu.Unlock()
	}
}
