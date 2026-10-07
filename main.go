// Command csv2jsonl converts a CSV file into a JSON Lines file.
//
// Usage:
//
//	csv2jsonl [flags] <input.csv> <output.jl>
//
// The first row of the CSV file must be a header. Each data row becomes one
// JSON object on its own line, keyed by the header's column names.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"time"

	"github.com/CHbobray/csv2jsonl/jsonlines"
)

const usageText = `csv2jsonl converts a comma-separated values (CSV) file to JSON Lines.

Usage:
  csv2jsonl [flags] <input.csv> <output.jl>

Example:
  csv2jsonl housesInput.csv housesOutput.jl

The first row of the input must be a header row. Each later row becomes one
JSON object per line in the output file. If the output file already exists,
it is overwritten.

Flags:
`

// usageError marks command-line mistakes, so that main can point the user to
// the help text and exit with status 2 (the convention for bad usage).
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func usageErrorf(format string, args ...any) error {
	return &usageError{msg: fmt.Sprintf(format, args...)}
}

// isUsageError reports whether err is (or wraps) a usageError.
func isUsageError(err error) bool {
	var target *usageError
	return errors.As(err, &target)
}

// config holds the parsed command-line settings.
type config struct {
	inputPath  string
	outputPath string
	quiet      bool
	cpuProfile string
	memProfile string
}

func main() {
	if err := run(os.Args[1:], os.Stderr); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return // -h or -help: usage was already printed
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		if isUsageError(err) {
			fmt.Fprintln(os.Stderr, "Run 'csv2jsonl -h' for help.")
			os.Exit(2)
		}
		os.Exit(1)
	}
}

// run is the testable body of the program. It takes arguments and an output
// stream for messages instead of reading os.Args and writing to os.Stderr
// directly.
func run(args []string, stderr io.Writer) error {
	cfg, err := parseArgs(args, stderr)
	if err != nil {
		return err
	}
	logger := log.New(stderr, "csv2jsonl: ", 0)
	if cfg.quiet {
		logger.SetOutput(io.Discard)
	}

	if cfg.cpuProfile != "" {
		stop, err := startCPUProfile(cfg.cpuProfile)
		if err != nil {
			return err
		}
		defer stop()
	}

	start := time.Now()
	rows, err := convertFile(cfg.inputPath, cfg.outputPath)
	if err != nil {
		return err
	}
	logger.Printf("wrote %d rows to %s in %v", rows, cfg.outputPath, time.Since(start).Round(time.Microsecond))

	if cfg.memProfile != "" {
		if err := writeMemProfile(cfg.memProfile); err != nil {
			return err
		}
	}
	return nil
}

// parseArgs parses flags and the two positional file paths and checks that
// they can be used.
func parseArgs(args []string, stderr io.Writer) (config, error) {
	var cfg config
	flags := flag.NewFlagSet("csv2jsonl", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.BoolVar(&cfg.quiet, "q", false, "quiet: don't print the summary line")
	flags.StringVar(&cfg.cpuProfile, "cpuprofile", "", "write a CPU profile to `file`")
	flags.StringVar(&cfg.memProfile, "memprofile", "", "write a heap profile to `file`")
	flags.Usage = func() {
		fmt.Fprint(stderr, usageText)
		flags.PrintDefaults()
	}

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return cfg, err
		}
		return cfg, usageErrorf("%v", err)
	}
	if flags.NArg() != 2 {
		return cfg, usageErrorf("expected 2 arguments (input and output paths), got %d", flags.NArg())
	}
	cfg.inputPath, cfg.outputPath = flags.Arg(0), flags.Arg(1)

	if err := checkPaths(cfg.inputPath, cfg.outputPath); err != nil {
		return cfg, usageErrorf("%v", err)
	}
	return cfg, nil
}

// checkPaths rejects obvious problems before any file is created or truncated.
func checkPaths(inputPath, outputPath string) error {
	info, err := os.Stat(inputPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("input file %q does not exist", inputPath)
	}
	if err != nil {
		return fmt.Errorf("cannot access input file: %w", err)
	}
	if info.IsDir() {
		return fmt.Errorf("input path %q is a directory, not a file", inputPath)
	}

	// Opening the output for writing would erase the input before it was read.
	if sameFile(inputPath, outputPath) {
		return errors.New("input and output paths refer to the same file")
	}
	if dir := filepath.Dir(outputPath); !isDir(dir) {
		return fmt.Errorf("output directory %q does not exist", dir)
	}
	return nil
}

// sameFile reports whether two paths name the same existing file.
func sameFile(a, b string) bool {
	infoA, errA := os.Stat(a)
	infoB, errB := os.Stat(b)
	return errA == nil && errB == nil && os.SameFile(infoA, infoB)
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// convertFile converts the CSV file at inputPath to JSON Lines at outputPath.
// If conversion fails, it removes the partial output file so that no
// half-written file is left behind.
func convertFile(inputPath, outputPath string) (rows int, err error) {
	input, err := os.Open(inputPath)
	if err != nil {
		return 0, err
	}
	defer input.Close()

	output, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	defer func() {
		// Close errors matter for writes: they can report a failed flush to disk.
		if closeErr := output.Close(); err == nil {
			err = closeErr
		}
		if err != nil {
			os.Remove(outputPath)
		}
	}()

	rows, err = jsonlines.Convert(input, output)
	if err != nil {
		return rows, fmt.Errorf("%s: %w", inputPath, err)
	}
	return rows, nil
}

// startCPUProfile starts CPU profiling and returns a function that stops it.
func startCPUProfile(path string) (stop func(), err error) {
	file, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("creating cpu profile: %w", err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		file.Close()
		return nil, fmt.Errorf("starting cpu profile: %w", err)
	}
	return func() {
		pprof.StopCPUProfile()
		file.Close()
	}, nil
}

// writeMemProfile writes a heap profile to path.
func writeMemProfile(path string) error {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("creating memory profile: %w", err)
	}
	defer file.Close()
	runtime.GC() // get up-to-date statistics
	if err := pprof.WriteHeapProfile(file); err != nil {
		return fmt.Errorf("writing memory profile: %w", err)
	}
	return nil
}
