package catalog

import (
	"encoding/json"
	"fmt"
	"math"
)

type arenaRow struct {
	ModelName    string   `json:"model_name"`
	Organization string   `json:"organization"`
	Rating       *float64 `json:"rating"`
	Score        *float64 `json:"score"`
	VoteCount    *float64 `json:"vote_count"`
	SessionCount *float64 `json:"session_count"`
	Rank         float64  `json:"rank"`
	Category     string   `json:"category"`
	PublishDate  string   `json:"leaderboard_publish_date"`
}

// ParseArenaPage reads one datasets-server /rows page of the LMArena
// leaderboard dataset for board and returns its rows and the total row count.
// The agent board names its columns score and session_count.
func ParseArenaPage(board string, data []byte) (rows []ArenaRow, total int, err error) {
	var page struct {
		Total int `json:"num_rows_total"`
		Rows  []struct {
			Row arenaRow `json:"row"`
		} `json:"rows"`
	}
	if err := json.Unmarshal(data, &page); err != nil {
		return nil, 0, fmt.Errorf("decode arena %s page: %w", board, err)
	}
	for _, r := range page.Rows {
		rating, votes := r.Row.Rating, r.Row.VoteCount
		if board == boardAgent {
			rating, votes = r.Row.Score, r.Row.SessionCount
		}
		rows = append(rows, ArenaRow{
			Board:       board,
			Category:    r.Row.Category,
			ModelName:   r.Row.ModelName,
			Org:         r.Row.Organization,
			Rank:        int(math.Round(r.Row.Rank)),
			Rating:      deref(rating),
			Votes:       int(math.Round(deref(votes))),
			PublishDate: r.Row.PublishDate,
		})
	}
	return rows, page.Total, nil
}

func deref(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}
