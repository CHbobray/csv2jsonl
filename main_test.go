package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunConvertsSample(t *testing.T) {
	outputPath := filepath.Join(t.TempDir(), "out.jl")
	var stderr bytes.Buffer

	if err := run([]string{"testdata/houses_sample.csv", outputPath}, &stderr); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	got, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	want, err := os.ReadFile("testdata/houses_sample.jl")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output mismatch\n got:\n%s\nwant:\n%s", got, want)
	}
	if !strings.Contains(stderr.String(), "wrote 3 rows") {
		t.Errorf("summary line missing from stderr: %q", stderr.String())
	}
}

func TestRunQuiet(t *testing.T) {
	var stderr bytes.Buffer
	outputPath := filepath.Join(t.TempDir(), "out.jl")
	if err := run([]string{"-q", "testdata/houses_sample.csv", outputPath}, &stderr); err != nil {
		t.Fatal(err)
	}
	if stderr.Len() != 0 {
		t.Errorf("expected no output with -q, got %q", stderr.String())
	}
}

func TestRunRejectsBadArguments(t *testing.T) {
	dir := t.TempDir()
	input := "testdata/houses_sample.csv"

	tests := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{"no arguments", nil, "expected 2 arguments"},
		{"one argument", []string{input}, "expected 2 arguments"},
		{"three arguments", []string{input, "a", "b"}, "expected 2 arguments"},
		{"missing input", []string{filepath.Join(dir, "nope.csv"), "out.jl"}, "does not exist"},
		{"input is a directory", []string{dir, filepath.Join(dir, "out.jl")}, "is a directory"},
		{"output same as input", []string{input, input}, "same file"},
		{"output dir missing", []string{input, filepath.Join(dir, "no", "out.jl")}, "output directory"},
		{"unknown flag", []string{"-x", input, "out.jl"}, "flag provided but not defined"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := run(tt.args, &bytes.Buffer{})
			if !isUsageError(err) {
				t.Fatalf("err = %v, want a usage error", err)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestRunRemovesPartialOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "bad.csv")
	outputPath := filepath.Join(dir, "out.jl")
	// Row 3 has too many fields, so conversion fails after one good row.
	if err := os.WriteFile(inputPath, []byte("a,b\n1,2\n3,4,5\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := run([]string{inputPath, outputPath}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "line 3") {
		t.Fatalf("err = %v, want an error mentioning line 3", err)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Error("partial output file was left behind")
	}
}

func TestRunWritesProfiles(t *testing.T) {
	dir := t.TempDir()
	cpu, mem := filepath.Join(dir, "cpu.prof"), filepath.Join(dir, "mem.prof")
	args := []string{"-q", "-cpuprofile", cpu, "-memprofile", mem,
		"testdata/houses_sample.csv", filepath.Join(dir, "out.jl")}

	if err := run(args, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{cpu, mem} {
		if info, err := os.Stat(path); err != nil || info.Size() == 0 {
			t.Errorf("profile %s missing or empty", path)
		}
	}
}

func TestRunWritesLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "run.log")
	args := []string{"-q", "-log", logPath, "testdata/houses_sample.csv", filepath.Join(dir, "out.jl")}

	// Run twice: the log file is appended to, not overwritten.
	for range 2 {
		if err := run(args, &bytes.Buffer{}); err != nil {
			t.Fatal(err)
		}
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	logText := string(content)
	for _, want := range []string{"time=", `msg="run started"`, `msg="run finished"`, "rows=3", "heap_alloc_bytes="} {
		if !strings.Contains(logText, want) {
			t.Errorf("log is missing %q:\n%s", want, logText)
		}
	}
	if got := strings.Count(logText, `msg="run finished"`); got != 2 {
		t.Errorf("found %d finished records, want 2 (log should append)", got)
	}
}

func TestRunLogsFailure(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "bad.csv")
	logPath := filepath.Join(dir, "run.log")
	if err := os.WriteFile(inputPath, []byte("a,b\n1,2,3\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := run([]string{"-log", logPath, inputPath, filepath.Join(dir, "out.jl")}, &bytes.Buffer{}); err == nil {
		t.Fatal("expected an error")
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "level=ERROR") || !strings.Contains(string(content), "wrong number of fields") {
		t.Errorf("failure was not logged:\n%s", content)
	}
}

func TestRunRejectsUnwritableLog(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "missing-dir", "run.log")
	err := run([]string{"-log", logPath, "testdata/houses_sample.csv", filepath.Join(dir, "out.jl")}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "opening log file") {
		t.Errorf("err = %v, want an 'opening log file' error", err)
	}
}
