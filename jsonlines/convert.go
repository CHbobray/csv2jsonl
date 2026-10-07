// Package jsonlines converts comma-separated values (CSV) into JSON Lines
// (newline-delimited JSON), one JSON object per CSV data row.
//
// The first CSV row is treated as the header; its column names become the
// JSON keys. Values that look like JSON numbers are written as numbers, and
// everything else is written as a JSON string. Key order in each output
// object follows the column order of the CSV header.
//
// The converter streams its input, so memory use stays flat no matter how
// large the CSV file is.
package jsonlines

import (
	"bufio"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// ErrNoHeader is returned when the input contains no header row.
var ErrNoHeader = errors.New("input has no header row")

// IsJSONNumber reports whether s can be written to JSON as a bare number.
// It follows the number grammar in RFC 8259, section 6:
//
//	[-] (0 | [1-9][0-9]*) [. [0-9]+] [(e|E) [+|-] [0-9]+]
//
// strconv.ParseFloat is not strict enough for this check: it accepts "NaN",
// "Inf", "0x1F", and "1_000", and none of those are valid JSON. The grammar
// is checked by hand instead of with a regular expression because this
// function runs on every CSV field. Benchmarks showed the regexp version was
// the program's main cost (see README).
func IsJSONNumber(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}

	// Integer part: a single 0, or a nonzero digit followed by more digits.
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && '1' <= s[i] && s[i] <= '9':
		i = skipDigits(s, i)
	default:
		return false
	}

	// Optional fraction: a dot followed by at least one digit.
	if i < len(s) && s[i] == '.' {
		start := i + 1
		if i = skipDigits(s, start); i == start {
			return false
		}
	}

	// Optional exponent: e or E, an optional sign, then at least one digit.
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		i++
		if i < len(s) && (s[i] == '+' || s[i] == '-') {
			i++
		}
		start := i
		if i = skipDigits(s, start); i == start {
			return false
		}
	}

	return i == len(s)
}

// skipDigits returns the index of the first non-digit byte in s at or after i.
func skipDigits(s string, i int) int {
	for i < len(s) && '0' <= s[i] && s[i] <= '9' {
		i++
	}
	return i
}

// Convert reads CSV from r and writes one JSON object per data row to w.
// It returns the number of data rows written.
//
// Convert fails when the header is missing, empty, or contains duplicate
// column names, or when a data row has a different number of fields than the
// header. Error messages include the CSV line number of the problem.
func Convert(r io.Reader, w io.Writer) (int, error) {
	reader := csv.NewReader(r)
	reader.ReuseRecord = true // one allocation per row rather than per record
	reader.TrimLeadingSpace = true

	header, err := readHeader(reader)
	if err != nil {
		return 0, err
	}
	keys, err := encodeKeys(header)
	if err != nil {
		return 0, err
	}

	out := bufio.NewWriter(w)
	var line []byte
	rows := 0
	for {
		record, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// csv.ParseError already carries the line number.
			return rows, fmt.Errorf("reading csv: %w", err)
		}

		line = appendObject(line[:0], keys, record)
		line = append(line, '\n')
		if _, err := out.Write(line); err != nil {
			return rows, fmt.Errorf("writing json lines: %w", err)
		}
		rows++
	}

	if err := out.Flush(); err != nil {
		return rows, fmt.Errorf("writing json lines: %w", err)
	}
	return rows, nil
}

// readHeader reads the first record and checks that it is usable as JSON keys.
func readHeader(reader *csv.Reader) ([]string, error) {
	header, err := reader.Read()
	if errors.Is(err, io.EOF) {
		return nil, ErrNoHeader
	}
	if err != nil {
		return nil, fmt.Errorf("reading csv header: %w", err)
	}

	// ReuseRecord means the next Read overwrites this slice, so copy it.
	header = append([]string(nil), header...)

	// Drop the UTF-8 byte-order mark that Excel puts at the start of files.
	// Left in place, it would turn the first key into "\ufeffvalue".
	header[0] = strings.TrimPrefix(header[0], "\ufeff")

	seen := make(map[string]bool, len(header))
	for i, name := range header {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("csv header column %d is empty", i+1)
		}
		if seen[name] {
			return nil, fmt.Errorf("csv header has duplicate column %q", name)
		}
		seen[name] = true
		header[i] = name
	}
	return header, nil
}

// encodeKeys encodes each column name once, as a quoted JSON string followed
// by a colon. That way the hot loop never re-encodes the keys.
func encodeKeys(header []string) ([][]byte, error) {
	keys := make([][]byte, len(header))
	for i, name := range header {
		quoted, err := json.Marshal(name)
		if err != nil {
			return nil, fmt.Errorf("encoding column name %q: %w", name, err)
		}
		keys[i] = append(quoted, ':')
	}
	return keys, nil
}

// appendObject appends one JSON object, built from keys and values, to buf.
// The csv.Reader has already checked that len(values) == len(keys)
// (FieldsPerRecord defaults to the header's field count).
func appendObject(buf []byte, keys [][]byte, values []string) []byte {
	buf = append(buf, '{')
	for i, value := range values {
		if i > 0 {
			buf = append(buf, ',')
		}
		buf = append(buf, keys[i]...)
		buf = appendValue(buf, value)
	}
	return append(buf, '}')
}

// appendValue appends value as a bare JSON number when it is one, and as a
// quoted, escaped JSON string otherwise.
func appendValue(buf []byte, value string) []byte {
	if IsJSONNumber(value) {
		return append(buf, value...)
	}
	// json.Marshal of a string never fails. It handles quotes, backslashes,
	// control characters, and invalid UTF-8.
	quoted, _ := json.Marshal(value)
	return append(buf, quoted...)
}
