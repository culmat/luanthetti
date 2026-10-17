# Content Manager Research: Existing Projects & Discussions

This document compiles research on existing Minetest/Luanti content management tools, package managers, and related discussions that inform the design of **Luanthetti**.

*Revised 2026-10-17.* The first draft assumed the in-game installer duplicates mods per game. It does not: Luanti has one `mods/` folder per installation and worlds pick mods in `world.mt` (see [How Luanti installs content](#how-luanti-installs-content)). Entries below are corrected accordingly, and two tools found later, **luma** and **luctl**, are added.

---

## Table of Contents

0. [How Luanti installs content](#how-luanti-installs-content)
1. [Existing Projects](#existing-projects)
2. [Relevant Discussions](#relevant-discussions)
3. [Key Lessons Learned](#key-lessons-learned)
4. [Feature Comparison](#feature-comparison)
5. [Design Inspirations](#design-inspirations)
6. [Gaps & Opportunities](#gaps--opportunities)

---

## How Luanti installs content

Verified in the engine source (`builtin/mainmenu/content/dlg_install.lua`, `contentdb.lua`, `pkgmgr.lua`, `update_detector.lua`; `doc/lua_api.md`; `doc/world_format.md`):

- A Luanti **installation** (user path: `~/.minetest`, `~/Library/Application Support/minetest`, `%APPDATA%\Luanti`, or the build directory for `RUN_IN_PLACE` builds) has `mods/`, `games/`, `textures/` and `worlds/`.
- Mods load from `games/<gameid>/mods/`, `mods/` and `worlds/<world>/worldmods/`. The user `mods/` folder is shared by all games.
- The in-game ContentDB installer writes every mod into `core.get_modpath()`, that single `mods/` folder. Games go to `games/`, texture packs to `textures/`.
- Each world chooses its mods through `load_mod_<name> = true|false|mods/<modpack>/<dir>` in `world.mt`. Absolute paths are allowed only for folders listed in the `LUANTI_MOD_PATH` environment variable.
- The "base game" dropdown (website and client) is a **compatibility filter**. It does not change the install location.
- A ContentDB install is recognised by `author` and `release` in the package `.conf`; the client writes those (plus `title`, `name`, `description` when absent) after extracting. Updates are detected by comparing `release` with `/api/updates/`.

Consequence: within one installation nothing is duplicated across games. Duplication and update tedium appear across **installations** (desktop, servers, Docker roots). That is Luanthetti's scope.

---

## Existing Projects

### 0. luma (luanti-org, official)

**Repository**: [https://github.com/luanti-org/luma](https://github.com/luanti-org/luma)
**Language**: Go
**Status**: Active, work in progress (created 2026-09-26, commits on 2026-10-17)
**License**: MIT
**Docs**: [docs.luanti.org/for-server-hosts/luma](https://docs.luanti.org/for-server-hosts/luma/)

#### Overview

A terminal UI and command line (`luma <noun> <verb>`) for one Luanti installation (`--dir`, `--flatpak`). Lists installed mods, games and texture packs with their release numbers, checks ContentDB for newer releases, updates them, and installs missing hard dependencies through the ContentDB API with the same provider preference as the client (package named like the mod, else the first ContentDB lists). Single static binaries for Linux, macOS and Windows.

#### Roadmap (from `ROADMAP.md`)

Search and install from ContentDB, an installed-content record (where a mod came from), optional git mode, per-world enable/disable in `world.mt`, a mod settings editor, world backups.

#### Strengths

✅ Official, single binary, TUI and CLI
✅ Dependency installation with the client's rules
✅ Respects the engine's conventions (`author`/`release` in `.conf`, `/api/updates/`)

#### Limitations (relative to Luanthetti's goals)

❌ One installation at a time, no shared library
❌ Compatibility is "declared", not resolved per game; no undeclared-dependency analysis
❌ No discovery filters beyond ContentDB's own
❌ luanti-org repositories do not accept AI-written contributions, so improvements cannot be sent there by an agent

**Decision:** Luanthetti complements luma. It covers several installations, resolved compatibility via luanti-depgraph, discovery and scan/adopt, and stays interoperable (same `.conf` keys, same update detection).

### 0b. luctl (by brylie)

**Repository**: [https://github.com/brylie/luctl](https://github.com/brylie/luctl)
**Language**: Go
**Status**: Low activity (last push 2026-06)
**License**: MIT

A server-side CLI with a declarative `luanti.toml` manifest (mods, config keys, paths), `world.mt` enable/disable, config sync and S3 backups. Package install/update/search through the ContentDB API, no dependency resolution, one server at a time.

### 0c. luanti-depgraph (`ldg`, culmat)

**Repository**: [https://github.com/culmat/luanti-depgraph](https://github.com/culmat/luanti-depgraph)
**Language**: Python
**License**: MIT

The dependency graph Luanthetti builds on: every ContentDB package with provided and required mod names, declared game support, static-analysis references for the most-used packages, and a resolver that answers "can X run in game G, and what has to be installed". Publishes a nightly SQLite snapshot and an explorer site. Luanthetti consumes the snapshot and ports the resolver to Go.

### 1. minetools (by ronoaldo)

**Repository**: [https://github.com/ronoaldo/minetools](https://github.com/ronoaldo/minetools)  
**Language**: Go  
**Status**: Active (recent commits in 2024)  
**License**: MIT

#### Overview

A collection of CLI utilities for managing Minetest servers from the command line. Includes a dedicated `contentdb` command for interacting with ContentDB.

#### Features

- **contentdb command**: Download mods from ContentDB and install to local mods folder
- **Pre-compiled binaries**: Available for Linux (amd64, arm64, 386)
- **Server-focused**: Designed for server management workflows
- **Simple workflow**: Single command to install a mod to current directory

#### Example Usage

```bash
# Download and install a mod to current directory's mods folder
contentdb install TenPlus1/ethereal

# Update existing mod
contentdb update TenPlus1/ethereal
```

#### Implementation Details

- Written in Go (fast, portable, single binary)
- Uses ContentDB API directly
- Designed to be embedded in Docker images for server deployments
- Part of the [luantiserver](https://github.com/ronoaldo/luantiserver) ecosystem

#### Strengths

✅ Simple, focused CLI tool  
✅ No complex dependencies  
✅ Server-oriented (good for headless use)  
✅ Integrates well with automation  
✅ Maintains filesystem structure Minetest expects  

#### Limitations

❌ No multi-installation management  
❌ No dependency resolution  
❌ No global library/concept  
❌ No enable/disable toggles  
❌ Requires manual invocation per mod  

---

### 2. minetest_mods_manager (by brtkuu)

**Repository**: [https://github.com/brtkuu/minetest_mods_manager](https://github.com/brtkuu/minetest_mods_manager)  
**npm Package**: [https://www.npmjs.com/package/minetest_mod_manager](https://www.npmjs.com/package/minetest_mod_manager)  
**Language**: JavaScript/Node.js  
**Status**: Inactive (last update ~4 years ago, 1 download/week)  
**License**: MIT

#### Overview

A Node.js-based command-line tool for managing Minetest content (mods, games, texture packs) from ContentDB.

#### Features

- **Search**: `minetest-manager search -q <query> -t <type> -h <hide>`
- **Download**: `minetest-manager download -u <author> -n <name> -t <type>`
- **API Token Support**: Uses ContentDB API tokens for authenticated actions
- **Content Flags**: Respects ContentDB content flags
- **Multiple Package Types**: Supports mods, games, texture packs

#### Example Usage

```bash
# Search for mods
minetest-manager search -q "dig" -t mod

# Download and install a mod
minetest-manager download -u Wuzza -n digilines -t mod

# With API token (for private packages?)
# Create token.txt in minetest folder with your API token
minetest-manager download -u author -n package -t mod
```

#### Implementation Details

- Uses ContentDB REST API
- Requires Node.js runtime
- Installs to current working directory's mods folder
- Supports ContentDB's content flags system

#### Strengths

✅ Cross-platform (Node.js)  
✅ Supports all ContentDB content types  
✅ API token integration  
✅ Search functionality  

#### Limitations

❌ Abandoned (inactive maintenance)  
❌ Very low adoption (3 downloads/week)  
❌ No dependency resolution  
❌ No per-game management  
❌ No global library  
❌ Requires Node.js installation  

---

### 3. 3m - Minetest Mod Manager

**Forum Thread**: [https://forum.minetest.net/viewtopic.php?id=2972](https://forum.minetest.net/viewtopic.php?id=2972)  
**Status**: Older project (2014 era)  
**Platform**: Likely Windows-based

#### Overview

An early GUI mod manager for Minetest. Appears to be one of the first attempts at a dedicated mod management tool.

#### Features (from forum description)

- GUI interface
- Mod installation
- Mod management
- Possibly dependency handling

#### Status

This appears to be an older project. The forum thread is from 2014, and there's no clear indication it's still maintained. However, it represents early thinking about mod management UX.

#### Key Insight

The discussion reveals early user demand for:
- Better mod discovery
- Easier installation
- Dependency management
- Version tracking

---

### 4. minetest-bower

**Forum Thread**: [https://forum.minetest.net/viewtopic.php?f=14&t=13012](https://forum.minetest.net/viewtopic.php?f=14&t=13012)

#### Overview

A package manager inspired by npm/bower, designed specifically for Minetest mods. Uses Git repositories.

#### Key Features

- Git-based mod management
- Dependency resolution
- Version management
- "Screenshots, descriptions" support

#### Status

Appears to be a proposal/work-in-progress. The thread discusses the concept and has some implementation details.

#### Design Philosophy

The author recognized that Minetest needed a proper package manager and looked to existing ecosystems (npm, bower) for inspiration.

---

### 5. Minetest Package Manager [WIP]

**Forum Thread**: [https://forum.minetest.net/viewtopic.php?f=14&t=13020](https://forum.minetest.net/viewtopic.php?f=14&t=13020)

#### Overview

Another attempt at a package manager for Minetest content. Described as "work in progress."

#### Key Discussion Points

- Multi-repository support
- APT-like interface
- Automatic dependency resolution
- Server and client support

---

### 6. Luanti's Built-in ContentDB Integration

**Documentation**: [https://content.luanti.org/help/installing/](https://content.luanti.org/help/installing/)  
**Issue**: [https://github.com/luanti-org/luanti/issues/8891](https://github.com/luanti-org/luanti/issues/8891)

#### Overview

Luanti (formerly Minetest) has built-in ContentDB integration with dependency installation capabilities.

#### Current State

- Can browse ContentDB from in-game menu
- Can install mods, games, texture packs
- **Dependency installation**: When installing a mod from ContentDB, Luanti attempts to download required dependencies automatically
- Shows dependency information dialog before installation
- Respects ContentDB content flags

#### Key Feature: Dependency Installation

From issue #8891:
- When user decides to install a package, Luanti queries ContentDB API for dependencies
- API returns list of dependencies and packages that fulfill them
- A dialog shows what will be installed
- User can proceed with installation

#### Implementation

The dependency resolution is handled client-side by Luanti, using ContentDB's API to fetch dependency information.

#### Strengths

✅ Native integration  
✅ Dependency resolution built-in  
✅ Good UX for dependency handling  
✅ Works for all content types  

#### Limitations

❌ **One installation only**: content lives in that installation's `mods/`; a second Luanti installation (server, Docker, dev build) starts from scratch
❌ **Declared compatibility only**: the "base game" filter shows what a package declares or ContentDB detects, not whether its dependencies resolve or whether it uses undeclared APIs
❌ **GUI only**: nothing for headless servers
❌ **The "base game dropdown" confusion**: the dropdown is a filter, but reads like an install target. (Corrected from the first draft, which assumed it chose a per-game install location.)

---

### 7. minetest-mods Organization

**GitHub**: [https://github.com/minetest-mods](https://github.com/minetest-mods)  
**Website**: [https://minetest-mods.github.io/mods.html](https://minetest-mods.github.io/mods.html)

#### Overview

A GitHub organization hosting many Minetest mods. Represents an alternative content distribution model to ContentDB.

#### Key Insight

There are **multiple content distribution channels** for Minetest:
- ContentDB (official, curated)
- GitHub (decentralized, developer-controlled)
- minetest-mods (organized, community)

A good content manager should support **multiple sources**, not just ContentDB.

---

## Relevant Discussions

### 1. "Package manager / repo for mods?" (Forum Thread)

**URL**: [https://forum.minetest.net/viewtopic.php?t=15145](https://forum.minetest.net/viewtopic.php?t=15145)

#### Key Points

- Users want easier mod management
- Discussion of whether to use existing package managers (apt, npm) vs. building Minetest-specific tool
- Recognition that Minetest's content structure is different from traditional software packages

#### Quote

> "What I'd like is a way to easily manage which mods I have installed... and be able to easily add/remove/mods and see what versions are available and installed."

---

### 2. "APT Repository for Mods" (Forum Thread)

**URL**: [https://forum.minetest.net/viewtopic.php?t=13051&start=25](https://forum.minetest.net/viewtopic.php?t=13051&start=25)

#### Key Points

- Discussion of creating a Debian/Ubuntu APT repository for Minetest mods
- Would allow `apt install minetest-mod-some-mod`
- Benefits: leverages existing system package management
- Drawbacks: doesn't handle Minetest-specific dependency types well

#### Insight

Users want **system integration**. A good Minetest content manager should ideally integrate with system package managers or at least provide similar UX.

---

## Feature Comparison

| Feature | minetools | minetest_mods_manager | luctl | Luanti Built-in | luma | Luanthetti |
|---------|-----------|------------------------|-------|-----------------|------|------------|
| **CLI** | ✅ | ✅ | ✅ | ❌ | ✅ | ✅ |
| **GUI / TUI** | ❌ | ❌ | ❌ | GUI | TUI | web UI |
| **ContentDB Integration** | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ |
| **Multiple Sources** | ❌ | ❌ | ❌ | ❌ | planned (git) | later |
| **Dependency Resolution** | ❌ | ❌ | ❌ | ✅ | ✅ | ✅ (depgraph) |
| **Compatibility per game** | ❌ | ❌ | ❌ | declared | declared | resolved: ok / risky / unsatisfiable |
| **Several installations** | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| **Shared Library** | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| **Update check** | ✅ | ❌ | ✅ | ✅ | ✅ | ✅ |
| **Version Pinning / Rollback** | ❌ | ❌ | ❌ | ❌ | ❌ | ✅ |
| **World enable/disable** | ❌ | ❌ | ✅ | ✅ | planned | ❌ (by design) |
| **Discovery filters** | ❌ | ❌ | ❌ | ContentDB's | ❌ | ✅ |
| **Maintenance Status** | ✅ Active | ❌ Inactive | ⚠️ Low | ✅ Active | ✅ Active | ⚠️ New |

---

## Key Lessons Learned

### 1. The "Base Game Dropdown" Problem is Real

**Evidence**: 
- User complaints in Luanti issue #8891 and forum threads
- The current Luanti implementation shows dependency dialogs but still requires game selection

**Root Cause (corrected)**: the dropdown is a compatibility filter that reads like an install target. Luanti itself has one `mods/` folder per installation, so there is no per-game duplication to solve.

**Solution Direction**: show compatibility as a resolved per-game status (what luanti-depgraph computes) instead of a dropdown, and solve the real duplication, which is across installations.

### 2. Server vs. Client Needs Differ

**Evidence**: 
- `minetools` is server-focused (CLI, automation-friendly)
- Luanti built-in is client-focused (GUI, in-game)
- Different workflows need different tools

**Insight**: Luanthetti should support **both**:
- CLI for server administrators
- TUI/optional GUI for desktop users

### 3. Multiple Content Sources Exist

**Evidence**:
- ContentDB (official)
- GitHub (decentralized)
- minetest-mods (organized)
- Git repositories (direct)

**Insight**: Luanthetti should be **source-agnostic**. Users should be able to:
```bash
luanthetti install contentdb:Wuzza/digilines
luanthetti install github:minetest-mods/digilines
luanthetti install git:https://gitlab.com/some/mod.git
```

### 4. Dependency Resolution is Table Stakes

**Evidence**:
- Luanti built-in has it
- minetest-bower emphasized it
- Users expect it

**Insight**: Using **luanti-depgraph** is the right choice - it's already the dependency resolution engine for the ecosystem.

### 5. Single Binary/Simple Installation Wins

**Evidence**:
- `minetools` (Go binary) - easy to install and use
- `minetest_mods_manager` (Node.js) - requires Node.js, low adoption

**Insight**: Luanthetti should have **minimal dependencies**. Options:
- Go (like minetools) - single binary, cross-platform
- Python (like ContentDB) - but requires Python runtime
- Rust - single binary, but more complex build

### 6. The "All" View is Confusing

**Evidence**: Your own observation

**Insight**: Content types (games, mods, texture packs) should be **separated by default**, with a smart "All" view that respects context.

### 7. Update Management is Important

**Evidence**: 
- Users want to know when updates are available
- Server admins need predictable update workflows

**Insight**: Auto-update should be **configurable**:
- Conservative: Only update if compatible with ALL installed games
- Per-Game: Update only in compatible games, skip others
- Aggressive: Update always, handle conflicts manually

---

## Design Inspirations

### From minetools

- **Simple CLI first**: Start with command-line, add GUI later
- **Server-friendly**: Support headless/automated use cases
- **Single command install**: `luanthetti install author/name`

### From Luanti Built-in

- **Dependency dialog**: Show what will be installed before doing it
- **ContentDB API integration**: Use the official API
- **In-game context awareness**: Know which game is currently active

### From npm/bower

- **Package sources**: Support multiple registries
- **Semantic versioning**: Respect version constraints
- **Dependency trees**: Visualize relationships

### From System Package Managers (apt, dnf, etc.)

- **Update checks**: Periodic update checks
- **Conflict resolution**: Handle incompatible updates
- **Rollback capability**: Revert to previous versions

---

## Gaps & Opportunities

### Unmet Needs (Opportunities for Luanthetti)

| Need | Current State | Opportunity |
|------|--------------|------------|
| Library shared across installations | ❌ Not available | ✅ Core feature |
| Install into every installation at once | ❌ Per installation | ✅ Default, `--into` restricts |
| Centralized updates | ❌ Per installation (luma: one at a time) | ✅ One command for all |
| Version pinning per installation, rollback | ❌ Not possible | ✅ |
| Resolved compatibility per game | ❌ Declared only | ✅ ok / risky / unsatisfiable via luanti-depgraph |
| Symlink-based (save disk) | ❌ Copies everywhere | ✅ Single install, multiple references |
| Multiple sources | ❌ ContentDB only | ✅ ContentDB + GitHub + Git + local |
| Offline mode | ⚠️ Limited | ✅ Full functionality with cache |
| Conflict visualization | ❌ None | ✅ Show compatibility matrix |

### Technical Gaps to Address

1. **Filesystem Abstraction Layer**
   - Need to handle: symlinks, copies, hard links
   - Need to detect filesystem capabilities
   - Need fallback for systems without symlink support

2. **Version Pinning Configuration**
   - Where to store per-game version pins?
   - How to handle mod-specific config files?

3. **Update Conflict Resolution**
   - Algorithm for handling version conflicts
   - User interface for resolving conflicts
   - Rollback strategy

4. **Multi-Source Integration**
   - Unified interface for different sources
   - Source priority/precedence rules
   - Caching strategy for each source type

---

## Recommendations

### 1. Start with minetools' Simplicity

Begin with a **minetools-like CLI** that adds your key innovations:
```bash
# Install into every registered installation where it works (default)
luanthetti install Wuzza/digilines

# Install into one installation
luanthetti install Wuzza/digilines --into server
```

**Note**: installations, not games, are the targets. Compatibility with the games present in each installation is shown per game and decides which dependencies are pulled in.

### 2. Integrate luanti-depgraph Early

Use it for:
- Compatibility detection
- Dependency resolution
- Conflict detection
- Update checking

This gives you **correctness** from day one.

### 3. Implement the Library

Structure (release ids, not semver, because that is what ContentDB and the client track):
```
<data>/library/mod/Wuzza/digilines/<release_id>/digilines/     one copy per release
<installation>/mods/digilines → <library>/mod/Wuzza/digilines/<release_id>/digilines   (symlink / junction / copy)
```

### 4. Support Multiple Sources from Start

Even if ContentDB is primary:
```bash
luanthetti install contentdb:Wuzza/digilines
luanthetti install github:minetest-mods/digilines
luanthetti install git:https://gitlab.com/user/mod.git
luanthetti install local:/path/to/mod
```

### 5. Make Compatibility the Default

When browsing/searching:
```bash
# Default: show all, but mark compatibility with installed games
luanthetti search "dig"

# Filter to only show packages compatible with specific game
luanthetti search "dig" --game mineclonia

# Show detailed compatibility info
luanthetti search "dig" --compatibility
```

**Note**: compatibility marks are computed for the games present in the user's installations, from luanti-depgraph's resolver, not from declared support alone.

---

## References

### Projects

1. [ronoaldo/minetools](https://github.com/ronoaldo/minetools) - CLI utilities with ContentDB integration
2. [brtkuu/minetest_mods_manager](https://github.com/brtkuu/minetest_mods_manager) - Node.js package manager
3. [minetest_mod_manager (npm)](https://www.npmjs.com/package/minetest_mod_manager) - Similar to above
4. [luanti-org/luanti](https://github.com/luanti-org/luanti) - Official Luanti repository
5. [luanti-org/luma](https://github.com/luanti-org/luma) - Official Go TUI/CLI package manager (one installation)
6. [brylie/luctl](https://github.com/brylie/luctl) - Server CLI with `luanti.toml` manifest
7. [culmat/luanti-depgraph](https://github.com/culmat/luanti-depgraph) - Dependency graph, resolver, nightly snapshot

### Discussions

1. [Luanti Issue #8891: Add dependency installation](https://github.com/luanti-org/luanti/issues/8891)
2. [Forum: Package manager / repo for mods?](https://forum.minetest.net/viewtopic.php?t=15145)
3. [Forum: APT Repository for Mods](https://forum.minetest.net/viewtopic.php?t=13051&start=25)
4. [Forum: 3m - Minetest Mod Manager](https://forum.minetest.net/viewtopic.php?id=2972)
5. [Forum: minetest-bower](https://forum.minetest.net/viewtopic.php?f=14&t=13012)
6. [Forum: Minetest Package Manager [WIP]](https://forum.minetest.net/viewtopic.php?f=14&t=13020)

### Documentation

1. [ContentDB API Documentation](https://content.luanti.org/help/api/)
2. [Luanti Mod Dependency Management](https://docs.luanti.org/for-creators/mod-dependency-management/)
3. [ContentDB Help: Installing](https://content.luanti.org/help/installing/)

---

## Categorization & Filtering Research

A major UX pain point identified: **ContentDB has thousands of packages with limited discovery tools**. Users struggle to find content relevant to their needs.

### Existing Categorization in ContentDB

| Field | Type | Current Usage | Coverage |
|-------|------|--------------|----------|
| `type` | enum | mod, game, texture_pack | ✅ All packages |
| `tags` | list | Free-form tags (material, tools, etc.) | ✅ ~50% packages |
| `dev_state` | enum | WIP, Beta, Actively Developed, etc. | ✅ Most packages |
| `downloads` | integer | Total download count | ✅ All packages |
| `score` | float | Quality score | ✅ All packages |
| `created_at` | date | Creation date | ✅ All packages |
| `approved_at` | date | Approval date | ✅ Approved packages |

### Proposed Filter Dimensions

#### 1. Content Type (✅ Existing)
- **Source**: `type` field
- **Values**: mod, game, texture_pack
- **Implementation**: Simple filter on API

#### 2. Category (✅ Existing via Tags)
- **Source**: `tags` field
- **Existing tags**: material, tools, building, decoration, transportation, armor, weapons, etc.
- **Implementation**: Filter by `tag=` parameter in ContentDB API
- **Example**: `/api/packages/?type=mod&tag=tools`

#### 3. Theme (⚠️ Needs Enrichment)
- **Proposed tags**: `theme:realistic`, `theme:fantasy`, `theme:sci-fi`, `theme:medieval`, `theme:modern`
- **Implementation options**:
  - **A. Crowdsourced**: Add to ContentDB tag system, let community tag
  - **B. Auto-detected**: Analyze descriptions for keywords
  - **C. Hybrid**: Both, with auto-suggestions
- **Recommendation**: Start with crowdsourcing via ContentDB's existing tag infrastructure

#### 4. Maturity (✅ Existing)
- **Source**: `dev_state` field
- **Values**: WIP, Beta, Actively Developed, Maintenance Only, As-Is, Deprecated
- **Implementation**: Filter by `dev_state=` (if API supports it) or client-side filtering

#### 5. Popularity (✅ Existing)
- **Source**: `downloads` field
- **Proposed thresholds**:
  - Popular: > 1000 downloads
  - Well-Known: > 100 downloads
  - New/Unknown: < 100 downloads
- **Implementation**: Client-side threshold application

#### 6. Maintenance Status (✅ Existing)
- **Source**: `created_at`, `approved_at`, last release date
- **Proposed calculation**:
  - Actively Maintained: Release in last 6 months
  - Recently Updated: Release in last 12 months
  - Stable: Approved > 12 months ago, no recent updates
  - New: Approved < 3 months ago
- **Implementation**: Client-side calculation from date fields

#### 7. Compatibility (✅ Via luanti-depgraph)
- **Source**: luanti-depgraph dependency analysis
- **Calculation**: Check which installed games the package supports
- **Implementation**: Query luanti-depgraph with user's game list

### Where Should This Data Live?

| Option | Pros | Cons | Recommendation |
|--------|------|------|----------------|
| **ContentDB** | Source of truth, benefits all users | Requires ContentDB changes | ✅ **Primary** |
| **luanti-depgraph** | Performance, offline cache | Expands scope beyond dependencies | ⚠️ Optional |
| **Luanthetti Local** | Full control, no external deps | Duplicate effort, inconsistent across clients | ✅ **Fallback** |

**Decision**: 
- **Primary storage**: ContentDB (add new tags, expose via API)
- **Cache**: luanti-depgraph can optionally cache for performance
- **Luanthetti**: Can always filter client-side as fallback

**Rationale**: 
- ContentDB already has tag infrastructure
- luanti-depgraph's mission is dependency resolution, not categorization
- Keep concerns separated: metadata in ContentDB, dependencies in depgraph
- Luanthetti can work with whatever is available

### Implementation Phases

**Phase 1: Use Existing Data Only**
- Content type filtering
- Existing tag filtering (categories)
- Maturity from dev_state
- Popularity from downloads
- Maintenance from dates
- Compatibility from luanti-depgraph

**Phase 2: Data Enrichment**
- Add theme tags to ContentDB
- Add quality/composite scores
- Improve tag coverage for existing packages

**Phase 3: Advanced Features**
- Personalized recommendations based on user's installed packages
- Community-curated collections/lists
- Machine learning for auto-tagging

---

## Conclusion

There have been **multiple attempts** at Minetest content management tools, and since September 2026 an official one (luma), but none offer:

1. ✅ A library shared across several installations
2. ✅ Resolved per-game compatibility (ok / risky / unsatisfiable) before installing
3. ✅ Centralised updates with pins and rollback
4. ✅ Discovery filters that include "works in my games"

**Luanthetti has a clear opportunity** to be the first tool that combines:
- The simplicity of `minetools`
- The dependency resolution of Luanti
- The multi-source vision of `minetest-bower`
- Your innovative global library + automatic multi-game distribution model

The **luanti-depgraph** integration is particularly valuable - it provides the dependency resolution engine, allowing you to focus on UX and the global library model.

**Note on per-world control**: Users already have per-world enable/disable via `world.mt`, and luma plans to edit it. Luanthetti focuses on the library, linking, compatibility and discovery.

**Language**: Go, like luma, luctl and minetools: single static binaries for every platform, and the ecosystem's tooling language. luanti-depgraph stays in Python as the data producer; only its resolver is ported and conformance-tested.

---

*Document created: 2026-10-17, revised the same day after verifying the engine's behaviour and finding luma.*
*Status: Research Complete*
*Related: [content_manager_concept.md](./content_manager_concept.md) - The main concept document for **Luanthetti**
*Project Name: **Luanthetti** (Luanti + Spaghetti - because dependencies are entangled like spaghetti)
