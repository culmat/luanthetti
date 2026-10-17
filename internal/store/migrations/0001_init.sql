CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);

-- A ContentDB package (or a local package) known to the library.
CREATE TABLE packages (
    id INTEGER PRIMARY KEY,
    author TEXT NOT NULL,
    name TEXT NOT NULL,
    type TEXT NOT NULL,                       -- mod | game | txp (ContentDB types)
    key TEXT GENERATED ALWAYS AS (lower(author) || '/' || name) STORED UNIQUE,
    title TEXT NOT NULL DEFAULT '',
    short_description TEXT NOT NULL DEFAULT '',
    source TEXT NOT NULL DEFAULT 'contentdb', -- contentdb | local
    folder_type TEXT NOT NULL,                -- mod | modpack | game | txp
    folder TEXT NOT NULL                      -- directory name Luanti sees (package.name)
);

-- One downloaded (or adopted) release of a package.
CREATE TABLE versions (
    id INTEGER PRIMARY KEY,
    package_id INTEGER NOT NULL REFERENCES packages (id) ON DELETE CASCADE,
    release_id INTEGER NOT NULL,
    path TEXT NOT NULL,                       -- the Luanti-ready folder inside the library
    title TEXT NOT NULL DEFAULT '',           -- release title / version string
    min_engine TEXT NOT NULL DEFAULT '',
    max_engine TEXT NOT NULL DEFAULT '',
    release_date TEXT NOT NULL DEFAULT '',
    origin TEXT NOT NULL,                     -- download | adopted | local
    sha256 TEXT NOT NULL DEFAULT '',
    added_at TEXT NOT NULL,
    complete INTEGER NOT NULL DEFAULT 0,
    UNIQUE (package_id, release_id)
);

-- A Luanti installation (user path, server root, RUN_IN_PLACE build).
CREATE TABLE installations (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    root TEXT NOT NULL UNIQUE,
    kind TEXT NOT NULL,                       -- user | run_in_place | server | flatpak | docker
    engine_version TEXT NOT NULL DEFAULT '',
    protocol INTEGER NOT NULL DEFAULT 0,
    engine_source TEXT NOT NULL DEFAULT '',   -- binary | table | manual | unknown
    binary_path TEXT NOT NULL DEFAULT '',
    extra_game_dirs TEXT NOT NULL DEFAULT '[]', -- JSON array
    link_mode TEXT NOT NULL DEFAULT 'auto',   -- auto | symlink | junction | copy | env
    added_at TEXT NOT NULL,
    last_scan_at TEXT NOT NULL DEFAULT ''
);

-- A package version placed into an installation.
CREATE TABLE links (
    id INTEGER PRIMARY KEY,
    installation_id INTEGER NOT NULL REFERENCES installations (id) ON DELETE CASCADE,
    package_id INTEGER NOT NULL REFERENCES packages (id),
    version_id INTEGER NOT NULL REFERENCES versions (id),
    path TEXT NOT NULL,                       -- absolute path inside the installation
    kind TEXT NOT NULL,                       -- symlink | junction | copy | env
    reason TEXT NOT NULL,                     -- explicit | dependency | adopted
    required_by TEXT NOT NULL DEFAULT '[]',   -- JSON array of package keys
    pending_replace TEXT NOT NULL DEFAULT '', -- version path mid-replacement (Windows), finished by repair
    created_at TEXT NOT NULL,
    UNIQUE (installation_id, path)
);

CREATE TABLE pins (
    installation_id INTEGER NOT NULL REFERENCES installations (id) ON DELETE CASCADE,
    package_id INTEGER NOT NULL REFERENCES packages (id) ON DELETE CASCADE,
    release_id INTEGER,                       -- NULL: hold whatever is linked
    PRIMARY KEY (installation_id, package_id)
);

CREATE TABLE scan_results (
    id INTEGER PRIMARY KEY,
    installation_id INTEGER NOT NULL REFERENCES installations (id) ON DELETE CASCADE,
    scanned_at TEXT NOT NULL,
    path TEXT NOT NULL,
    folder_type TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL,                     -- managed | adoptable | unmanaged | git | foreign_link | broken_link | replaced
    author TEXT NOT NULL DEFAULT '',
    name TEXT NOT NULL DEFAULT '',
    release_id INTEGER NOT NULL DEFAULT 0,
    detail TEXT NOT NULL DEFAULT ''
);

-- ContentDB JSON caches, keyed by release id where the data is per release.
CREATE TABLE cdb_packages (
    key TEXT PRIMARY KEY,                     -- lower(author)/name
    release_id INTEGER NOT NULL,
    fetched_at TEXT NOT NULL,
    detail_json BLOB NOT NULL,
    releases_json BLOB NOT NULL DEFAULT ''
);
CREATE TABLE cdb_blobs (
    key TEXT PRIMARY KEY,                     -- tags | updates:<engine>:<protocol> | search:<hash>
    fetched_at TEXT NOT NULL,
    json BLOB NOT NULL
);

CREATE TABLE jobs (
    id TEXT PRIMARY KEY,
    kind TEXT NOT NULL,
    state TEXT NOT NULL,
    started_at TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT '',
    error TEXT NOT NULL DEFAULT '',
    json BLOB NOT NULL
);

CREATE INDEX links_package ON links (package_id);
CREATE INDEX links_version ON links (version_id);
CREATE INDEX versions_package ON versions (package_id);
CREATE INDEX scan_installation ON scan_results (installation_id);
