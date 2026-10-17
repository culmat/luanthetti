package app

import (
	"time"

	"github.com/culmat/luanthetti/internal/depgraph"
	"github.com/culmat/luanthetti/internal/installs"
	"github.com/culmat/luanthetti/internal/store"
)

// GameRef is a game present in an installation, mapped to its ContentDB package if known.
type GameRef struct {
	ID    string `json:"id"`
	Key   string `json:"key,omitempty"` // author/name on ContentDB; empty when unknown
	Title string `json:"title,omitempty"`
	pkg   *depgraph.Pkg
}

// InstallView is an installation with what is in it.
type InstallView struct {
	store.Installation
	Games        []GameRef `json:"games"`
	UnknownGames []GameRef `json:"unknown_games"`
	Links        int       `json:"links"`
	Exists       bool      `json:"exists"`
}

// DetectReport is `installs detect`.
type DetectReport struct {
	Candidates []DetectedInstall `json:"candidates"`
	Added      []string          `json:"added"`
}

// DetectedInstall is a candidate, with its registration if any.
type DetectedInstall struct {
	installs.Candidate
	Registered string `json:"registered,omitempty"` // name, if already registered
}

// GameStatus is a package's compatibility with one game of an installation.
type GameStatus struct {
	Game    string   `json:"game"`
	Title   string   `json:"title,omitempty"`
	Status  string   `json:"status"` // ok | risky | unsatisfiable | unknown
	Missing []string `json:"missing,omitempty"`
	Install []string `json:"install,omitempty"`
	Risky   []string `json:"undeclared_missing,omitempty"`
}

// PlanItem is one package an operation will place into an installation.
type PlanItem struct {
	Package    string   `json:"package"` // author/name
	Type       string   `json:"type"`
	Folder     string   `json:"folder"`
	Release    int64    `json:"release"`
	Reason     string   `json:"reason"` // explicit | dependency
	RequiredBy []string `json:"required_by,omitempty"`
	Path       string   `json:"path,omitempty"`
	LinkKind   string   `json:"link_kind,omitempty"`
	Status     string   `json:"status"` // planned | installed | present | skipped | failed
	Note       string   `json:"note,omitempty"`
}

// InstallResult is what happened in one installation.
type InstallResult struct {
	Installation string       `json:"installation"`
	Games        []GameStatus `json:"games"`
	Items        []PlanItem   `json:"items"`
	Warnings     []string     `json:"warnings"`
	Skipped      string       `json:"skipped,omitempty"`
}

// InstallReport is `install`.
type InstallReport struct {
	DryRun  bool            `json:"dry_run"`
	Results []InstallResult `json:"results"`
}

// RemoveReport is `remove`.
type RemoveReport struct {
	Removed  []RemovedLink `json:"removed"`
	Library  []string      `json:"library_removed"`
	Warnings []string      `json:"warnings"`
}

// RemovedLink is one link removed.
type RemovedLink struct {
	Installation string `json:"installation"`
	Package      string `json:"package"`
	Path         string `json:"path"`
	Reason       string `json:"reason"`
}

// ListRow is one linked package.
type ListRow struct {
	Installation string   `json:"installation"`
	Package      string   `json:"package"`
	Type         string   `json:"type"`
	FolderType   string   `json:"folder_type"`
	Folder       string   `json:"folder"`
	Release      int64    `json:"release"`
	ReleaseTitle string   `json:"release_title,omitempty"`
	Kind         string   `json:"link_kind"`
	Reason       string   `json:"reason"`
	RequiredBy   []string `json:"required_by,omitempty"`
	Pinned       bool     `json:"pinned"`
	Latest       int64    `json:"latest,omitempty"`
	Outdated     bool     `json:"outdated"`
	State        string   `json:"state"` // ok | missing | replaced | broken | wrong_target
	Path         string   `json:"path"`
}

// LibraryRow is one package in the library.
type LibraryRow struct {
	Package  string           `json:"package"`
	Type     string           `json:"type"`
	Source   string           `json:"source"`
	Versions []LibraryVersion `json:"versions"`
}

// LibraryVersion is one stored release.
type LibraryVersion struct {
	Release int64  `json:"release"`
	Title   string `json:"title,omitempty"`
	Links   int    `json:"links"`
	Origin  string `json:"origin"`
	Path    string `json:"path"`
}

// ListReport is `list`.
type ListReport struct {
	Rows    []ListRow    `json:"rows"`
	Library []LibraryRow `json:"library,omitempty"`
}

