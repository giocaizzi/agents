package catalog

import (
	"archive/zip"
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
)

// maxEpochFile bounds one decompressed CSV (the largest real one is far smaller).
const maxEpochFile = 64 << 20

type epochSource struct {
	file, keyCol, scoreCol, benchmark string
}

// epochSources are the files read, in output order. OSWorld reads osworld_2
// only: mixing in os_world_external.csv would compare two benchmarks.
func epochSources() []epochSource {
	return []epochSource{
		{"eci_scores.csv", "Model", "eci", benchECI},
		{"terminalbench_external.csv", colModelVersion, "Accuracy mean", "terminalBench"},
		{"swe_bench_verified.csv", colModelVersion, "mean_score", "sweBenchVerified"},
		{"metr_time_horizons_external.csv", colModelVersion, "Time horizon", "metrHorizonMin"},
		{"osworld_2_external.csv", colModelVersion, "Binary accuracy", "osworld"},
	}
}

// ParseEpoch reads Epoch AI's benchmark_data.zip. Rows with an empty model or
// an unparsable score are skipped; a missing file or column is an error.
func ParseEpoch(zipData []byte) ([]EpochRow, error) {
	zr, err := zip.NewReader(bytes.NewReader(zipData), int64(len(zipData)))
	if err != nil {
		return nil, fmt.Errorf("open epoch archive: %w", err)
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		if _, dup := files[path.Base(f.Name)]; !dup {
			files[path.Base(f.Name)] = f
		}
	}
	var out []EpochRow
	for _, src := range epochSources() {
		f, ok := files[src.file]
		if !ok {
			return nil, fmt.Errorf("epoch archive: %s not found", src.file)
		}
		rows, err := readEpochCSV(f, src)
		if err != nil {
			return nil, err
		}
		out = append(out, rows...)
	}
	return out, nil
}

func readEpochCSV(f *zip.File, src epochSource) ([]EpochRow, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", src.file, err)
	}
	defer rc.Close()
	data, err := io.ReadAll(io.LimitReader(rc, maxEpochFile))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", src.file, err)
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", src.file, err)
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("%s: empty file", src.file)
	}
	header := records[0]
	header[0] = strings.TrimPrefix(header[0], "\xef\xbb\xbf")
	keyIdx := slices.Index(header, src.keyCol)
	scoreIdx := slices.Index(header, src.scoreCol)
	var errs []error
	if keyIdx < 0 {
		errs = append(errs, fmt.Errorf("%s: column %q not found", src.file, src.keyCol))
	}
	if scoreIdx < 0 {
		errs = append(errs, fmt.Errorf("%s: column %q not found", src.file, src.scoreCol))
	}
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	var rows []EpochRow
	for _, rec := range records[1:] {
		if keyIdx >= len(rec) || scoreIdx >= len(rec) {
			continue
		}
		model := strings.TrimSpace(rec[keyIdx])
		score, err := strconv.ParseFloat(strings.TrimSpace(rec[scoreIdx]), 64)
		if model == "" || err != nil || math.IsNaN(score) || math.IsInf(score, 0) {
			continue
		}
		rows = append(rows, EpochRow{Benchmark: src.benchmark, Model: model, Score: score})
	}
	return rows, nil
}
