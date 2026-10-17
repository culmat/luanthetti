package depgraph

import "math"

// Summary is ldg's `Pkg.summary()`.
type Summary struct {
	Package   string  `json:"package"`
	Type      string  `json:"type"`
	Title     string  `json:"title"`
	Score     float64 `json:"score"`
	Downloads int64   `json:"downloads"`
}

// InstallEntry is one package the resolver would install.
type InstallEntry struct {
	Package      string   `json:"package"`
	For          []string `json:"for"`
	NeededBy     string   `json:"needed_by"`
	Alternatives int      `json:"alternatives"`
}

// Candidate is a provider that can't be used, with the reason.
type Candidate struct {
	Package string `json:"package"`
	Problem string `json:"problem"`
}

// Missing explains a mod name the resolver can't satisfy.
type Missing struct {
	Modname       string      `json:"modname"`
	NeededBy      string      `json:"needed_by"`
	Providers     int         `json:"providers"`
	Candidates    []Candidate `json:"candidates"`
	OnlyInGames   []string    `json:"only_in_games"`
	GameProviders int         `json:"game_providers"`
}

// Shadowed is a mod name the target or an install replaces in the game.
type Shadowed struct {
	Modname string `json:"modname"`
	By      string `json:"by"`
	Note    string `json:"note"`
}

// Resolution is ldg's `Resolution.as_dict()`.
type Resolution struct {
	Target              string         `json:"target"`
	Game                string         `json:"game"`
	OK                  bool           `json:"ok"`
	FromGame            []string       `json:"from_game"`
	Install             []InstallEntry `json:"install"`
	Missing             []Missing      `json:"missing"`
	Shadowed            []Shadowed     `json:"shadowed"`
	OptionalAvailable   []string       `json:"optional_available"`
	OptionalUnavailable []string       `json:"optional_unavailable"`
	Warnings            []string       `json:"warnings"`
}

// Claim is ContentDB's own game-support statement for a package and game.
type Claim struct {
	Supports   bool `json:"supports"`
	Confidence int  `json:"confidence"`
}

// CompatRow is one game in a compatibility report.
type CompatRow struct {
	Game              string   `json:"game"`
	Title             string   `json:"title"`
	Status            string   `json:"status"` // ok | risky | unsatisfiable
	Missing           []string `json:"missing"`
	Install           []string `json:"install"`
	UndeclaredMissing []string `json:"undeclared_missing"`
	OptionalAvailable []string `json:"optional_available"`
	ContentDBSays     *Claim   `json:"contentdb_says"`
}

// CompatReport is ldg's `compat_report`.
type CompatReport struct {
	Target          string      `json:"target"`
	AnalyzedSources bool        `json:"analyzed_sources"`
	Games           []CompatRow `json:"games"`
}

// Ref is a static-analysis reference from a mod's code to another mod name.
type Ref struct {
	Mod        string
	Modname    string
	Kind       string // global | itemstring | modpath_guard
	Symbol     string
	Count      int
	File       string
	Line       int
	Declared   bool
	Transitive bool
	Guarded    bool
	Requires   []string
}

// ProvidersReport is ldg's `providers`.
type ProvidersReport struct {
	Modname string    `json:"modname"`
	Count   int       `json:"count"`
	Mods    []Summary `json:"mods"`
	Games   []Summary `json:"games"`
	Note    *string   `json:"note"`
}

// SearchHit is one row of ldg's `search`.
type SearchHit struct {
	Summary
	Provides []string `json:"provides"`
}

// NameCheck is ldg's `name_check`.
type NameCheck struct {
	Modname        string   `json:"modname"`
	Valid          bool     `json:"valid"`
	Available      bool     `json:"available"`
	UsedBy         []string `json:"used_by"`
	BundledInGames []string `json:"bundled_in_games"`
	Policy         string   `json:"policy"`
}

func round1(x float64) float64 { return math.Round(x*10) / 10 }

func strs(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}
