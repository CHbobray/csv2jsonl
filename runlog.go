package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"time"
)

// openRunLog returns a logger that appends timestamped key=value records to
// the file at path, together with a function that closes the file. With an
// empty path, it returns a logger that discards everything, so callers can
// log unconditionally.
//
// The file is opened in append mode, so it builds up a history across runs.
func openRunLog(path string) (*slog.Logger, func(), error) {
	if path == "" {
		return slog.New(slog.DiscardHandler), func() {}, nil
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, fmt.Errorf("opening log file: %w", err)
	}
	return newRunLogger(file), func() { file.Close() }, nil
}

// newRunLogger builds the logger used for run logs. It is separate from
// openRunLog so that tests can log into a buffer.
func newRunLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewTextHandler(w, nil))
}

// logRunFinished records the results of a successful run: row count, timing,
// file sizes, and the Go runtime's memory statistics.
func logRunFinished(runLog *slog.Logger, cfg config, rows int, elapsed time.Duration) {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)

	runLog.Info("run finished",
		"rows", rows,
		"duration", elapsed,
		"rows_per_sec", int(float64(rows)/elapsed.Seconds()),
		"input_bytes", fileSize(cfg.inputPath),
		"output_bytes", fileSize(cfg.outputPath),
		"heap_alloc_bytes", mem.HeapAlloc,
		"total_alloc_bytes", mem.TotalAlloc,
		"sys_bytes", mem.Sys,
		"gc_cycles", mem.NumGC,
	)
}

// fileSize returns the size of the file at path, or -1 if it cannot be read.
func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return -1
	}
	return info.Size()
}
