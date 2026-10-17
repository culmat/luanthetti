package depgraph

import (
	"database/sql"
	"encoding/json"
	"strings"
)

// Details are package columns beyond what the resolver needs: filled by `ldg build --details`
// (the nightly snapshot always is), absent in older snapshots.
type Details struct {
	ShortDescription string   `json:"short_description,omitempty"`
	ReleaseID        int64    `json:"release_id,omitempty"`
	License          string   `json:"license,omitempty"`
	MediaLicense     string   `json:"media_license,omitempty"`
	DevState         string   `json:"dev_state,omitempty"` // lower-case, e.g. actively_developed
	SupportsAllGames bool     `json:"supports_all_games,omitempty"`
	MinEngine        string   `json:"min_engine,omitempty"`
	MaxEngine        string   `json:"max_engine,omitempty"`
	Repo             string   `json:"repo,omitempty"`
	Tags             []string `json:"tags,omitempty"`
	CreatedAt        string   `json:"created_at,omitempty"`
	ApprovedAt       string   `json:"approved_at,omitempty"`
	LatestReleaseAt  string   `json:"latest_release_at,omitempty"`
}

// Details returns the extra columns of a package (loaded once for all packages).
func (idx *Index) Details(p *Pkg) Details {
	idx.detailsOnce.Do(idx.loadDetails)
	return idx.details[p.ID]
}

// HasTags reports whether the snapshot carries ContentDB tags.
func (idx *Index) HasTags() bool {
	idx.detailsOnce.Do(idx.loadDetails)
	return idx.hasTags
}

func (idx *Index) loadDetails() {
	idx.details = map[int64]Details{}
	cols, err := columns(idx.conn, "package")
	if err != nil {
		return
	}
	want := []string{"short_description", "release_id", "license", "media_license", "dev_state", "supports_all_games",
		"min_engine", "max_engine", "repo", "tags", "created_at", "approved_at", "latest_release_at"}
	var sel []string
	for _, c := range want {
		if cols[c] {
			sel = append(sel, c)
		} else {
			sel = append(sel, "NULL")
		}
	}
	idx.hasTags = cols["tags"]
	rows, err := idx.conn.Query("SELECT id, " + strings.Join(sel, ", ") + " FROM package")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var short, license, media, dev, minE, maxE, repo, tags, created, approved, latest sql.NullString
		var rel sql.NullInt64
		var all sql.NullInt64
		if rows.Scan(&id, &short, &rel, &license, &media, &dev, &all, &minE, &maxE, &repo, &tags, &created, &approved, &latest) != nil {
			continue
		}
		d := Details{ShortDescription: short.String, ReleaseID: rel.Int64, License: license.String, MediaLicense: media.String,
			DevState: strings.ToLower(dev.String), SupportsAllGames: all.Int64 == 1, MinEngine: minE.String, MaxEngine: maxE.String,
			Repo: repo.String, CreatedAt: created.String, ApprovedAt: approved.String, LatestReleaseAt: latest.String}
		if tags.Valid {
			_ = json.Unmarshal([]byte(tags.String), &d.Tags)
		}
		idx.details[id] = d
	}
}
