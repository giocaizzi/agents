package catalog

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// buildZip returns a zip with the given files (path → content).
func buildZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// epochFixtures reads the trimmed real CSVs, placing eci_scores.csv in its
// real subdirectory.
func epochFixtures(t *testing.T) map[string]string {
	t.Helper()
	files := map[string]string{}
	for _, name := range []string{
		"eci_scores.csv", "terminalbench_external.csv", "swe_bench_verified.csv",
		"metr_time_horizons_external.csv", "osworld_2_external.csv",
	} {
		data, err := os.ReadFile("testdata/epoch/" + name)
		if err != nil {
			t.Fatal(err)
		}
		files[name] = string(data)
	}
	files["epoch_capabilities_index/eci_scores.csv"] = files["eci_scores.csv"]
	delete(files, "eci_scores.csv")
	return files
}

func TestParseEpoch(t *testing.T) {
	t.Parallel()
	rows, err := ParseEpoch(buildZip(t, epochFixtures(t)))
	if err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, r := range rows {
		counts[r.Benchmark]++
	}
	// terminalbench fixture: 3 rows plus one with an empty model, which is skipped.
	want := map[string]int{"eci": 3, "terminalBench": 3, "sweBenchVerified": 3, "metrHorizonMin": 3, "osworld": 3}
	if diff := cmp.Diff(want, counts); diff != "" {
		t.Errorf("rows per benchmark (-want +got):\n%s", diff)
	}
	if rows[0] != (EpochRow{Benchmark: "eci", Model: "GPT-6 Astra", Score: 169.07}) {
		t.Errorf("first row = %+v", rows[0])
	}
}

func TestParseEpochSkipsBadRows(t *testing.T) {
	t.Parallel()
	files := epochFixtures(t)
	files["swe_bench_verified.csv"] = "Model version,mean_score\n" +
		"a_max,0.5\n,0.9\nb_max,n/a\n  c_high ,  0.7 \nd,NaN\nshort\n"
	rows, err := ParseEpoch(buildZip(t, files))
	if err != nil {
		t.Fatal(err)
	}
	var got []EpochRow
	for _, r := range rows {
		if r.Benchmark == "sweBenchVerified" {
			got = append(got, r)
		}
	}
	want := []EpochRow{
		{Benchmark: "sweBenchVerified", Model: "a_max", Score: 0.5},
		{Benchmark: "sweBenchVerified", Model: "c_high", Score: 0.7},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("(-want +got):\n%s", diff)
	}
}

func TestParseEpochErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		mutate  func(files map[string]string)
		wantErr string
	}{
		{"missing file", func(f map[string]string) { delete(f, "osworld_2_external.csv") }, "osworld_2_external.csv not found"},
		{
			"os_world_external is not osworld_2",
			func(f map[string]string) {
				f["os_world_external.csv"] = f["osworld_2_external.csv"]
				delete(f, "osworld_2_external.csv")
			},
			"osworld_2_external.csv not found",
		},
		{"missing key column", func(f map[string]string) { f["swe_bench_verified.csv"] = "Name,mean_score\na,1\n" }, `column "Model version" not found`},
		{"missing score column", func(f map[string]string) { f["swe_bench_verified.csv"] = "Model version,score\na,1\n" }, `column "mean_score" not found`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			files := epochFixtures(t)
			tt.mutate(files)
			_, err := ParseEpoch(buildZip(t, files))
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
	if _, err := ParseEpoch([]byte("not a zip")); err == nil {
		t.Error("want error for invalid archive")
	}
}