// InfoReport is `info`.
type InfoReport struct {
	Package          string               `json:"package"`
	Type             string               `json:"type"`
	Title            string               `json:"title"`
	ShortDescription string               `json:"short_description"`
	Provides         []string             `json:"provides"`
	Depends          []string             `json:"depends"`
	OptionalDepends  []string             `json:"optional_depends"`
	Score            float64              `json:"score"`
	Downloads        int64                `json:"downloads"`
	Details          depgraph.Details     `json:"details"`
	Releases         []ReleaseInfo        `json:"releases"`
	Compat           []depgraph.CompatRow `json:"compat"`
	AnalyzedSources  bool                 `json:"analyzed_sources"`
	Installed        []ListRow            `json:"installed"`
	URL              string               `json:"url"`
	Thumbnail        string               `json:"thumbnail,omitempty"`
	Screenshots      []string             `json:"screenshots"`
	Source           string               `json:"source"` // live | cache | snapshot
	SnapshotBuiltAt  time.Time            `json:"snapshot_built_at"`
	Notes            []string             `json:"notes"`
}

// ReleaseInfo is one ContentDB release.
type ReleaseInfo struct {
	ID        int64  `json:"id"`
	Title     string `json:"title"`
	Date      string `json:"date"`
	MinEngine string `json:"min_engine,omitempty"`
	MaxEngine string `json:"max_engine,omitempty"`
}

// SearchQuery filters `search`.
type SearchQuery struct {
	Q              string        `json:"q"`
	Type           string        `json:"type,omitempty"` // mod | game | txp
	Tags           []string      `json:"tags,omitempty"`
	DevStates      []string      `json:"dev_states,omitempty"`
	MinDownloads   int64         `json:"min_downloads,omitempty"`
	MinScore       float64       `json:"min_score,omitempty"`
	UpdatedWithin  time.Duration `json:"updated_within,omitempty"`
	CompatibleWith []string      `json:"compatible_with,omitempty"` // installation names or game names
	Sort           string        `json:"sort,omitempty"`            // score | downloads | updated | name
	Limit          int           `json:"limit,omitempty"`
	Thumbnails     bool          `json:"-"`
}

// SearchHit is one result.
type SearchHit struct {
	Package          string            `json:"package"`
	Type             string            `json:"type"`
	Title            string            `json:"title"`
	ShortDescription string            `json:"short_description"`
	Score            float64           `json:"score"`
	Downloads        int64             `json:"downloads"`
	DevState         string            `json:"dev_state,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	LatestReleaseAt  string            `json:"latest_release_at,omitempty"`
	Release          int64             `json:"release,omitempty"`
	Thumbnail        string            `json:"thumbnail,omitempty"`
	Compat           map[string]string `json:"compat"` // game key → ok | risky | unsatisfiable
	InstalledIn      []string          `json:"installed_in"`
}

// SearchReport is `search`.
type SearchReport struct {
	Hits     []SearchHit `json:"hits"`
	Total    int         `json:"total"`
	Games    []GameRef   `json:"games"`
	Notes    []string    `json:"notes"`
	Degraded bool        `json:"degraded"`
}

// UpdateItem is one package that is (or was) out of date in an installation.
type UpdateItem struct {
	Installation string       `json:"installation"`
	Package      string       `json:"package"`
	Folder       string       `json:"folder"`
	Current      int64        `json:"current"`
	Latest       int64        `json:"latest"`
	Status       string       `json:"status"` // outdated | updated | pinned | failed | excluded
	Note         string       `json:"note,omitempty"`
	NewDeps      []string     `json:"new_dependencies,omitempty"`
	Games        []GameStatus `json:"games,omitempty"`
}

// UpdateReport is `update`.
type UpdateReport struct {
	Check    bool         `json:"check"`
	DryRun   bool         `json:"dry_run"`
	Items    []UpdateItem `json:"items"`
	Pruned   []string     `json:"pruned"`
	Warnings []string     `json:"warnings"`
}

// ScanReport is `scan`.
type ScanReport struct {
	Installation string             `json:"installation"`
	Results      []store.ScanResult `json:"results"`
	Counts       map[string]int     `json:"counts"`
}

// AdoptReport is `adopt`.
type AdoptReport struct {
	DryRun  bool        `json:"dry_run"`
	Adopted []AdoptItem `json:"adopted"`
	Skipped []AdoptItem `json:"skipped"`
}

// AdoptItem is one directory brought under management (or not).
type AdoptItem struct {
	Installation string `json:"installation"`
	Path         string `json:"path"`
	Package      string `json:"package,omitempty"`
	Release      int64  `json:"release,omitempty"`
	Action       string `json:"action"` // moved | linked_existing | copy_registered | skip
	Note         string `json:"note,omitempty"`
}

// VerifyReport is `verify` / `repair`.
type VerifyReport struct {
	Problems []Problem `json:"problems"`
	Repaired []Problem `json:"repaired"`
	OK       int       `json:"ok"`
}

// Problem is something wrong with a link or an installation.
type Problem struct {
	Installation string `json:"installation"`
	Path         string `json:"path"`
	Package      string `json:"package,omitempty"`
	Kind         string `json:"kind"` // missing | replaced | broken_link | wrong_target | incomplete | conf_mismatch | foreign_link | missing_dependency | pending_replace
	Detail       string `json:"detail"`
	Fix          string `json:"fix,omitempty"`
}
