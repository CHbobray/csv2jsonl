package jsonlines

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"os"
	"strings"
	"testing"
)

func TestIsJSONNumber(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"0", true},
		{"452600", true},
		{"-12", true},
		{"8.3252", true},
		{"1e10", true},
		{"-2.5E-3", true},
		{"", false},
		{"007", false}, // JSON forbids leading zeros; keep zip codes as strings
		{".5", false},
		{"5.", false},
		{"+1", false},
		{"NaN", false},
		{"Inf", false},
		{"0x1F", false},
		{"1_000", false},
		{"1,000", false},
		{" 42", false},
		{"NEAR BAY", false},
	}
	for _, tt := range tests {
		if got := IsJSONNumber(tt.input); got != tt.want {
			t.Errorf("IsJSONNumber(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestConvert(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     string
		wantRows int
	}{
		{
			name:     "numbers are unquoted",
			input:    "value,income\n452600,8.3252\n",
			want:     `{"value":452600,"income":8.3252}` + "\n",
			wantRows: 1,
		},
		{
			name:     "text, empty values, and leading zeros become strings",
			input:    "zip,city,note\n02134,Boston,\n",
			want:     `{"zip":"02134","city":"Boston","note":""}` + "\n",
			wantRows: 1,
		},
		{
			name:     "quotes, commas, and newlines are escaped",
			input:    "id,text\n1,\"say \"\"hi\"\", ok\"\n2,\"two\nlines\"\n",
			want:     `{"id":1,"text":"say \"hi\", ok"}` + "\n" + `{"id":2,"text":"two\nlines"}` + "\n",
			wantRows: 2,
		},
		{
			name:     "header only gives empty output",
			input:    "a,b\n",
			want:     "",
			wantRows: 0,
		},
		{
			name:     "byte-order mark and header spaces are stripped",
			input:    "\ufeff a , b\n1,2\n",
			want:     `{"a":1,"b":2}` + "\n",
			wantRows: 1,
		},
		{
			name:     "windows line endings",
			input:    "a,b\r\n1,2\r\n",
			want:     `{"a":1,"b":2}` + "\n",
			wantRows: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			rows, err := Convert(strings.NewReader(tt.input), &out)
			if err != nil {
				t.Fatalf("Convert returned error: %v", err)
			}
			if rows != tt.wantRows {
				t.Errorf("rows = %d, want %d", rows, tt.wantRows)
			}
			if got := out.String(); got != tt.want {
				t.Errorf("output mismatch\n got: %q\nwant: %q", got, tt.want)
			}
		})
	}
}

func TestConvertErrors(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr string
	}{
		{"empty input", "", "no header"},
		{"empty column name", "a,,c\n1,2,3\n", "column 2 is empty"},
		{"duplicate column name", "a,b,a\n1,2,3\n", `duplicate column "a"`},
		{"too few fields", "a,b,c\n1,2\n", "wrong number of fields"},
		{"too many fields", "a,b\n1,2,3\n", "wrong number of fields"},
		{"bad quoting", "a,b\n1,\"unterminated\n", "extraneous or missing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Convert(strings.NewReader(tt.input), io.Discard)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("error %q does not contain %q", err, tt.wantErr)
			}
		})
	}
}

func TestConvertNoHeaderIsSentinel(t *testing.T) {
	_, err := Convert(strings.NewReader(""), io.Discard)
	if !errors.Is(err, ErrNoHeader) {
		t.Errorf("err = %v, want ErrNoHeader", err)
	}
}

// TestConvertHousingSample checks the result against the expected output given
// in the assignment.
func TestConvertHousingSample(t *testing.T) {
	input, err := os.Open("../testdata/houses_sample.csv")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	want, err := os.ReadFile("../testdata/houses_sample.jl")
	if err != nil {
		t.Fatal(err)
	}

	var got bytes.Buffer
	if _, err := Convert(input, &got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got.Bytes(), want) {
		t.Errorf("output mismatch\n got:\n%s\nwant:\n%s", got.Bytes(), want)
	}
}

