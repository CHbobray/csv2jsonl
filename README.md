# csv2jsonl

A Go command-line utility that converts comma-separated values (CSV) into
[JSON Lines](https://jsonlines.org/) (newline-delimited JSON, also `.ndjson`).
It was written for the California housing data (Miller 2015), and it works on
**any** CSV file that has a header row (the optional general-purpose extension).

```
$ csv2jsonl housesInput.csv housesOutput.jl
csv2jsonl: wrote 20640 rows to housesOutput.jl in 14.604ms   (timing varies by machine)
```

The first lines of `housesOutput.jl`:

```json
{"value":452600,"income":8.3252,"age":41,"rooms":880,"bedrooms":129,"pop":322,"hh":126}
{"value":358500,"income":8.3014,"age":21,"rooms":7099,"bedrooms":1106,"pop":2401,"hh":1138}
{"value":352100,"income":7.2574,"age":52,"rooms":1467,"bedrooms":190,"pop":496,"hh":177}
```

## Repository contents

| Path | Role |
| --- | --- |
| `main.go` | Command-line program: argument parsing, input checks, file handling, logging, and optional profiling. |
| `main_test.go` | Tests for the command line: good runs, bad arguments, cleanup after failure, and profile output. |
| `jsonlines/convert.go` | Reusable package that does the conversion. It has no dependency on the command line, so other Go programs can import it. |
| `jsonlines/convert_test.go` | Unit tests, a synthetic-data round-trip test, a fuzz test, and benchmarks. |
| `testdata/houses_sample.csv` | The first three housing records, used as a test input. |
| `testdata/houses_sample.jl` | The expected output for that sample, copied from the assignment. |
| `housesInput.csv` | The full assignment data set (20,640 records). |
| `go.mod` | Go module definition. The program uses only the standard library. |

## How the conversion works

1. The first CSV row is the **header**. Its column names become the JSON
   keys, in the same order. A UTF-8 byte-order mark (which Excel adds) and
   spaces around the names are removed.
2. Each later row becomes **one JSON object on one line**, ending in `\n`.
3. A value that matches the JSON number grammar (RFC 8259) is written as a
   **number**, for example `452600` or `8.3252`. Every other value is written
   as a properly escaped **string**. This includes empty fields (`""`),
   numbers with leading zeros such as ZIP codes (`"02134"`), and text such as
   `"NEAR BAY"`.
4. The input is **streamed** one row at a time, so memory use stays flat
   even for very large files.

### Error handling (user input checks)

The program checks its input before it creates any output, and it explains
each problem in plain language:

| Problem | Message | Exit code |
| --- | --- | --- |
| Wrong number of arguments | `expected 2 arguments (input and output paths), got N` | 2 |
| Input file missing, or a directory | `input file "x.csv" does not exist` | 2 |
| Output path same as input (would erase the data) | `input and output paths refer to the same file` | 2 |
| Output folder missing | `output directory "..." does not exist` | 2 |
| Empty file, empty or duplicate header names | `csv header has duplicate column "a"` | 1 |
| A row with the wrong number of fields, or bad quoting | `record on line 3: wrong number of fields` | 1 |

If conversion fails partway, the partial output file is deleted, so no
half-written file is left behind.

## Building the application

You need [Go 1.24 or newer](https://go.dev/dl/). Check with `go version`.

```bash
git clone https://github.com/CHbobray/csv2jsonl.git
cd csv2jsonl
```

**Windows** (creates `csv2jsonl.exe`):

```powershell
go build -o csv2jsonl.exe .
```

**macOS / Linux** (creates the executable `csv2jsonl`):

```bash
go build -o csv2jsonl .
```

**Cross-compiling** (build for another system from the one you're on):

```bash
GOOS=windows GOARCH=amd64 go build -o csv2jsonl.exe .   # Windows
GOOS=darwin  GOARCH=arm64 go build -o csv2jsonl-mac .   # Apple silicon Mac
GOOS=darwin  GOARCH=amd64 go build -o csv2jsonl-mac .   # Intel Mac
```

On PowerShell, set the variables first: `$env:GOOS="darwin"; $env:GOARCH="arm64"; go build -o csv2jsonl-mac .`

> **macOS note:** A Go build is a single command-line executable. It is not a
> `.app` bundle, because a `.app` is meant for graphical programs. Run it from
> Terminal. If Gatekeeper blocks a binary you copied from somewhere else, run
> `xattr -d com.apple.quarantine csv2jsonl-mac`.

## Using the application

```text
csv2jsonl [flags] <input.csv> <output.jl>

Flags:
  -q                 quiet: don't print the summary line
  -cpuprofile file   write a CPU profile to file
  -memprofile file   write a heap profile to file
  -h                 show help
```

Examples:

```bash
./csv2jsonl housesInput.csv housesOutput.jl          # macOS / Linux
.\csv2jsonl.exe housesInput.csv housesOutput.jl      # Windows
go run . housesInput.csv housesOutput.jl             # without building
```

Flags must come **before** the two file paths. To check any output line,
paste it into <https://jsonlint.com/>. You can also check the whole file with
`jq`: `jq -c . housesOutput.jl > /dev/null && echo all lines valid`.

### Using the converter from your own Go code

```go
import "github.com/CHbobray/csv2jsonl/jsonlines"

rows, err := jsonlines.Convert(csvReader, jsonWriter) // any io.Reader / io.Writer
```

## Testing

```bash
go test ./...                      # all unit tests
go test -v ./...                   # verbose, one line per test
go test -cover ./...               # coverage summary
go test -coverprofile=c.out ./... && go tool cover -html=c.out   # coverage report in a browser
```

What the tests cover:

- **Number detection**: valid JSON numbers, plus values that look numeric but
  aren't valid JSON (`007`, `NaN`, `0x1F`, `1_000`, `.5`).
- **Conversion**: numbers vs. strings, escaping of quotes, commas, tabs,
  newlines and Unicode, byte-order marks, Windows line endings, and
  header-only files.
- **Bad input**: empty files, empty or duplicate column names, rows that are
  too short or too long, and broken quoting.
- **Assignment sample**: the output must match the assignment's example
  exactly, byte for byte.
- **Synthetic data**: 500 randomly generated rows with awkward characters.
  Every output line is checked with `json.Valid` and decoded back to the
  original values.
- **Fuzzing**: random CSV input. Whenever the conversion succeeds, every output
  line must be valid JSON. Run it with
  `go test -fuzz=FuzzConvert -fuzztime=60s ./jsonlines`.
- **Command line**: argument checks, quiet mode, deleting partial output, and
  writing profiles.

Coverage is about 93% for the `jsonlines` package and about 81% for `main`.

## Performance, benchmarks, and profiling

The benchmarks use synthetic housing data the same size as the real file
(20,640 rows):

```bash
go test -run='^$' -bench=. -benchmem ./jsonlines
```

| Version | Time per 20,640 rows | Throughput | Number check |
| --- | --- | --- | --- |
| First version (`regexp`) | 21.5 ms | 34 MB/s | 412 ns / 5 values |
| Final version (hand-written scanner) | **6.2 ms** | **117 MB/s** | **34 ns / 5 values** |

The first version checked numbers with a regular expression. The benchmark
pointed to that check as the slowest part, so it was replaced with a short
function that walks the RFC 8259 grammar. The full conversion became about
3.4× faster. Memory use is about 670 KB per run (roughly one small allocation
per row) and stays the same however large the input is.

To profile a real run:

```bash
./csv2jsonl -cpuprofile cpu.prof -memprofile mem.prof housesInput.csv housesOutput.jl
go tool pprof -top cpu.prof
go tool pprof -http=:8080 cpu.prof     # interactive view in a browser
```

## Code quality tooling

These tools follow the Go Time #237 episode on Go tooling (Ryer, Dogan, and
Boursiquot):

```bash
gofmt -l .          # lists files that need formatting (should print nothing)
go vet ./...        # finds suspicious code
staticcheck ./...   # linter: go install honnef.co/go/tools/cmd/staticcheck@latest
go mod tidy         # keeps go.mod clean
```

Design choices:

- **Standard library only.** Gerardi (2021) suggests Cobra for larger command-line
  tools. This program has two arguments and three flags, so the standard `flag`
  package is enough and avoids a dependency.
- **Logic in a package, not in `main`.** All conversion work is in `jsonlines`,
  which only needs an `io.Reader` and an `io.Writer`. That makes it
  easy to test and reuse with files, network streams, or in-memory buffers.
- **`main` holds only a testable `run` function.** `main` just calls
  `run(args, stderr)` and sets the exit code, so the tests can drive the
  complete program.

## References

- Bodner, J. (2024). *Learning Go* (2nd ed.). O'Reilly. Chapter 15, "Writing Tests."
- Gerardi, R. (2021). *Powerful Command-Line Applications in Go*. Pragmatic Bookshelf.
- McConnell, S. (2004). *Code Complete* (2nd ed.). Microsoft Press.
- Miller, T. W. (2015). *Modeling Techniques in Predictive Analytics*. Pearson.
- Ryer, M., Dogan, J., & Boursiquot, J. (2019). Go tooling [Audio podcast episode]. *Go Time* #237 (Changelog).
- Bray, T. (2017). RFC 8259: The JavaScript Object Notation (JSON) Data Interchange Format.

## Use of AI assistants

I used Claude (Anthropic's AI assistant) extensively on this assignment. I gave it
the assignment instructions and the housesInput.csv data file, and it wrote the Go
code: the `jsonlines` conversion package, the command-line program in `main.go`, the
unit tests, fuzz test, and benchmarks, and the first draft of this README. While
writing the code, Claude ran the benchmarks, found that a regular expression for
detecting numbers was the main bottleneck, and replaced it with a hand-written
check, which made the conversion about 3.4 times faster.

Claude also walked me step by step through setting up and submitting the project:
configuring Git, filling in my GitHub username, building the program, and pushing
the repository to GitHub.

My own work was setting up the project on my Windows machine and verifying it there.
I ran `gofmt`, `go vet`, `staticcheck`, and `go test` (all passed), built
`csv2jsonl.exe`, converted the full data set (20,640 rows), checked output lines
with jsonlint.com, tried invalid inputs to confirm the error messages, ran the
benchmarks, and updated the README with my own results. I then read through the
code to understand how it works.
