package catalog

import (
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
)

func TestParseArenaPage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		file, board string
		total       int
		first       ArenaRow
		rows        int
	}{
		{
			file: "arena_text.json", board: ArenaBoard, total: 11132, rows: 4,
			first: ArenaRow{
				Board: ArenaBoard, Category: "overall", ModelName: "Gemini 4 Carbon (High)", Org: "google",
				Rank: 1, Rating: 1527.5065447464403, Votes: 4403, PublishDate: "2026-10-28",
			},
		},
		{
			file: "arena_agent.json", board: "agent", total: 58, rows: 3,
			first: ArenaRow{
				Board: "agent", Category: "overall", ModelName: "Claude Opus 5.5 (Max)", Org: "anthropic",
				Rank: 1, Rating: 0.20144840119751856, Votes: 14169, PublishDate: "2026-11-04",
			},
		},
		{file: "arena_webdev.json", board: "webdev", total: 655, rows: 3},
	}
	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile("testdata/" + tt.file)
			if err != nil {
				t.Fatal(err)
			}
			rows, total, err := ParseArenaPage(tt.board, data)
			if err != nil {
				t.Fatal(err)
			}
			if total != tt.total || len(rows) != tt.rows {
				t.Fatalf("total = %d, rows = %d; want %d, %d", total, len(rows), tt.total, tt.rows)
			}
			if tt.first.ModelName == "" {
				return
			}
			if diff := cmp.Diff(tt.first, rows[0]); diff != "" {
				t.Errorf("first row (-want +got):\n%s", diff)
			}
		})
	}
}

func TestParseArenaPageFloatsAndInvalid(t *testing.T) {
	t.Parallel()
	data := []byte(`{"num_rows_total": 1, "rows": [{"row_idx": 0, "row": {
	  "model_name": "M", "organization": "o", "rating": 1500.0, "vote_count": 4403.0,
	  "rank": 2.0, "category": "coding", "leaderboard_publish_date": "2026-11-04"}}]}`)
	rows, total, err := ParseArenaPage(ArenaBoard, data)
	if err != nil {
		t.Fatal(err)
	}
	want := []ArenaRow{{
		Board: ArenaBoard, Category: "coding", ModelName: "M", Org: "o",
		Rank: 2, Rating: 1500, Votes: 4403, PublishDate: "2026-11-04",
	}}
	if diff := cmp.Diff(want, rows); diff != "" || total != 1 {
		t.Errorf("rows (-want +got):\n%s\ntotal = %d", diff, total)
	}
	if _, _, err := ParseArenaPage(ArenaBoard, []byte(`{`)); err == nil {
		t.Error("want error for invalid JSON")
	}
}