// TestConvertRandomRoundTrip generates synthetic CSV with awkward values and
// checks that every output line is valid JSON that decodes back to the
// original values.
func TestConvertRandomRoundTrip(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	header := []string{"id", "price", "label", "notes"}
	const rowCount = 500

	records := make([][]string, rowCount)
	for i := range records {
		records[i] = []string{
			fmt.Sprint(i),
			fmt.Sprintf("%.4f", rng.Float64()*1e6),
			randomText(rng),
			randomText(rng),
		}
	}

	var out bytes.Buffer
	rows, err := Convert(strings.NewReader(toCSV(header, records)), &out)
	if err != nil {
		t.Fatal(err)
	}
	if rows != rowCount {
		t.Fatalf("rows = %d, want %d", rows, rowCount)
	}

	scanner := bufio.NewScanner(&out)
	for i := 0; scanner.Scan(); i++ {
		line := scanner.Bytes()
		if !json.Valid(line) {
			t.Fatalf("line %d is not valid JSON: %s", i+1, line)
		}
		var decoded map[string]any
		if err := json.Unmarshal(line, &decoded); err != nil {
			t.Fatal(err)
		}
		for col, name := range header {
			if got := fmt.Sprint(decoded[name]); !IsJSONNumber(records[i][col]) && got != records[i][col] {
				t.Errorf("line %d, %s = %q, want %q", i+1, name, got, records[i][col])
			}
		}
	}
}

// randomText returns a string mixing characters that need JSON or CSV escaping.
func randomText(rng *rand.Rand) string {
	pieces := []string{"plain", `"quoted"`, "a,b", "back\\slash", "tab\t", "new\nline", "café", "日本", "", "123"}
	var b strings.Builder
	for range rng.Intn(4) + 1 {
		b.WriteString(pieces[rng.Intn(len(pieces))])
	}
	return b.String()
}

// toCSV builds a CSV document with every field quoted.
func toCSV(header []string, records [][]string) string {
	var b strings.Builder
	writeRow := func(fields []string) {
		for i, field := range fields {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`"` + strings.ReplaceAll(field, `"`, `""`) + `"`)
		}
		b.WriteByte('\n')
	}
	writeRow(header)
	for _, record := range records {
		writeRow(record)
	}
	return b.String()
}

// syntheticHousingCSV builds a housing-style CSV with rowCount random rows.
func syntheticHousingCSV(rowCount int) []byte {
	rng := rand.New(rand.NewSource(42))
	var b bytes.Buffer
	b.WriteString("value,income,age,rooms,bedrooms,pop,hh\n")
	for range rowCount {
		fmt.Fprintf(&b, "%d,%.4f,%d,%d,%d,%d,%d\n",
			rng.Intn(500000)+15000, rng.Float64()*15, rng.Intn(52)+1,
			rng.Intn(10000)+2, rng.Intn(2000)+1, rng.Intn(5000)+3, rng.Intn(1500)+1)
	}
	return b.Bytes()
}

// BenchmarkConvert measures throughput on 20,640 rows, the size of the
// California housing data set.
func BenchmarkConvert(b *testing.B) {
	data := syntheticHousingCSV(20640)
	b.SetBytes(int64(len(data)))
	b.ReportAllocs()
	for b.Loop() {
		if _, err := Convert(bytes.NewReader(data), io.Discard); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkIsJSONNumber(b *testing.B) {
	values := []string{"452600", "8.3252", "NEAR BAY", "-1.5e3", ""}
	for b.Loop() {
		for _, v := range values {
			IsJSONNumber(v)
		}
	}
}

// FuzzConvert feeds random input to Convert. Whenever Convert succeeds, every
// output line must be valid JSON. Run with: go test -fuzz=FuzzConvert ./jsonlines
func FuzzConvert(f *testing.F) {
	f.Add("value,income\n452600,8.3252\n")
	f.Add("a,b\n\"x,\"\"y\"\"\",-0.5e3\n")
	f.Add("\ufeffk\n\x00\xff\n")
	f.Fuzz(func(t *testing.T, input string) {
		var out bytes.Buffer
		if _, err := Convert(strings.NewReader(input), &out); err != nil {
			return // rejecting bad CSV is fine; producing bad JSON is not
		}
		scanner := bufio.NewScanner(&out)
		scanner.Buffer(nil, len(input)*8+64) // allow lines as long as the input can make
		for scanner.Scan() {
			if line := scanner.Bytes(); !json.Valid(line) {
				t.Fatalf("invalid JSON line %q from input %q", line, input)
			}
		}
	})
}
