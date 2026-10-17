package contentdb

// ShortPackage is a row of /api/packages/ (the fields the in-game client relies on).
type ShortPackage struct {
	Author           string   `json:"author"`
	Name             string   `json:"name"`
	Title            string   `json:"title"`
	ShortDescription string   `json:"short_description"`
	Type             string   `json:"type"` // mod | game | txp
	Release          int64    `json:"release"`
	Thumbnail        string   `json:"thumbnail,omitempty"`
	Aliases          []string `json:"aliases,omitempty"`
}

// Key is lower(author)/name, Luanti's identity for a package.
func (p ShortPackage) Key() string { return key(p.Author, p.Name) }

// Detail is /api/packages/<author>/<name>/ (the fields Luanthetti uses; the rest is ignored).
type Detail struct {
	Author           string        `json:"author"`
	Name             string        `json:"name"`
	Title            string        `json:"title"`
	ShortDescription string        `json:"short_description"`
	LongDescription  string        `json:"long_description,omitempty"`
	Type             string        `json:"type"`
	State            string        `json:"state,omitempty"`
	DevState         string        `json:"dev_state,omitempty"`
	AIDisclosure     string        `json:"ai_disclosure,omitempty"`
	Maintainers      []string      `json:"maintainers,omitempty"`
	CreatedAt        string        `json:"created_at,omitempty"`
	ApprovedAt       string        `json:"approved_at,omitempty"`
	License          string        `json:"license,omitempty"`
	MediaLicense     string        `json:"media_license,omitempty"`
	Repo             string        `json:"repo,omitempty"`
	Website          string        `json:"website,omitempty"`
	IssueTracker     string        `json:"issue_tracker,omitempty"`
	Forums           any           `json:"forums,omitempty"`
	Tags             []string      `json:"tags"`
	ContentWarnings  []string      `json:"content_warnings"`
	Release          int64         `json:"release"`
	Score            float64       `json:"score"`
	Downloads        int64         `json:"downloads"`
	Thumbnail        string        `json:"thumbnail,omitempty"`
	Screenshots      []string      `json:"screenshots"`
	URL              string        `json:"url,omitempty"`
	Provides         []string      `json:"provides,omitempty"`
	GameSupport      []GameSupport `json:"game_support,omitempty"`
	SupportsAllGames bool          `json:"supports_all_games"`
}

// Key is lower(author)/name.
func (d *Detail) Key() string { return key(d.Author, d.Name) }

// GameSupport is one entry of a package's game_support list.
type GameSupport struct {
	Supports   bool `json:"supports"`
	Confidence int  `json:"confidence"`
	Game       struct {
		Author string `json:"author"`
		Name   string `json:"name"`
	} `json:"game"`
}

// Release is a row of /api/packages/<author>/<name>/releases/.
type Release struct {
	ID          int64          `json:"id"`
	Title       string         `json:"title"`
	Name        string         `json:"name,omitempty"` // the version string
	ReleaseDate string         `json:"release_date"`
	URL         string         `json:"url"`
	Commit      string         `json:"commit,omitempty"`
	Downloads   int64          `json:"downloads,omitempty"`
	MinEngine   *EngineVersion `json:"min_minetest_version"`
	MaxEngine   *EngineVersion `json:"max_minetest_version"`
	Notes       string         `json:"release_notes,omitempty"`
}

// EngineVersion is ContentDB's engine version reference.
type EngineVersion struct {
	Name            string `json:"name"`
	ProtocolVersion int    `json:"protocol_version,omitempty"`
}

// Tag is a row of /api/tags/.
type Tag struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Views       int    `json:"views,omitempty"`
}

func key(author, name string) string {
	b := make([]byte, 0, len(author)+1+len(name))
	for i := 0; i < len(author); i++ {
		c := author[i]
		if 'A' <= c && c <= 'Z' {
			c += 'a' - 'A'
		}
		b = append(b, c)
	}
	b = append(b, '/')
	return string(b) + name
}
