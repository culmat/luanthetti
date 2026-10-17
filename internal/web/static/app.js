// Luanthetti web UI: vanilla ES module, no build step, no third-party code.

const token = (() => {
  const m = location.hash.match(/[#&]t=([^&]+)/);
  if (m) {
    sessionStorage.setItem("luanthetti-token", m[1]);
    history.replaceState(null, "", location.pathname + "#/search");
  }
  return sessionStorage.getItem("luanthetti-token") || "";
})();

// --- tiny DOM helper -------------------------------------------------------
function h(tag, attrs, ...children) {
  const el = document.createElement(tag);
  for (const [k, v] of Object.entries(attrs || {})) {
    if (v === undefined || v === null || v === false) continue;
    if (k.startsWith("on")) el.addEventListener(k.slice(2), v);
    else if (k === "class") el.className = v;
    else el.setAttribute(k, v === true ? "" : v);
  }
  for (const c of children.flat(Infinity)) {
    if (c === undefined || c === null || c === false) continue;
    el.append(c instanceof Node ? c : document.createTextNode(String(c)));
  }
  return el;
}
const $ = (sel) => document.querySelector(sel);

// --- API -------------------------------------------------------------------
async function api(path, opts = {}) {
  const res = await fetch(path, {
    method: opts.method || "GET",
    headers: { "X-Luanthetti-Token": token, ...(opts.body ? { "Content-Type": "application/json" } : {}) },
    body: opts.body ? JSON.stringify(opts.body) : undefined,
    signal: opts.signal,
  });
  if (res.status === 204) return null;
  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new Error((data.error && data.error.message) || res.statusText);
  return data;
}
const thumbURL = (url) => `/api/thumb?url=${encodeURIComponent(url)}&t=${encodeURIComponent(token)}`;

// --- state -----------------------------------------------------------------
const state = {
  installs: [],
  tags: [],
  filters: { q: "", type: "mod", tags: [], dev: [], minDownloads: "", within: "", compat: [], sort: "score" },
  limit: 48,
  jobs: new Map(),
};
const DEV_STATES = [
  ["actively_developed", "Actively developed"], ["maintenance_only", "Maintenance only"], ["beta", "Beta"],
  ["wip", "Work in progress"], ["as_is", "As-is"], ["looking_for_maintainer", "Looking for maintainer"], ["deprecated", "Deprecated"],
];
const STATUS_TEXT = { ok: "works", risky: "risky", unsatisfiable: "won't run", unknown: "unknown" };
const STATUS_HELP = {
  ok: "Dependencies can be met in this game.",
  risky: "Installable, but the code uses another mod's API without declaring it; it may break in this game.",
  unsatisfiable: "A required mod name can't be provided in this game.",
  unknown: "Not resolved.",
};
const gameShort = (key) => key.split("/")[1] || key;

// --- routing ---------------------------------------------------------------
const views = { search: viewSearch, installed: viewInstalled, updates: viewUpdates, installs: viewInstalls };
function route() {
  const m = location.hash.match(/^#\/([a-z]+)(?:\/(.*))?/);
  const tab = m && views[m[1]] ? m[1] : "search";
  document.querySelectorAll("#tabs a").forEach((a) => a.classList.toggle("active", a.dataset.tab === tab));
  views[tab]();
  if (m && m[1] === "pkg" && m[2]) showPackage(decodeURIComponent(m[2]));
}
window.addEventListener("hashchange", route);

// --- banner ----------------------------------------------------------------
async function loadMeta() {
  try {
    const meta = await api("/api/meta");
    const msgs = [];
    if (meta.offline) msgs.push("Offline mode: showing cached data only.");
    if (meta.snapshot_error) msgs.push("Dependency snapshot unavailable: " + meta.snapshot_error);
    else if (meta.snapshot_stale) msgs.push(`The dependency snapshot is ${Math.round(meta.snapshot_age_days)} days old. Refresh it from the Installations tab.`);
    if (meta.installations === 0) msgs.push("No Luanti installation registered yet. Add one under Installations.");
    const b = $("#banner");
    b.hidden = msgs.length === 0;
    b.replaceChildren(...msgs.map((m) => h("div", {}, m)));
  } catch (e) {
    const b = $("#banner");
    b.hidden = false;
    b.textContent = token ? "Cannot reach luanthetti: " + e.message : "Open the URL printed by `luanthetti ui` (it carries an access token).";
  }
}

// --- search ----------------------------------------------------------------
let searchAbort = null;
let searchTimer = null;

function viewSearch() {
  const f = state.filters;
  const results = h("div", { id: "results" });
  const input = h("input", { type: "search", placeholder: "Search mods, games and texture packs", value: f.q, autofocus: true,
    oninput: (e) => { f.q = e.target.value; state.limit = 48; scheduleSearch(); } });
  const sort = h("select", { onchange: (e) => { f.sort = e.target.value; runSearch(); } },
    [["score", "Best score"], ["downloads", "Most downloads"], ["updated", "Recently updated"], ["name", "Name"]]
      .map(([v, t]) => h("option", { value: v, selected: f.sort === v }, t)));
  $("#view").replaceChildren(h("div", { class: "search-layout" }, filtersPanel(), h("div", {}, h("div", { class: "searchbar" }, input, sort), results)));
  runSearch();
}

function filtersPanel() {
  const f = state.filters;
  const radio = (name, value, label) => h("label", {}, h("input", { type: "radio", name, value, checked: f.type === value,
    onchange: () => { f.type = value; runSearch(); } }), label);
  const check = (list, value, label) => h("label", {}, h("input", { type: "checkbox", checked: list.includes(value),
    onchange: (e) => { e.target.checked ? list.push(value) : list.splice(list.indexOf(value), 1); runSearch(); } }), label);
  const select = (key, options) => h("select", { onchange: (e) => { f[key] = e.target.value; runSearch(); } },
    options.map(([v, t]) => h("option", { value: v, selected: String(f[key]) === v }, t)));
  const games = new Map();
  for (const inst of state.installs) for (const g of inst.games) games.set(g.key, g.title || g.id);
  const topTags = [...state.tags].sort((a, b) => (b.views || 0) - (a.views || 0)).slice(0, 40);
  return h("aside", { class: "filters" },
    h("h3", {}, "Type"), radio("type", "mod", "Mods"), radio("type", "game", "Games"), radio("type", "txp", "Texture packs"), radio("type", "", "All"),
    state.installs.length ? [h("h3", {}, "Works in"), state.installs.map((i) => check(f.compat, i.name, `${i.name} (${i.games.map((g) => g.id).join(", ") || "no games"})`))] : null,
    h("h3", {}, "Maturity"), DEV_STATES.map(([v, t]) => check(f.dev, v, t)),
    h("h3", {}, "Popularity"), select("minDownloads", [["", "Any"], ["100", "100+ downloads"], ["1000", "1,000+ downloads"], ["10000", "10,000+ downloads"]]),
    h("h3", {}, "Maintenance"), select("within", [["", "Any time"], ["90", "Released in the last 3 months"], ["180", "… 6 months"], ["365", "… year"], ["730", "… 2 years"]]),
    topTags.length ? [h("h3", {}, "Category"), topTags.map((t) => check(f.tags, t.name, t.title))] : null,
    games.size ? h("p", { class: "muted small" }, "Badges show compatibility with the games of your installations, from luanti-depgraph.") : null,
  );
}

function scheduleSearch() {
  clearTimeout(searchTimer);
  searchTimer = setTimeout(runSearch, 250);
}

async function runSearch() {
  const f = state.filters;
  const p = new URLSearchParams({ q: f.q, sort: f.sort, limit: state.limit });
  if (f.type) p.set("type", f.type);
  f.tags.forEach((t) => p.append("tag", t));
  f.dev.forEach((d) => p.append("dev_state", d));
  f.compat.forEach((c) => p.append("compatible_with", c));
  if (f.minDownloads) p.set("min_downloads", f.minDownloads);
  if (f.within) p.set("updated_within_days", f.within);
  if (searchAbort) searchAbort.abort();
  searchAbort = new AbortController();
  const box = $("#results");
  if (!box) return;
  try {
    const rep = await api("/api/search?" + p, { signal: searchAbort.signal });
    const cards = rep.hits.map(card);
    box.replaceChildren(
      h("p", { class: "muted small" }, `${rep.total} package${rep.total === 1 ? "" : "s"}`, rep.notes.length ? " · " + rep.notes.join(" · ") : ""),
      cards.length ? h("div", { class: "cards" }, cards) : h("div", { class: "empty" }, "Nothing matches these filters."),
      rep.total > rep.hits.length ? h("div", { class: "more" }, h("button", { onclick: () => { state.limit += 48; runSearch(); } }, "Show more")) : null,
    );
  } catch (e) {
    if (e.name !== "AbortError") box.replaceChildren(h("div", { class: "empty error" }, e.message));
  }
}

function compatBadges(compat) {
  return Object.entries(compat || {}).map(([game, s]) =>
    h("span", { class: "badge " + s, title: `${game}: ${STATUS_HELP[s] || s}` }, `${gameShort(game)} ${STATUS_TEXT[s] || s}`));
}

function card(hit) {
  const [author, name] = hit.package.split("/");
  const open = () => { location.hash = "#/search"; showPackage(hit.package); };
  const installed = hit.installed_in.length > 0;
  return h("article", { class: "card" },
    h("div", { class: "thumb", onclick: open }, hit.thumbnail ? h("img", { src: thumbURL(hit.thumbnail), alt: "", loading: "lazy" }) : hit.type),
    h("div", { class: "body" },
      h("div", { class: "title", onclick: open }, hit.title || name),
      h("div", { class: "author" }, author, " · ", name),
      h("div", { class: "desc" }, hit.short_description),
      h("div", { class: "meta" },
        h("span", { class: "chip" }, `${hit.downloads.toLocaleString()} ↓`),
        hit.dev_state ? h("span", { class: "chip" }, hit.dev_state.replaceAll("_", " ")) : null,
        hit.latest_release_at ? h("span", { class: "chip", title: "latest release" }, hit.latest_release_at.slice(0, 10)) : null,
        installed ? h("span", { class: "badge installed", title: hit.installed_in.join(", ") }, "installed") : null),
      h("div", { class: "meta" }, compatBadges(hit.compat)),
      h("div", { class: "actions" },
        h("button", { onclick: open }, "Details"),
        h("button", { class: "primary", onclick: () => planInstall([hit.package]) }, installed ? "Install elsewhere" : "Install"))));
}

// --- package detail ----------------------------------------------------------
async function showPackage(key) {
  modal(h("p", { class: "muted" }, "Loading ", key, "…"));
  try {
    const info = await api("/api/packages/" + key);
    const d = info.details || {};
    const compatRows = info.compat.map((c) => h("tr", {},
      h("td", {}, c.title || c.game), h("td", {}, h("span", { class: "badge " + c.status, title: STATUS_HELP[c.status] }, STATUS_TEXT[c.status] || c.status)),
      h("td", { class: "small" },
        c.missing.length ? "missing: " + c.missing.join(", ") : c.undeclared_missing.length ? "undeclared: " + c.undeclared_missing.join(", ") : c.install.length ? "installs " + c.install.join(", ") : "",
      ),
      h("td", { class: "small muted" }, c.contentdb_says ? (c.contentdb_says.supports ? "ContentDB: yes" : "ContentDB: no") : "")));
    const latest = info.releases[0];
    modal(
      h("div", { class: "detail-head" },
        info.thumbnail ? h("img", { src: thumbURL(info.thumbnail), alt: "" }) : h("div"),
        h("div", {},
          h("h2", {}, info.title), h("div", { class: "muted" }, info.package, " · ", info.type),
          h("p", {}, info.short_description),
          h("div", { class: "meta" },
            h("span", { class: "chip" }, `${info.downloads.toLocaleString()} downloads`),
            d.dev_state ? h("span", { class: "chip" }, d.dev_state.replaceAll("_", " ")) : null,
            d.license ? h("span", { class: "chip" }, d.license) : null,
            (d.tags || []).map((t) => h("span", { class: "chip" }, t))),
          latest ? h("p", { class: "small muted" }, `Latest release ${latest.title} (${(latest.date || "").slice(0, 10)})`,
            latest.min_engine ? `, Luanti ${latest.min_engine}${latest.max_engine ? " to " + latest.max_engine : " or newer"}` : "") : null,
          h("p", { class: "small" }, h("a", { href: info.url, target: "_blank", rel: "noopener noreferrer" }, "Open on ContentDB")))),
      info.depends.length ? h("p", { class: "small" }, h("strong", {}, "Needs: "), info.depends.join(", ")) : null,
      info.optional_depends.length ? h("p", { class: "small muted" }, "Optional: ", info.optional_depends.join(", ")) : null,
      compatRows.length ? [h("h3", {}, "Compatibility"), h("table", {}, h("tbody", {}, compatRows))] : null,
      info.installed.length ? [h("h3", {}, "Installed"), h("ul", {}, info.installed.map((r) => h("li", {}, `${r.installation}: release ${r.release} (${r.state})`)))] : null,
      info.notes.map((n) => h("p", { class: "small muted" }, n)),
      h("div", { class: "row" },
        info.installed.length ? h("button", { class: "danger", onclick: () => confirmRemove(info.package, info.installed.map((r) => r.installation)) }, "Remove…") : null,
        h("button", { onclick: closeModal }, "Close"),
        h("button", { class: "primary", onclick: () => planInstall([info.package]) }, "Install…")));
  } catch (e) {
    modal(h("p", { class: "error" }, e.message), h("div", { class: "row" }, h("button", { onclick: closeModal }, "Close")));
  }
}

// --- install flow -----------------------------------------------------------
async function planInstall(specs, into) {
  if (!state.installs.length) {
    modal(h("p", {}, "Register a Luanti installation first."), h("div", { class: "row" },
      h("button", { onclick: closeModal }, "Close"), h("button", { class: "primary", onclick: () => { closeModal(); location.hash = "#/installs"; } }, "Installations")));
    return;
  }
  const chosen = new Set(into || state.installs.map((i) => i.name));
  modal(h("p", { class: "muted" }, "Working out what ", specs.join(", "), " needs…"));
  let plan;
  try {
    plan = await api("/api/plan", { method: "POST", body: { kind: "install", specs, into: [...chosen] } });
  } catch (e) {
    modal(h("p", { class: "error" }, e.message), h("div", { class: "row" }, h("button", { onclick: closeModal }, "Close")));
    return;
  }
  const blocks = plan.results.map((r) => h("div", { class: "plan-inst" },
    h("h3", {}, h("label", {}, h("input", { type: "checkbox", checked: chosen.has(r.installation) && !r.skipped, disabled: !!r.skipped,
      onchange: (e) => e.target.checked ? chosen.add(r.installation) : chosen.delete(r.installation) }), " ", r.installation)),
    r.games.length ? h("div", { class: "meta" }, r.games.map((g) => h("span", { class: "badge " + g.status, title: STATUS_HELP[g.status] },
      `${gameShort(g.game)} ${STATUS_TEXT[g.status] || g.status}`))) : null,
    r.skipped ? h("p", { class: "warn small" }, r.skipped) : h("ul", {}, r.items.map((it) => h("li", { class: it.status === "skipped" ? "warn" : "" },
      it.package, it.release ? ` (release ${it.release})` : "", it.reason === "dependency" ? h("span", { class: "muted" }, ` for ${it.required_by.join(", ")}`) : "",
      it.status === "present" ? h("span", { class: "muted" }, " already installed") : "", it.note ? h("span", { class: "small" }, ": " + it.note) : ""))),
    r.warnings.map((w) => h("p", { class: "small muted" }, w))));
  modal(h("h2", {}, "Install ", specs.join(", ")), blocks,
    h("div", { class: "row" }, h("button", { onclick: closeModal }, "Cancel"),
      h("button", { class: "primary", onclick: async () => {
        const target = [...chosen].filter((n) => plan.results.some((r) => r.installation === n && !r.skipped));
        if (!target.length) return;
        closeModal();
        await submitJob({ kind: "install", specs, into: target });
      } }, "Install")));
}

function confirmRemove(pkg, installs) {
  const chosen = new Set(installs);
  modal(h("h2", {}, "Remove ", pkg),
    h("p", {}, "Unlink it from these installations. Dependencies nothing else needs are removed too; the library keeps nothing that no installation uses."),
    installs.map((n) => h("label", {}, h("input", { type: "checkbox", checked: true, onchange: (e) => e.target.checked ? chosen.add(n) : chosen.delete(n) }), " ", n)),
    h("div", { class: "row" }, h("button", { onclick: closeModal }, "Cancel"),
      h("button", { class: "danger", onclick: async () => { closeModal(); await submitJob({ kind: "remove", specs: [pkg], into: [...chosen] }); } }, "Remove")));
}

// --- installed / updates ----------------------------------------------------
async function viewInstalled() {
  $("#view").replaceChildren(h("p", { class: "muted" }, "Loading…"));
  try {
    const rep = await api("/api/installed");
    const rows = rep.rows.map((r) => h("tr", {},
      h("td", {}, r.installation), h("td", {}, h("a", { href: "#/pkg/" + r.package }, r.package)), h("td", {}, r.folder), h("td", {}, r.release),
      h("td", {}, r.link_kind), h("td", {}, r.reason, r.pinned ? " · pinned" : ""),
      h("td", { class: r.state === "ok" ? "" : "warn" }, r.state),
      h("td", {}, r.reason === "explicit" || r.reason === "adopted" ? h("button", { class: "danger", onclick: () => confirmRemove(r.package, [r.installation]) }, "Remove") : null)));
    $("#view").replaceChildren(
      h("div", { class: "toolbar" }, h("h2", {}, "Installed"), h("button", { onclick: () => submitJob({ kind: "repair" }) }, "Repair links")),
      rows.length ? h("table", {}, h("thead", {}, h("tr", {}, ["Installation", "Package", "Folder", "Release", "Link", "Reason", "State", ""].map((t) => h("th", {}, t)))), h("tbody", {}, rows))
        : h("div", { class: "empty" }, "Nothing installed through Luanthetti yet. Search for something, or adopt what is already installed under Installations."));
  } catch (e) {
    $("#view").replaceChildren(h("div", { class: "empty error" }, e.message));
  }
}

async function viewUpdates() {
  $("#view").replaceChildren(h("p", { class: "muted" }, "Checking for updates…"));
  try {
    const rep = await api("/api/updates");
    const outdated = rep.items.filter((i) => i.status === "outdated");
    const rows = rep.items.map((i) => h("tr", {},
      h("td", {}, i.installation), h("td", {}, h("a", { href: "#/pkg/" + i.package }, i.package)), h("td", {}, `${i.current} → ${i.latest}`),
      h("td", {}, i.status, i.note ? h("span", { class: "muted small" }, " " + i.note) : ""),
      h("td", { class: "small" }, (i.new_dependencies || []).join(", ")),
      h("td", {}, i.status === "outdated" ? h("button", { onclick: () => submitJob({ kind: "update", specs: [i.package], into: [i.installation] }) }, "Update") : null)));
    $("#view").replaceChildren(
      h("div", { class: "toolbar" }, h("h2", {}, "Updates"),
        h("button", { class: "primary", disabled: !outdated.length, onclick: () => submitJob({ kind: "update" }) }, `Update all (${outdated.length})`)),
      rep.warnings.map((w) => h("p", { class: "warn small" }, w)),
      rows.length ? h("table", {}, h("thead", {}, h("tr", {}, ["Installation", "Package", "Release", "Status", "New dependencies", ""].map((t) => h("th", {}, t)))), h("tbody", {}, rows))
        : h("div", { class: "empty" }, "Everything is up to date."));
  } catch (e) {
    $("#view").replaceChildren(h("div", { class: "empty error" }, e.message));
  }
}

// --- installations ------------------------------------------------------------
async function viewInstalls() {
  await loadInstalls();
  const rows = state.installs.map((i) => h("tr", {},
    h("td", {}, h("strong", {}, i.name)), h("td", { class: "small" }, i.root, i.exists ? "" : h("span", { class: "error" }, " (missing)")),
    h("td", {}, i.engine_version || "?"), h("td", {}, i.link_mode), h("td", {}, i.links),
    h("td", { class: "small" }, i.games.map((g) => g.title || g.id).concat(i.unknown_games.map((g) => g.id + "?")).join(", ")),
    h("td", {}, h("button", { onclick: () => scanAndAdopt(i.name) }, "Adopt…"), " ",
      h("button", { class: "danger", onclick: () => removeInstall(i.name) }, "Forget"))));
  const path = h("input", { type: "text", placeholder: "Path to a Luanti user folder or server root", required: true });
  const name = h("input", { type: "text", placeholder: "Name (optional)" });
  const detected = h("div", {});
  $("#view").replaceChildren(
    h("div", { class: "toolbar" }, h("h2", {}, "Installations"),
      h("button", { onclick: () => submitJob({ kind: "refresh" }) }, "Refresh dependency data"),
      h("button", { onclick: () => detect(detected) }, "Detect")),
    rows.length ? h("table", {}, h("thead", {}, h("tr", {}, ["Name", "Path", "Luanti", "Links", "Packages", "Games", ""].map((t) => h("th", {}, t)))), h("tbody", {}, rows))
      : h("div", { class: "empty" }, "No installations yet. Detect them or add a path below."),
    detected,
    h("h3", {}, "Add an installation"),
    h("form", { class: "inline", onsubmit: async (e) => {
      e.preventDefault();
      try { await api("/api/installations", { method: "POST", body: { path: path.value, name: name.value } }); viewInstalls(); loadMeta(); }
      catch (err) { alert(err.message); }
    } }, path, name, h("button", { class: "primary", type: "submit" }, "Add")));
}

async function detect(box) {
  box.replaceChildren(h("p", { class: "muted" }, "Looking for Luanti installations…"));
  const rep = await api("/api/detect");
  box.replaceChildren(h("h3", {}, "Found on this machine"), rep.candidates.length ? h("ul", {}, rep.candidates.map((c) => h("li", {},
    `${c.root} (${c.kind}, Luanti ${c.engine_version || "?"}, ${c.mods} mods, games: ${c.games.map((g) => g.id).join(", ") || "none"}) `,
    c.registered ? h("span", { class: "muted" }, "registered as " + c.registered)
      : h("button", { onclick: async () => { await api("/api/installations", { method: "POST", body: { path: c.root } }); viewInstalls(); loadMeta(); } }, "Add")))) : h("p", { class: "muted" }, "None found."));
}

async function scanAndAdopt(inst) {
  modal(h("h2", {}, "Adopt packages in ", inst),
    h("p", {}, "Folders installed by Luanti's own content browser or by luma carry their ContentDB identity. Adopting moves them into the library and puts a link in their place; Luanti sees no difference. Git checkouts and folders without that identity are left alone."),
    h("div", { class: "row" }, h("button", { onclick: closeModal }, "Cancel"),
      h("button", { class: "primary", onclick: async () => { closeModal(); await submitJob({ kind: "adopt", into: [inst] }); } }, "Adopt all adoptable")));
}

async function removeInstall(name) {
  if (!confirm(`Forget the installation "${name}"? Its folders stay as they are.`)) return;
  try { await api("/api/installations/" + encodeURIComponent(name), { method: "DELETE" }); viewInstalls(); loadMeta(); }
  catch (e) { alert(e.message); }
}

async function loadInstalls() {
  try { state.installs = await api("/api/installations"); } catch { state.installs = []; }
}

// --- jobs -------------------------------------------------------------------
async function submitJob(body) {
  try {
    const { id } = await api("/api/jobs", { method: "POST", body: { specs: [], into: [], ...body } });
    state.jobs.set(id, { id, kind: body.kind, title: body.kind + " " + (body.specs || []).join(", "), state: "queued", log: [] });
    showJobs(true);
    renderJobs();
  } catch (e) {
    alert(e.message);
  }
}

function renderJobs() {
  const list = [...state.jobs.values()].sort((a, b) => (b.created_at || "").localeCompare(a.created_at || ""));
  const active = list.filter((j) => j.state === "queued" || j.state === "running").length;
  $("#jobs-count").textContent = active ? String(active) : "";
  $("#jobs").replaceChildren(
    h("div", { class: "toolbar" }, h("strong", {}, "Jobs"), h("span", { class: "muted small" }, active ? `${active} running` : ""),
      h("button", { onclick: () => showJobs(false) }, "Hide")),
    list.length ? list.slice(0, 20).map((j) => h("div", { class: "job" },
      h("div", { class: "head" }, h("span", {}, j.title || j.kind), h("span", { class: "state-" + j.state }, j.state,
        j.state === "running" || j.state === "queued" ? h("button", { onclick: () => api(`/api/jobs/${j.id}/cancel`, { method: "POST", body: {} }) }, "Cancel") : null)),
      j.error ? h("div", { class: "error small" }, j.error) : null,
      (j.log || []).length ? h("pre", {}, j.log.map((l) => l.msg).join("\n")) : null))
      : h("p", { class: "muted small" }, "No jobs yet."));
}

function showJobs(show) {
  $("#jobs").hidden = !show;
  $("#jobs-toggle").setAttribute("aria-expanded", String(show));
}

function connectEvents() {
  const es = new EventSource("/api/events?t=" + encodeURIComponent(token));
  es.addEventListener("job", (e) => {
    const j = JSON.parse(e.data);
    const prev = state.jobs.get(j.id);
    state.jobs.set(j.id, j);
    renderJobs();
    if (prev && prev.state !== j.state && (j.state === "done" || j.state === "failed")) {
      loadInstalls().then(route);
      loadMeta();
    }
  });
  es.addEventListener("log", (e) => {
    const { job, line } = JSON.parse(e.data);
    const j = state.jobs.get(job);
    if (j) { j.log = [...(j.log || []), line]; renderJobs(); }
  });
  es.addEventListener("snapshot", () => loadMeta());
}

// --- modal ------------------------------------------------------------------
function modal(...content) {
  const m = $("#modal");
  m.querySelector(".modal-box").replaceChildren(...content.flat(Infinity).filter(Boolean));
  m.hidden = false;
}
function closeModal() {
  $("#modal").hidden = true;
  if (location.hash.startsWith("#/pkg/")) history.replaceState(null, "", "#/search");
}
$("#modal").addEventListener("click", (e) => { if (e.target.id === "modal") closeModal(); });
document.addEventListener("keydown", (e) => { if (e.key === "Escape") closeModal(); });
$("#jobs-toggle").addEventListener("click", () => showJobs($("#jobs").hidden));

// --- start ------------------------------------------------------------------
(async function start() {
  await loadMeta();
  if (!token) return;
  await loadInstalls();
  try { state.tags = await api("/api/tags"); } catch { state.tags = []; }
  try { for (const j of await api("/api/jobs")) state.jobs.set(j.id, j); } catch {}
  renderJobs();
  connectEvents();
  route();
})();
