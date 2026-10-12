// The file manager: the files of this device and of every paired one, laid
// out the way desktop file managers are: places on the left, tabs, a path
// bar and a toolbar on top, the folder in the middle, a status bar below.
// Two folders can be shown side by side.
//
// Every tab keeps its own folder, history and selection. The parts of a
// panel are redrawn on their own, so that selecting a file does not disturb
// the scroll position or a typed filter.

const SELF = "self";
const coarse = window.matchMedia("(pointer: coarse)");

const fm = {
  tabs: [],
  // panes holds the tabs on screen, left and right; side is the one in use.
  panes: [0, null],
  side: 0,
  tab: null,
  ownShares: [],
  sort: { key: "name", dir: 1 },
  mode: "list",
  hidden: false,
  clip: null,
  active: 0,
  bookmarks: [],
  recents: [],
  loaded: false,
};

// What belongs to a tab is reached through fm as well, for the tab in use.
const TAB_KEYS = ["dev", "res", "places", "back", "forward", "selected", "anchor", "selecting", "filter", "renaming", "root", "pathEdit"];
for (const key of TAB_KEYS) {
  Object.defineProperty(fm, key, {
    get: () => (fm.tab ? fm.tab[key] : undefined),
    set: (value) => {
      if (fm.tab) fm.tab[key] = value;
    },
  });
}

const newTab = () => ({ dev: null, res: null, places: [], back: [], forward: [], selected: new Set(), anchor: null, selecting: false, filter: "", renaming: null, root: null, pathEdit: false });

// inTab runs fn as if tab were the one in use.
function inTab(tab, fn) {
  const current = fm.tab;
  fm.tab = tab;
  try {
    return fn();
  } finally {
    fm.tab = current;
  }
}

const shown = () => fm.panes.filter((i) => i !== null).map((i) => fm.tabs[i]);
const otherTab = () => {
  const i = fm.panes[1 - fm.side];
  return i === null || i === undefined ? null : fm.tabs[i];
};
const q = (selector) => (fm.root ? fm.root.querySelector(selector) : null);

const sepOf = (path) => (path.includes("\\") && !path.includes("/") ? "\\" : "/");
const joinPath = (dir, name) => dir.replace(/[\\/]+$/, "") + sepOf(dir) + name;
const extOf = (name) => (name.includes(".") ? name.slice(name.lastIndexOf(".") + 1).toLowerCase() : "");
const baseOf = (path) => path.replace(/[\\/]+$/, "").split(/[\\/]/).pop() || path;
function parentOf(path) {
  const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
  return path.slice(0, cut) || path.slice(0, cut + 1);
}

const kinds = {
  image: ["jpg", "jpeg", "png", "gif", "webp", "bmp", "svg", "avif"],
  video: ["mp4", "mkv", "webm", "mov", "avi", "m4v"],
  audio: ["mp3", "ogg", "flac", "wav", "m4a", "opus", "aac"],
  pdf: ["pdf"],
  office: ["docx", "pptx", "xlsx", "odt", "odp", "ods", "doc", "ppt", "xls", "rtf"],
  archive: ["zip", "tar", "tgz", "gz", "jar", "apk"],
  text: ["txt", "md", "log", "json", "yml", "yaml", "toml", "ini", "conf", "sh", "py", "go", "js", "ts", "jsx", "tsx", "css", "html", "xml", "c", "h", "cpp", "hpp", "rs", "java", "kt", "cs", "php", "rb", "lua", "sql", "csv", "desktop", "mk", "mod", "gradle", "bat", "ps1"],
};
// What a device may be able to picture; where it cannot, the icon stays.
const thumbable = ["jpg", "jpeg", "png", "gif", "pdf", "mp4", "mkv", "webm", "mov", "avi", "m4v"];
const typeIcons = { folder: "folder", image: "image", video: "video", audio: "audio", pdf: "pdf", office: "office", archive: "archive", text: "code", doc: "doc" };

function kindOf(e) {
  if (e.dir) return "folder";
  const ext = extOf(e.name);
  for (const [kind, list] of Object.entries(kinds)) if (list.includes(ext)) return kind;
  return "doc";
}

const placeIcons = { home: "home", desktop: "desktop", documents: "doc", downloads: "download", pictures: "image", camera: "image", music: "audio", videos: "video", root: "disk" };

function fileURL(kind, dev, path, extra) {
  const token = api.web ? "&token=" + encodeURIComponent(location.hash.slice(1)) : "";
  return "/api/fs/" + kind + "?device=" + encodeURIComponent(dev) + "&path=" + encodeURIComponent(path) + (extra || "") + token;
}

const fmDevice = (id) => (id === SELF ? { id: SELF, name: state.self.deviceName, type: state.self.deviceType, self: true } : state.devices.find((d) => d.id === id));
const fmCall = (op, path, more) => api.FS(fm.dev, { op, path, ...(more || {}) });
const iconButton = (name, title, action, extra) => h("button", { class: "icon-button" + (extra || ""), title, icon: name, onclick: action });

// What is remembered between runs.

async function loadUI() {
  if (fm.loaded) return;
  fm.loaded = true;
  try {
    const saved = JSON.parse((await api.UIState()) || "{}");
    fm.bookmarks = saved.bookmarks || [];
    fm.recents = saved.recents || [];
    if (saved.mode) fm.mode = saved.mode;
    if (saved.sort) fm.sort = saved.sort;
    fm.hidden = !!saved.hidden;
  } catch (err) {
    // Nothing remembered yet.
  }
}

function saveUI() {
  api.SaveUIState(JSON.stringify({ bookmarks: fm.bookmarks, recents: fm.recents, mode: fm.mode, sort: fm.sort, hidden: fm.hidden })).catch(() => {});
}

// Dialogs and menus

function closeLayer() {
  document.querySelectorAll(".fm-menu, .fm-dialog-back").forEach((n) => n.remove());
}

// menu shows a list of actions at a point of the screen.
function menu(x, y, items) {
  closeLayer();
  const node = h(
    "div",
    { class: "fm-menu" },
    ...items
      .filter(Boolean)
      .map((item) =>
        item === "-"
          ? h("hr", {})
          : h(
              "button",
              {
                class: "fm-menu-item" + (item.danger ? " danger" : ""),
                disabled: !!item.disabled,
                onclick: () => {
                  closeLayer();
                  item.action();
                },
              },
              h("span", { class: "fm-menu-icon", icon: item.icon || "blank" }),
              item.label,
            ),
      ),
  );
  document.body.append(node);
  const box = node.getBoundingClientRect();
  node.style.left = Math.max(8, Math.min(x, window.innerWidth - box.width - 8)) + "px";
  node.style.top = Math.max(8, Math.min(y, window.innerHeight - box.height - 8)) + "px";
}
document.addEventListener("pointerdown", (e) => !e.target.closest(".fm-menu") && document.querySelectorAll(".fm-menu").forEach((n) => n.remove()), true);

// ask shows a small form and resolves to its values, or to null when
// dismissed. content, if given, is shown above the fields.
function ask({ title, text, fields, ok, danger, content }) {
  return new Promise((resolve) => {
    closeLayer();
    const done = (value) => {
      back.remove();
      resolve(value);
    };
    const inputs = (fields || []).map((f) =>
      f.options
        ? h("select", { name: f.key }, ...f.options.map((o) => h("option", { value: o.value }, o.label)))
        : h("input", { name: f.key, value: f.value || "", placeholder: f.label || "", spellcheck: "false", autocomplete: "off" }),
    );
    const form = h(
      "form",
      {
        class: "fm-dialog",
        onsubmit: (e) => {
          e.preventDefault();
          done(Object.fromEntries(inputs.map((i) => [i.name, i.value.trim()])));
        },
      },
      h("h2", {}, title),
      text ? h("p", { class: "muted" }, text) : null,
      content || null,
      ...inputs,
      h("div", { class: "row between" }, ok ? h("button", { type: "button", class: "button", onclick: () => done(null) }, t("cancel")) : h("span", {}), h("button", { class: "button primary" + (danger ? " danger" : "") }, ok || t("close"))),
    );
    const back = h("div", { class: "fm-dialog-back", onpointerdown: (e) => e.target === back && done(null) }, form);
    back.addEventListener("keydown", (e) => {
      if (e.key === "Escape") done(null);
      e.stopPropagation();
    });
    document.body.append(back);
    const first = inputs.find((i) => i.tagName === "INPUT") || form.querySelector("button.primary");
    first.focus();
    if (first.select) first.select();
  });
}

// Navigation

async function fmGo(dev, path, record, tab) {
  tab = tab || fm.tab;
  const from = tab.res && tab.dev ? { dev: tab.dev, path: tab.res.path } : null;
  let res;
  try {
    res = await api.FS(dev, { op: "list", path });
  } catch (err) {
    // A device that keeps its files to itself still shows its network folders.
    if (path !== "" || dev === SELF) {
      toast(String(err), true);
      return false;
    }
    res = { path: "", parent: "", entries: [], denied: true };
  }
  if (!fm.tabs.includes(tab)) return false;
  if (dev !== tab.dev) {
    tab.places = [];
    api
      .FS(dev, { op: "places" })
      .then((p) => {
        if (tab.dev !== dev) return;
        tab.places = p.entries || [];
        if (res.denied && p.shares && p.shares.length) fmGo(dev, p.shares[0].path, false, tab);
        inTab(tab, drawBar);
        if (tab === fm.tab) drawPlaces();
      })
      .catch(() => {});
  }
  if (record !== false && from && (from.dev !== dev || from.path !== res.path)) {
    tab.back.push(from);
    tab.forward = [];
  }
  Object.assign(tab, { dev, res, selecting: false, renaming: null, filter: "", pathEdit: false });
  tab.selected.clear();
  if (tab === fm.tab) {
    view.id = dev;
    view.path = res.path;
    drawPlaces();
  }
  inTab(tab, drawPane);
  drawTabs();
  return true;
}

const fmReload = (tab) => {
  tab = tab || fm.tab;
  return tab && tab.res ? fmGo(tab.dev, tab.res.path, false, tab) : false;
};

async function fmStep(from, to) {
  const tab = fm.tab;
  const target = from.pop();
  if (!target) return;
  const here = { dev: tab.dev, path: tab.res.path };
  if (await fmGo(target.dev, target.path, false, tab)) to.push(here);
  inTab(tab, drawBar);
}

// reveal opens the folder of a file and shows the file.
async function reveal(dev, path) {
  try {
    const stat = await api.FS(dev, { op: "stat", path });
    if (stat.entry && stat.entry.dir) return fmGo(dev, path);
    if ((await fmGo(dev, parentOf(path))) && stat.entry) preview(stat.entry);
  } catch (err) {
    toast(String(err), true);
  }
}

async function search(query) {
  const tab = fm.tab;
  if (!query || !tab.res.path) return;
  const base = tab.res.search ? tab.res.base : tab.res;
  try {
    const found = await api.FS(tab.dev, { op: "search", path: base.path, to: query });
    if (!fm.tabs.includes(tab)) return;
    tab.res = { ...found, path: base.path, parent: base.parent, search: query, base };
    tab.selected.clear();
    tab.filter = "";
    inTab(tab, drawPane);
  } catch (err) {
    toast(String(err), true);
  }
}

function loadOwnShares() {
  api
    .FS(SELF, { op: "places" })
    .then((p) => {
      fm.ownShares = p.shares || [];
      drawPlaces();
    })
    .catch(() => {});
}

// Tabs and panels

function addTab(dev, path, background) {
  const tab = newTab();
  fm.tabs.push(tab);
  if (!background) {
    fm.panes[fm.side] = fm.tabs.length - 1;
    fm.tab = tab;
  }
  layout();
  fmGo(dev, path, false, tab);
  return tab;
}

function showTab(i) {
  const other = 1 - fm.side;
  if (fm.panes[other] === i) fm.panes[other] = fm.panes[fm.side];
  fm.panes[fm.side] = i;
  fm.tab = fm.tabs[i];
  layout();
}

function closeTab(i) {
  if (fm.tabs.length === 1) return;
  fm.tabs.splice(i, 1);
  const free = () => fm.tabs.findIndex((_, n) => !fm.panes.includes(n));
  fm.panes = fm.panes.map((p) => (p === null ? null : p === i ? -1 : p > i ? p - 1 : p));
  for (const side of [0, 1]) {
    if (fm.panes[side] !== -1) continue;
    const next = free();
    fm.panes[side] = next >= 0 ? next : side === 0 && fm.panes[1] !== null ? fm.panes[1] : null;
    if (side === 0 && fm.panes[0] === fm.panes[1]) fm.panes[1] = null;
  }
  if (fm.panes[0] === null) fm.panes = [0, null];
  if (fm.panes[fm.side] === null) fm.side = 0;
  fm.tab = fm.tabs[fm.panes[fm.side]];
  layout();
}

function toggleSplit() {
  if (fm.panes[1] !== null) {
    fm.panes[1] = null;
    fm.side = 0;
    fm.tab = fm.tabs[fm.panes[0]];
    return layout();
  }
  const spare = fm.tabs.findIndex((_, n) => n !== fm.panes[0]);
  if (spare >= 0) {
    fm.panes[1] = spare;
    return layout();
  }
  const from = fm.tab;
  const tab = newTab();
  fm.tabs.push(tab);
  fm.panes[1] = fm.tabs.length - 1;
  layout();
  fmGo(from.dev, from.res.path, false, tab);
}

function activate(side) {
  if (fm.side === side || fm.panes[side] === null) return;
  fm.side = side;
  fm.tab = fm.tabs[fm.panes[side]];
  view.id = fm.dev;
  view.path = fm.res ? fm.res.path : "";
  for (const tab of shown()) if (tab.root) tab.root.classList.toggle("active", tab === fm.tab);
  drawPlaces();
  drawTabs();
}

// Entries and selection

function fmEntries() {
  const needle = (fm.filter || "").toLowerCase();
  const list = fm.res.entries.filter((e) => fm.res.search || ((fm.hidden || !e.name.startsWith(".")) && (!needle || e.name.toLowerCase().includes(needle))));
  const { key, dir } = fm.sort;
  const value = (e) => (key === "size" ? e.size : key === "modified" ? e.modified : e.name.toLowerCase());
  return list.sort((a, b) => {
    if (a.dir !== b.dir) return a.dir ? -1 : 1;
    const x = value(a);
    const y = value(b);
    return (x < y ? -1 : x > y ? 1 : a.name.localeCompare(b.name)) * dir;
  });
}

const picked = () => fm.res.entries.filter((e) => fm.selected.has(e.path));

function syncSelection() {
  if (!fm.root) return;
  for (const node of fm.root.querySelectorAll("[data-path]")) node.classList.toggle("sel", fm.selected.has(node.dataset.path));
  if (!fm.selected.size) fm.selecting = false;
  fm.root.classList.toggle("selecting", fm.selecting);
  drawStatus();
}

function select(e, ev) {
  const list = fmEntries();
  if (ev && ev.shiftKey && fm.anchor) {
    const a = list.findIndex((x) => x.path === fm.anchor);
    const b = list.findIndex((x) => x.path === e.path);
    if (a >= 0 && b >= 0) {
      if (!ev.ctrlKey) fm.selected.clear();
      for (let i = Math.min(a, b); i <= Math.max(a, b); i++) fm.selected.add(list[i].path);
      return syncSelection();
    }
  }
  if ((ev && (ev.ctrlKey || ev.metaKey)) || fm.selecting) {
    if (fm.selected.has(e.path)) fm.selected.delete(e.path);
    else fm.selected.add(e.path);
  } else {
    fm.selected.clear();
    fm.selected.add(e.path);
  }
  fm.anchor = e.path;
  syncSelection();
}

function selectAll() {
  for (const e of fmEntries()) fm.selected.add(e.path);
  fm.selecting = coarse.matches;
  syncSelection();
}

// Actions

async function act(action, done) {
  const tabs = shown();
  try {
    await action();
    if (done) toast(done);
  } catch (err) {
    toast(String(err), true);
  }
  for (const tab of tabs) if (fm.tabs.includes(tab) && !tab.res.search) fmReload(tab);
}

function remember(dev, e) {
  fm.recents = [{ dev, name: e.name, path: e.path }, ...fm.recents.filter((r) => r.dev !== dev || r.path !== e.path)].slice(0, 8);
  saveUI();
  drawPlaces();
}

function openEntry(e) {
  if (e.dir) return fmGo(fm.dev, e.path);
  remember(fm.dev, e);
  const kind = kindOf(e);
  // The window plays no video itself; the system's player does it better.
  if (kind !== "doc" && (api.web || (kind !== "video" && kind !== "audio"))) return preview(e);
  if (!api.web) return act(() => api.FSOpen(fm.dev, e.path), fm.dev === SELF ? "" : t("opening", e.name));
  toast(t("noPreview"));
}

function download(entries) {
  const dev = fm.dev;
  act(() => api.FSPaste(dev, entries.map((e) => e.path), SELF, "", false), t("downloading"));
}

function toClip(entries, cut) {
  if (!entries.length) return;
  fm.clip = { dev: fm.dev, paths: entries.map((e) => e.path), cut };
  toast(t(cut ? "cutDone" : "copyDone", entries.length));
  for (const tab of shown()) inTab(tab, drawStatus);
}

function paste(dir) {
  const clip = fm.clip;
  const dev = fm.dev;
  dir = dir || fm.res.path;
  if (!clip || !dir) return;
  if (clip.cut) fm.clip = null;
  act(() => api.FSPaste(clip.dev, clip.paths, dev, dir, clip.cut), t("pasting"));
}

// toOther sends the selection to the folder of the other panel.
function toOther(entries, move) {
  const other = otherTab();
  const dev = fm.dev;
  if (!other || !other.res || !other.res.path || !entries.length) return;
  act(() => api.FSPaste(dev, entries.map((e) => e.path), other.dev, other.res.path, move), t("pasting"));
}

async function removeEntries(entries) {
  if (!entries.length) return;
  const tab = fm.tab;
  const d = fmDevice(tab.dev) || {};
  // A phone has no trash can: what is deleted there is gone.
  if (d.type === "phone") {
    const sure = await ask({ title: t("deleteTitle"), text: t("deleteText", entries.length === 1 ? entries[0].name : t("items", entries.length)), ok: t("deleteForever"), danger: true });
    if (!sure) return;
  }
  act(async () => {
    for (const e of entries) await api.FS(tab.dev, { op: "delete", path: e.path });
  }, t(d.type === "phone" ? "deleted" : "trashed"));
}

async function newFolder() {
  const tab = fm.tab;
  const out = await ask({ title: t("newFolder"), fields: [{ key: "name", label: t("folderName"), value: t("newFolder") }], ok: t("create") });
  if (out && out.name) act(() => api.FS(tab.dev, { op: "mkdir", path: joinPath(tab.res.path, out.name) }));
}

function upload() {
  const dev = fm.dev;
  const dir = fm.res.path;
  if (!api.web) return act(() => api.FSUpload(dev, dir, t("chooseFiles")), "");
  const input = h("input", { type: "file", multiple: true });
  input.addEventListener("change", () => uploadFiles(dev, dir, input.files));
  input.click();
}

function uploadFiles(dev, dir, list) {
  if (!list.length) return;
  const form = new FormData();
  for (const file of list) form.append("file", file, file.name);
  act(() => api.FSUploadForm(dev, dir, form), t("uploaded"));
}

function rename(e) {
  fm.renaming = e.path;
  drawContent();
}

function bookmark(dev, path) {
  if (fm.bookmarks.some((b) => b.dev === dev && b.path === path)) return;
  fm.bookmarks.push({ dev, path, name: baseOf(path) });
  saveUI();
  drawPlaces();
  toast(t("bookmarked"));
}

async function searchPrompt() {
  const out = await ask({ title: t("searchTitle"), text: t("searchText"), fields: [{ key: "query", label: t("searchWhat") }], ok: t("searchGo") });
  if (out && out.query) search(out.query);
}

// properties shows what is known about a file or a folder; the size of a
// folder takes a moment to add up.
function properties(e) {
  const dev = fm.dev;
  const row = (label, value) => h("div", { class: "fm-prop" }, h("span", { class: "muted" }, label), h("span", { class: "fm-prop-value" }, value));
  const size = h("span", { class: "fm-prop-value" }, e.dir ? "…" : formatSize(e.size));
  const disk = h("span", { class: "fm-prop-value" }, "…");
  const date = e.modified ? new Date(e.modified * 1000).toLocaleString(language) : "—";
  const d = fmDevice(dev) || { name: "" };
  ask({
    title: e.name,
    content: h(
      "div",
      { class: "fm-props" },
      row(t("propType"), e.dir ? t("propFolder") : (extOf(e.name) || t("propFile")).toUpperCase()),
      h("div", { class: "fm-prop" }, h("span", { class: "muted" }, t("bySize")), size),
      row(t("byDate"), date),
      row(t("propWhere"), parentOf(e.path)),
      row(t("propDevice"), d.name),
      h("div", { class: "fm-prop" }, h("span", { class: "muted" }, t("propDisk")), disk),
    ),
  });
  api
    .FS(dev, { op: "props", path: e.path })
    .then((p) => {
      if (e.dir) size.textContent = formatSize(p.entry.size) + " · " + t("propFiles", p.count || 0) + (p.truncated ? " +" : "");
      disk.textContent = p.total ? t("propFree", formatSize(p.free), formatSize(p.total)) : "—";
    })
    .catch((err) => (size.textContent = String(err)));
}

async function newShare() {
  const hosts = [];
  if (state.self.deviceType !== "phone") hosts.push({ value: SELF, label: t("thisDeviceShort", state.self.deviceName) });
  for (const d of state.devices) if (d.connected && d.canBrowse && d.type !== "phone") hosts.push({ value: d.id, label: d.name });
  if (!hosts.length) return toast(t("shareNoHost"), true);
  const out = await ask({
    title: t("newShare"),
    text: t("newShareText"),
    fields: [
      { key: "name", label: t("folderName"), value: t("shareDefault") },
      { key: "host", options: hosts },
    ],
    ok: t("create"),
  });
  if (!out || !out.name) return;
  try {
    const made = await api.FS(out.host, { op: "share.create", to: out.name });
    loadOwnShares();
    refresh();
    fmGo(out.host, made.path);
  } catch (err) {
    toast(String(err), true);
  }
}

function entryMenu(e, x, y) {
  const list = picked();
  const one = list.length === 1;
  const tab = fm.tab;
  const other = otherTab();
  const phone = (fmDevice(fm.dev) || {}).type === "phone";
  menu(x, y, [
    one && { label: t("open"), icon: e.dir ? "folder" : "open", action: () => openEntry(e) },
    one && e.dir && { label: t("openInTab"), icon: "plus", action: () => addTab(tab.dev, e.path, true) },
    fm.dev !== SELF && { label: t("download"), icon: "download", action: () => download(list) },
    "-",
    { label: t("copy"), icon: "copy", action: () => toClip(list, false) },
    { label: t("cut"), icon: "cut", action: () => toClip(list, true) },
    one && e.dir && fm.clip && { label: t("pasteInto"), icon: "paste", action: () => paste(e.path) },
    other && other.res && other.res.path && { label: t("copyToOther"), icon: "forward", action: () => toOther(list, false) },
    other && other.res && other.res.path && { label: t("moveToOther"), icon: "move", action: () => toOther(list, true) },
    "-",
    { label: t("packZip"), icon: "archive", action: () => act(() => api.FS(tab.dev, { op: "zip", path: tab.res.search ? parentOf(e.path) : tab.res.path, paths: list.map((x) => x.path) }), t("packed")) },
    one && kindOf(e) === "archive" && { label: t("unpackHere"), icon: "folderPlus", action: () => act(() => api.FS(tab.dev, { op: "unzip", path: e.path, to: parentOf(e.path) }), t("unpacked")) },
    one && e.dir && { label: t("toBookmarks"), icon: "star", action: () => bookmark(tab.dev, e.path) },
    "-",
    one && { label: t("rename"), icon: "rename", action: () => rename(e) },
    { label: t(phone ? "deleteForever" : "toTrash"), icon: "trash", danger: true, action: () => removeEntries(list) },
    "-",
    one && { label: t("properties"), icon: "info", action: () => properties(e) },
  ]);
}

function folderMenu(x, y) {
  const tab = fm.tab;
  menu(x, y, [
    { label: t("paste"), icon: "paste", disabled: !fm.clip, action: () => paste() },
    "-",
    { label: t("newFolder"), icon: "folderPlus", action: newFolder },
    { label: t("upload"), icon: "upload", action: upload },
    "-",
    { label: t("selectAll"), icon: "check", action: selectAll },
    { label: t("searchTitle"), icon: "search", action: searchPrompt },
    { label: t("toBookmarks"), icon: "star", action: () => bookmark(tab.dev, tab.res.path) },
    { label: t("newTab"), icon: "plus", action: () => addTab(tab.dev, tab.res.path) },
    "-",
    { label: t("reload"), icon: "refresh", action: () => fmReload() },
    { label: t("properties"), icon: "info", action: () => properties({ name: baseOf(tab.res.path), path: tab.res.path, dir: true, modified: 0, size: 0 }) },
  ]);
}

function viewMenu(ev) {
  const box = ev.currentTarget.getBoundingClientRect();
  const redraw = () => {
    saveUI();
    for (const tab of shown()) inTab(tab, drawPane);
  };
  const by = (key, label) => ({
    label,
    icon: fm.sort.key === key ? "check" : "blank",
    action: () => {
      fm.sort = { key, dir: fm.sort.key === key ? -fm.sort.dir : 1 };
      redraw();
    },
  });
  menu(box.left, box.bottom + 4, [
    by("name", t("byName")),
    by("size", t("bySize")),
    by("modified", t("byDate")),
    "-",
    {
      label: t("hiddenFiles"),
      icon: fm.hidden ? "check" : "blank",
      action: () => {
        fm.hidden = !fm.hidden;
        redraw();
      },
    },
    "-",
    { label: t("searchTitle"), icon: "search", action: searchPrompt },
    { label: t("newTab"), icon: "plus", action: () => addTab(fm.dev, fm.res.path) },
    { label: t("selectAll"), icon: "check", action: selectAll },
  ]);
}

// Dragging

let drag = null;

// dropOn makes node a place files can be dropped on. where says which
// folder that is, or null for none; a folder still to be found out is
// given by a device alone.
function dropOn(node, where) {
  let depth = 0;
  node.addEventListener("dragover", (ev) => {
    const dest = where();
    if (!dest || (drag && dest.dir && drag.dev === dest.dev && drag.paths.some((p) => p === dest.dir || parentOf(p) === dest.dir))) return;
    ev.preventDefault();
    ev.stopPropagation();
    ev.dataTransfer.dropEffect = !drag || drag.dev !== dest.dev || ev.ctrlKey ? "copy" : "move";
  });
  node.addEventListener("dragenter", () => {
    if (where() && ++depth === 1) node.classList.add("drop");
  });
  node.addEventListener("dragleave", () => {
    if (--depth <= 0) {
      depth = 0;
      node.classList.remove("drop");
    }
  });
  node.addEventListener("drop", async (ev) => {
    depth = 0;
    node.classList.remove("drop");
    const dest = where();
    if (!dest) return;
    ev.preventDefault();
    ev.stopPropagation();
    const moved = drag;
    drag = null;
    try {
      if (!dest.dir) {
        // Dropped on a device: its downloads folder, or failing that its home.
        const p = await api.FS(dest.dev, { op: "places" });
        const place = (p.entries || []).find((x) => x.kind === "downloads") || (p.entries || []).find((x) => x.kind === "home") || (p.shares || [])[0];
        if (!place) throw t("accessOff");
        dest.dir = place.path;
      }
      if (moved) {
        const move = moved.dev === dest.dev && !ev.ctrlKey;
        act(() => api.FSPaste(moved.dev, moved.paths, dest.dev, dest.dir, move), t("pasting"));
      } else if (api.web && ev.dataTransfer.files.length) {
        uploadFiles(dest.dev, dest.dir, ev.dataTransfer.files);
      }
    } catch (err) {
      toast(String(err), true);
    }
  });
}
document.addEventListener("dragend", () => {
  drag = null;
  document.querySelectorAll(".drop").forEach((n) => n.classList.remove("drop"));
});

// Files dropped onto the window from the system's file manager come with
// their paths; they go into the folder under the pointer.
function watchSystemDrops() {
  if (!window.runtime || !window.runtime.OnFileDrop || watchSystemDrops.on) return;
  watchSystemDrops.on = true;
  window.runtime.OnFileDrop((x, y, paths) => {
    if (view.kind !== "files" || !paths || !paths.length) return;
    const under = document.elementFromPoint(x, y);
    const pane = under && under.closest(".fm-pane");
    const tab = (pane && fm.tabs.find((tb) => tb.root === pane)) || fm.tab;
    if (!tab || !tab.res || !tab.res.path) return;
    const item = under && under.closest("[data-path]");
    const folder = item && tab.res.entries.find((e) => e.dir && e.path === item.dataset.path);
    act(() => api.FSUploadPaths(tab.dev, folder ? folder.path : tab.res.path, paths), t("pasting"));
  }, false);
}

// band selects with a rectangle drawn over the empty part of the folder.
function band(ev, content) {
  if (coarse.matches || ev.button !== 0 || ev.target.closest("[data-path], button, input")) return;
  const start = { x: ev.clientX, y: ev.clientY };
  const keep = ev.ctrlKey || ev.shiftKey ? new Set(fm.selected) : new Set();
  const tab = fm.tab;
  let box = null;
  const moveTo = (e) => {
    if (!box && Math.abs(e.clientX - start.x) + Math.abs(e.clientY - start.y) < 6) return;
    if (!box) document.body.append((box = h("div", { class: "fm-band" })));
    const r = { left: Math.min(start.x, e.clientX), top: Math.min(start.y, e.clientY), right: Math.max(start.x, e.clientX), bottom: Math.max(start.y, e.clientY) };
    Object.assign(box.style, { left: r.left + "px", top: r.top + "px", width: r.right - r.left + "px", height: r.bottom - r.top + "px" });
    tab.selected = new Set(keep);
    for (const node of content.querySelectorAll("[data-path]")) {
      const b = node.getBoundingClientRect();
      if (b.left < r.right && b.right > r.left && b.top < r.bottom && b.bottom > r.top) tab.selected.add(node.dataset.path);
    }
    inTab(tab, syncSelection);
  };
  const stop = () => {
    document.removeEventListener("pointermove", moveTo);
    document.removeEventListener("pointerup", stop);
    if (!box) return;
    box.remove();
    // The click that ends the drag must not clear what was just selected.
    content.dataset.banded = "1";
    setTimeout(() => delete content.dataset.banded, 0);
  };
  document.addEventListener("pointermove", moveTo);
  document.addEventListener("pointerup", stop);
}

// Preview

function preview(e) {
  const kind = kindOf(e);
  const ext = extOf(e.name);
  const url = fileURL("raw", fm.dev, e.path);
  const note = h("span", { class: "fm-preview-note" });
  const wait = (label) => h("div", { class: "fm-preview-wait" }, h("div", { class: "pulse" }), h("p", { class: "muted" }, label));
  const failed = (err) => h("p", { class: "bad" }, t("previewFailed") + " " + String(err));
  const holder = h("div", { class: "fm-preview-body kind-" + kind, onclick: (ev) => ev.target === ev.currentTarget && close() });
  const put = (node) => document.body.contains(holder) && holder.replaceChildren(node);

  if (kind === "image") holder.append(h("img", { src: url, alt: "" }));
  else if (kind === "video") holder.append(h("video", { src: url, controls: true, autoplay: true }));
  else if (kind === "audio") holder.append(h("audio", { src: url, controls: true, autoplay: true }));
  else if (kind === "pdf") holder.append(pdfView(url, (info) => (note.textContent = info)));
  else if (kind === "archive") {
    holder.append(wait(t("loading")));
    api
      .FS(fm.dev, { op: "archive", path: e.path })
      .then((res) => put(archiveView(res)))
      .catch((err) => put(failed(err)));
  } else if (kind === "office") {
    holder.append(wait(t("converting")));
    api
      .FSPreview(fm.dev, e.path)
      .then((p) => {
        if (p.kind === "pdf") return put(pdfView(fileURL("raw", p.device, p.path), (info) => (note.textContent = info)));
        note.textContent = t("plainView");
        put(htmlView(p.html));
      })
      .catch((err) => put(failed(err)));
  } else {
    holder.append(wait(t("loading")));
    fetch(url, { headers: { Range: "bytes=0-524287" } })
      .then((r) => (r.ok ? r.text() : Promise.reject(r.statusText)))
      .then((text) => {
        if (e.size > 524288) note.textContent = t("textCut");
        put(ext === "md" ? markdownView(text) : codeView(text, ext));
      })
      .catch((err) => put(failed(err)));
  }

  const images = fmEntries().filter((x) => kindOf(x) === "image");
  const step = (by) => {
    const next = images[images.findIndex((x) => x.path === e.path) + by];
    if (next) preview(next);
  };
  const close = () => node.remove();
  const node = h(
    "div",
    {
      class: "fm-preview",
      tabindex: "-1",
      onkeydown: (ev) => {
        if (ev.key === "Escape") close();
        if (kind === "image" && ev.key === "ArrowRight") step(1);
        if (kind === "image" && ev.key === "ArrowLeft") step(-1);
        ev.stopPropagation();
      },
    },
    h(
      "div",
      { class: "fm-preview-bar" },
      h("span", { class: "fm-preview-name" }, e.name),
      note,
      kind === "image" && images.length > 1 ? iconButton("back", t("previous"), () => step(-1)) : null,
      kind === "image" && images.length > 1 ? iconButton("forward", t("next"), () => step(1)) : null,
      fm.dev !== SELF ? iconButton("download", t("download"), () => download([e])) : null,
      !api.web ? iconButton("open", t("openExternal"), () => act(() => api.FSOpen(fm.dev, e.path))) : null,
      iconButton("close", t("close"), close),
    ),
    holder,
  );
  document.querySelectorAll(".fm-preview").forEach((n) => n.remove());
  document.body.append(node);
  node.focus();
}

// Drawing

function drawTabs() {
  const strip = $("fm-tabs");
  if (!strip) return;
  strip.hidden = fm.tabs.length < 2;
  strip.replaceChildren(
    ...fm.tabs.map((tab, i) => {
      const d = (tab.dev && fmDevice(tab.dev)) || { type: "desktop", name: "" };
      const label = tab.res ? (tab.res.path ? baseOf(tab.res.path) : d.name) : "…";
      const node = h(
        "button",
        {
          class: "fm-tab" + (tab === fm.tab ? " active" : fm.panes.includes(i) ? " shown" : ""),
          title: tab.res ? d.name + " · " + tab.res.path : "",
          onclick: () => showTab(i),
          onauxclick: (e) => e.button === 1 && closeTab(i),
        },
        h("span", { class: "fm-tab-icon", icon: d.self ? "home" : d.type }),
        h("span", { class: "fm-tab-text" }, label),
        h("span", {
          class: "fm-tab-close",
          icon: "close",
          onclick: (e) => {
            e.stopPropagation();
            closeTab(i);
          },
        }),
      );
      if (tab.dev) tinted(node, tab.dev === SELF ? state.self.deviceId : tab.dev);
      return node;
    }),
    iconButton("plus", t("newTab"), () => addTab(fm.dev, fm.res ? fm.res.path : "")),
  );
}

function allShares() {
  return [...fm.ownShares.map((s) => ({ ...s, dev: SELF, owner: "" })), ...state.devices.filter((d) => d.connected).flatMap((d) => (d.shares || []).map((s) => ({ ...s, dev: d.id, owner: d.name })))];
}

function drawBar() {
  const bar = q(".fm-bar");
  const res = fm.res;
  if (!bar || !res) return;
  const tab = fm.tab;
  const d = fmDevice(fm.dev) || { name: "" };
  let crumbs;
  if (fm.pathEdit) {
    crumbs = h("input", {
      class: "fm-path",
      value: res.path,
      spellcheck: "false",
      onkeydown: (e) => {
        e.stopPropagation();
        if (e.key !== "Enter" && e.key !== "Escape") return;
        tab.pathEdit = false;
        if (e.key === "Enter" && e.target.value.trim()) fmGo(tab.dev, e.target.value.trim(), true, tab);
        inTab(tab, drawBar);
      },
      onblur: () => {
        if (!tab.pathEdit) return;
        tab.pathEdit = false;
        inTab(tab, drawBar);
      },
    });
    setTimeout(() => {
      crumbs.focus();
      crumbs.select();
    }, 0);
  } else {
    crumbs = h("div", {
      class: "fm-crumbs",
      title: t("editPath"),
      onclick: (e) => {
        if (e.target !== crumbs) return;
        tab.pathEdit = true;
        inTab(tab, drawBar);
      },
    });
    const crumb = (label, target, extra, iconName) => {
      const node = h("button", { class: "fm-crumb" + (extra || ""), onclick: () => fmGo(tab.dev, target, true, tab) }, iconName ? h("span", { class: "fm-crumb-icon", icon: iconName }) : null, label);
      dropOn(node, () => (target ? { dev: tab.dev, dir: target } : null));
      return node;
    };
    crumbs.append(crumb(d.name, "", " of-device", d.self ? "home" : d.type));
    if (res.path) {
      const sep = sepOf(res.path);
      let at = res.path.startsWith(sep) ? sep : "";
      const parts = res.path.split(sep).filter(Boolean);
      // Inside a network folder the path starts at the folder, not at the
      // disk; inside the home folder it starts at home, which the device
      // stands for.
      const inside = (p) => res.path === p.path || res.path.startsWith(p.path + sep);
      const share = allShares().find((s) => s.dev === fm.dev && inside(s));
      const home = fm.places.find((p) => p.kind === "home" && inside(p));
      const skip = share ? share.path.split(sep).filter(Boolean).length - 1 : home ? home.path.split(sep).filter(Boolean).length : 0;
      parts.forEach((part, i) => {
        at = at && !at.endsWith(sep) ? at + sep + part : at + part;
        if (i < skip) return;
        crumbs.append(h("span", { class: "fm-crumb-sep" }, "›"), crumb(part, at, i === parts.length - 1 ? " last" : ""));
      });
    }
  }
  const filter = h("input", {
    class: "fm-filter",
    placeholder: t("filter"),
    title: t("filterHint"),
    value: fm.filter,
    spellcheck: "false",
    oninput: (e) => {
      tab.filter = e.target.value;
      inTab(tab, drawContent);
    },
    onkeydown: (e) => {
      e.stopPropagation();
      if (e.key === "Enter" && e.target.value.trim()) inTab(tab, () => search(e.target.value.trim()));
      if (e.key === "Escape") e.target.blur();
    },
  });
  const flip = () => {
    fm.mode = fm.mode === "list" ? "grid" : "list";
    saveUI();
    for (const x of shown()) inTab(x, drawPane);
  };
  bar.replaceChildren(
    iconButton("back", t("back"), () => fmStep(tab.back, tab.forward), tab.back.length ? "" : " off"),
    iconButton("forward", t("forward"), () => fmStep(tab.forward, tab.back), (tab.forward.length ? "" : " off") + " fm-wide"),
    iconButton("up", t("up"), () => res.parent && fmGo(tab.dev, res.parent, true, tab), res.parent ? "" : " off"),
    crumbs,
    filter,
    iconButton("search", t("searchTitle"), searchPrompt, " fm-narrow"),
    iconButton(fm.mode === "list" ? "grid" : "list", t(fm.mode === "list" ? "viewIcons" : "viewList"), flip),
    h("button", { class: "icon-button", title: t("sort"), icon: "sort", onclick: viewMenu }),
    iconButton("folderPlus", t("newFolder"), newFolder, res.path ? "" : " off"),
    iconButton("upload", t("upload"), upload, res.path ? "" : " off"),
    iconButton("split", t("split"), toggleSplit, " fm-wide" + (fm.panes[1] !== null ? " on" : "")),
  );
  if (!fm.pathEdit) crumbs.scrollLeft = crumbs.scrollWidth;
}

function placeButton(iconName, label, active, action, onmenu, dest) {
  const node = h("button", { class: "fm-place" + (active ? " active" : ""), onclick: action, oncontextmenu: onmenu || ((e) => e.preventDefault()) }, h("span", { class: "fm-place-icon", icon: iconName }), h("span", { class: "fm-place-text" }, label));
  if (dest) dropOn(node, () => dest);
  return node;
}

function drawPlaces() {
  if (!$("fm-places") || !fm.res) return;
  const here = fm.res.path;
  const out = [];
  if (fm.places.length) {
    out.push(h("div", { class: "fm-places-head" }, t("places")));
    for (const p of fm.places) out.push(placeButton(placeIcons[p.kind] || "folder", p.kind === "home" ? t("homeFolder") : p.name, here === p.path, () => fmGo(fm.dev, p.path), null, { dev: fm.dev, dir: p.path }));
  }

  const forget = (list, item, e) => {
    e.preventDefault();
    menu(e.clientX, e.clientY, [
      {
        label: t("forgetPlace"),
        icon: "close",
        action: () => {
          fm[list] = fm[list].filter((x) => x !== item);
          saveUI();
          drawPlaces();
        },
      },
    ]);
  };
  const suffix = (dev) => (dev === fm.dev ? "" : " · " + ((fmDevice(dev) || {}).name || "?"));
  if (fm.bookmarks.length) {
    out.push(h("div", { class: "fm-places-head" }, t("bookmarks")));
    for (const b of fm.bookmarks) out.push(placeButton("star", b.name + suffix(b.dev), fm.dev === b.dev && here === b.path, () => fmGo(b.dev, b.path), (e) => forget("bookmarks", b, e), { dev: b.dev, dir: b.path }));
  }

  out.push(h("div", { class: "fm-places-head" }, t("shares"), iconButton("plus", t("newShare"), newShare)));
  const shares = allShares();
  if (!shares.length) out.push(h("p", { class: "fm-places-empty" }, t("noShares")));
  for (const s of shares) {
    out.push(
      placeButton(
        "share",
        s.owner ? s.name + " · " + s.owner : s.name,
        fm.dev === s.dev && here === s.path,
        () => fmGo(s.dev, s.path),
        (e) => {
          e.preventDefault();
          menu(e.clientX, e.clientY, [
            { label: t("open"), icon: "folder", action: () => fmGo(s.dev, s.path) },
            {
              label: t("shareRemove"),
              icon: "close",
              danger: true,
              action: async () => {
                if (!(await ask({ title: t("shareRemove"), text: t("shareRemoveText", s.name), ok: t("shareRemoveOk") }))) return;
                try {
                  await api.FS(s.dev, { op: "share.remove", path: s.path });
                } catch (err) {
                  toast(String(err), true);
                }
                loadOwnShares();
                refresh();
              },
            },
          ]);
        },
        { dev: s.dev, dir: s.path },
      ),
    );
  }

  out.push(h("div", { class: "fm-places-head" }, t("devices")));
  out.push(tinted(placeButton(state.self.deviceType === "phone" ? "phone" : "desktop", t("thisDeviceShort", state.self.deviceName), fm.dev === SELF, () => fmGo(SELF, ""), null, { dev: SELF, dir: "" }), state.self.deviceId));
  for (const d of state.devices) if (d.connected && d.canBrowse) out.push(tinted(placeButton(d.type, d.name, fm.dev === d.id, () => fmGo(d.id, ""), null, { dev: d.id, dir: "" }), d.id));

  if (fm.recents.length) {
    out.push(h("div", { class: "fm-places-head" }, t("recents")));
    for (const r of fm.recents) out.push(placeButton(typeIcons[kindOf(r)] || "doc", r.name, false, () => reveal(r.dev, r.path), (e) => forget("recents", r, e)));
  }
  $("fm-places").replaceChildren(...out);
}

function nameNode(e) {
  const tab = fm.tab;
  if (fm.renaming !== e.path) {
    // What a search found says where it is.
    const where = fm.res.search ? parentOf(e.path).slice(fm.res.base.path.length).replace(/^[\\/]/, "") : "";
    return h("span", { class: "fm-name" }, e.name, where ? h("span", { class: "fm-where" }, where) : null);
  }
  const input = h("input", {
    class: "fm-rename",
    value: e.name,
    spellcheck: "false",
    draggable: "false",
    onclick: (ev) => ev.stopPropagation(),
    ondblclick: (ev) => ev.stopPropagation(),
    onkeydown: (ev) => {
      ev.stopPropagation();
      if (ev.key !== "Enter" && ev.key !== "Escape") return;
      const name = ev.target.value.trim();
      tab.renaming = null;
      if (ev.key === "Enter" && name && name !== e.name) act(() => api.FS(tab.dev, { op: "rename", path: e.path, to: name }));
      else inTab(tab, drawContent);
    },
    onblur: () => {
      if (tab.renaming === e.path) {
        tab.renaming = null;
        inTab(tab, drawContent);
      }
    },
  });
  setTimeout(() => {
    input.focus();
    const dot = e.dir ? -1 : e.name.lastIndexOf(".");
    input.setSelectionRange(0, dot > 0 ? dot : e.name.length);
  }, 0);
  return input;
}

function entryNode(e) {
  const tab = fm.tab;
  const kind = kindOf(e);
  let picture = h("span", { class: "fm-icon k-" + kind, icon: typeIcons[kind] });
  if (fm.mode === "grid" && thumbable.includes(extOf(e.name))) {
    const img = h("img", { class: "fm-thumb", loading: "lazy", alt: "", draggable: "false", src: fileURL("thumb", fm.dev, e.path, "&size=192&m=" + e.modified) });
    const fallback = picture;
    img.addEventListener("error", () => img.replaceWith(fallback));
    picture = img;
  }
  const date = e.modified ? new Date(e.modified * 1000).toLocaleDateString(language, { day: "2-digit", month: "short", year: "numeric" }) : "";
  const node = h(
    "div",
    {
      class: (fm.mode === "grid" ? "fm-tile" : "fm-row") + (fm.selected.has(e.path) ? " sel" : ""),
      "data-path": e.path,
      title: e.name,
      draggable: coarse.matches || fm.renaming === e.path ? "false" : "true",
      onclick: (ev) => {
        ev.stopPropagation();
        if (coarse.matches && !tab.selecting) return openEntry(e);
        select(e, ev);
      },
      ondblclick: (ev) => {
        ev.stopPropagation();
        if (!coarse.matches) openEntry(e);
      },
      oncontextmenu: (ev) => {
        ev.preventDefault();
        ev.stopPropagation();
        if (coarse.matches) {
          // A long press starts picking files; the actions wait in the bar below.
          tab.selecting = true;
          if (!tab.selected.has(e.path)) tab.selected.add(e.path);
          tab.anchor = e.path;
          return syncSelection();
        }
        if (!tab.selected.has(e.path)) select(e);
        entryMenu(e, ev.clientX, ev.clientY);
      },
      ondragstart: (ev) => {
        if (!tab.selected.has(e.path)) inTab(tab, () => select(e));
        drag = { dev: tab.dev, paths: [...tab.selected] };
        ev.dataTransfer.effectAllowed = "copyMove";
        ev.dataTransfer.setData("text/plain", drag.paths.join("\n"));
      },
    },
    h("span", { class: "fm-mark", icon: "check" }),
    picture,
    nameNode(e),
  );
  if (fm.mode === "list") node.append(h("span", { class: "fm-meta" }, e.dir ? "" : formatSize(e.size)), h("span", { class: "fm-meta date" }, date));
  if (e.dir) dropOn(node, () => ({ dev: tab.dev, dir: e.path }));
  return node;
}

function drawContent() {
  const content = q(".fm-content");
  if (!content || !fm.res) return;
  const tab = fm.tab;
  const entries = fmEntries();
  content.className = "fm-content " + fm.mode;
  const head = (key, label, extra) =>
    h(
      "button",
      {
        class: "fm-col" + (extra || "") + (fm.sort.key === key ? " on" : ""),
        onclick: (ev) => {
          ev.stopPropagation();
          fm.sort = { key, dir: fm.sort.key === key ? -fm.sort.dir : 1 };
          saveUI();
          for (const x of shown()) inTab(x, drawContent);
        },
      },
      label,
      fm.sort.key === key ? (fm.sort.dir > 0 ? " ↑" : " ↓") : "",
    );
  const out = [];
  if (fm.res.search) {
    out.push(
      h(
        "div",
        { class: "fm-banner" },
        h("span", { class: "fm-banner-icon", icon: "search" }),
        h("span", { class: "fm-banner-text" }, t("searchResults", fm.res.search, entries.length) + (fm.res.truncated ? " " + t("searchMore") : "")),
        h("button", { class: "button small", onclick: () => fmReload(tab) }, t("searchClose")),
      ),
    );
  }
  if (fm.mode === "list") out.push(h("div", { class: "fm-head" }, h("span", {}), head("name", t("byName")), head("size", t("bySize"), " right"), head("modified", t("byDate"), " right date")));
  if (fm.res.denied) out.push(h("p", { class: "muted fm-empty" }, t("accessOff")));
  else if (!entries.length) out.push(h("p", { class: "muted fm-empty" }, t(fm.filter || fm.res.search ? "nothingFound" : "emptyFolder")));
  else out.push(h("div", { class: "fm-items" }, ...entries.map(entryNode)));
  if (fm.res.truncated && !fm.res.search) out.push(h("p", { class: "muted small fm-empty" }, t("truncated")));
  content.replaceChildren(...out);
  drawStatus();
}

function drawStatus() {
  const bar = q(".fm-status");
  if (!bar || !fm.res) return;
  const tab = fm.tab;
  const list = picked();
  const left = h("span", { class: "fm-status-text" });
  if (list.length) {
    const size = list.reduce((sum, e) => sum + (e.dir ? 0 : e.size), 0);
    left.textContent = t("selected", list.length) + (size ? " · " + formatSize(size) : "");
  } else {
    const entries = fmEntries();
    const folders = entries.filter((e) => e.dir).length;
    left.textContent = t("counts", folders, entries.length - folders);
  }
  const out = [left];

  const job = (state.transfers || []).find((tr) => tr.state === "active");
  if (job) {
    const fill = h("i", {});
    fill.style.width = (job.size ? Math.min(100, (job.done / job.size) * 100) : 100) + "%";
    out.push(h("span", { class: "fm-job" + (job.size ? "" : " busy") }, h("span", { class: "fm-job-name" }, job.name), h("span", { class: "fm-job-bar" }, fill)));
  }

  const actions = h("span", { class: "fm-actions" });
  const run = (fn) => () => inTab(tab, fn);
  if (list.length) {
    const other = inTab(tab, otherTab);
    if (other && tab === fm.tab && other.res && other.res.path) actions.append(iconButton("forward", t("copyToOther"), run(() => toOther(list, false)), " fm-wide"));
    if (fm.dev !== SELF) actions.append(iconButton("download", t("download"), run(() => download(list))));
    actions.append(iconButton("copy", t("copy"), run(() => toClip(list, false))), iconButton("cut", t("cut"), run(() => toClip(list, true))));
    if (list.length === 1) actions.append(iconButton("rename", t("rename"), run(() => rename(list[0]))));
    actions.append(
      iconButton("trash", t("delete"), run(() => removeEntries(list)), " danger"),
      iconButton("close", t("deselect"), run(() => {
        tab.selected.clear();
        syncSelection();
      })),
    );
  } else if (fm.clip && fm.res.path && !fm.res.search) {
    actions.append(
      h("button", { class: "button primary small", onclick: run(() => paste()) }, h("span", { class: "button-icon", icon: "paste" }), t(fm.clip.cut ? "moveHere" : "pasteHere", fm.clip.paths.length)),
      iconButton("close", t("cancel"), () => {
        fm.clip = null;
        for (const x of shown()) inTab(x, drawStatus);
      }),
    );
  }
  out.push(actions);
  bar.replaceChildren(...out);
}

function drawPane() {
  if (!fm.root || !fm.res) return;
  tinted(fm.root, fm.dev === SELF ? state.self.deviceId : fm.dev);
  fm.root.classList.toggle("selecting", fm.selecting);
  drawBar();
  drawContent();
}

// layout builds the panels for the tabs on screen.
function layout() {
  const holder = $("fm-panes");
  if (!holder) return;
  if (narrow.matches && fm.panes[1] !== null) {
    fm.panes[1] = null;
    fm.side = 0;
  }
  fm.tab = fm.tabs[fm.panes[fm.side]];
  for (const tab of fm.tabs) tab.root = null;
  holder.classList.toggle("split", fm.panes[1] !== null);
  holder.replaceChildren(
    ...fm.panes.map((index, side) => {
      if (index === null) return null;
      const tab = fm.tabs[index];
      const content = h("div", {
        class: "fm-content",
        onclick: () => {
          if (content.dataset.banded) return;
          tab.selected.clear();
          inTab(tab, syncSelection);
        },
        oncontextmenu: (e) => {
          e.preventDefault();
          if (tab.res && tab.res.path && !tab.res.search) inTab(tab, () => folderMenu(e.clientX, e.clientY));
        },
        onpointerdown: (e) => inTab(tab, () => band(e, content)),
      });
      dropOn(content, () => (tab.res && tab.res.path && !tab.res.search ? { dev: tab.dev, dir: tab.res.path } : null));
      const root = h(
        "div",
        { class: "fm-pane" + (tab === fm.tab ? " active" : "") },
        h("div", { class: "fm-top" }, side === 0 ? iconButton("close", t("close"), () => show(narrow.matches ? { kind: "home" } : { kind: "device", id: fm.dev === SELF ? null : fm.dev }), " fm-leave") : null, h("div", { class: "fm-bar" })),
        content,
        h("div", { class: "fm-status" }),
      );
      // Whatever is touched in a panel makes it the one in use first.
      root.addEventListener("pointerdown", () => activate(side), true);
      root.addEventListener("focusin", () => activate(side));
      tab.root = root;
      return root;
    }).filter(Boolean),
  );
  for (const tab of shown()) inTab(tab, drawPane);
  drawTabs();
  drawPlaces();
}

// Entry points for the rest of the interface

async function fmShow(dev, path) {
  await loadUI();
  $("view").replaceChildren(h("div", { id: "fm", class: "fm" }, h("div", { id: "fm-tabs", class: "fm-tabs", hidden: true }), h("div", { class: "fm-main" }, h("div", { id: "fm-places", class: "fm-places" }), h("div", { id: "fm-panes", class: "fm-panes" }))));
  watchSystemDrops();
  loadOwnShares();
  // Coming back, the tabs are as they were left, unless a place was asked for.
  if (fm.tabs.length && !path && fm.tabs.every((tab) => tab.res)) {
    layout();
    for (const tab of shown()) fmReload(tab);
    return;
  }
  const tab = newTab();
  fm.tabs = [tab];
  fm.panes = [0, null];
  fm.side = 0;
  fm.tab = tab;
  layout();
  if (path) {
    // Asked to show a file: open its folder and the file on top.
    try {
      const stat = await api.FS(dev, { op: "stat", path });
      if (stat.entry && !stat.entry.dir) {
        if (await fmGo(dev, parentOf(path), false, tab)) inTab(tab, () => preview(stat.entry));
        return;
      }
    } catch (err) {
      // The listing below reports it.
    }
  }
  if (!(await fmGo(dev, path || "", false, tab)) && !(await fmGo(SELF, "", false, tab)) && view.kind === "files") show({ kind: narrow.matches ? "home" : "device", id: null });
}

// fmState follows the state of the daemon: a finished transfer may have
// changed the folders on screen.
function fmState() {
  if (view.kind !== "files" || !fm.tab || !fm.res) return;
  const active = (state.transfers || []).filter((tr) => tr.state === "active").length;
  for (const tab of shown()) {
    if (!tab.res) continue;
    if (active < fm.active && !tab.res.search) fmReload(tab);
    else inTab(tab, drawStatus);
  }
  if (active >= fm.active) drawPlaces();
  fm.active = active;
}

// fmBack is the phone's Back button inside the file manager.
function fmBack() {
  const open = document.querySelector(".fm-preview, .fm-dialog-back, .fm-menu");
  if (open) open.remove();
  else if (fm.selected.size) {
    fm.selected.clear();
    syncSelection();
  } else if (fm.res && fm.res.search) fmReload();
  else if (fm.back.length) fmStep(fm.back, fm.forward);
  else if (fm.tabs.length > 1) closeTab(fm.tabs.indexOf(fm.tab));
  else return false;
  return true;
}

document.addEventListener("keydown", (e) => {
  if (typeof view === "undefined" || view.kind !== "files" || !fm.tab || !fm.res) return;
  if (document.querySelector(".fm-dialog-back, .fm-preview") || /^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName)) return;
  const list = picked();
  const ctrl = e.ctrlKey || e.metaKey;
  const entries = fmEntries();
  const move = (by) => {
    const at = entries.findIndex((x) => x.path === fm.anchor);
    const next = entries[Math.max(0, Math.min(entries.length - 1, at < 0 ? 0 : at + by))];
    if (!next) return;
    select(next, e.shiftKey ? e : null);
    const node = [...fm.root.querySelectorAll("[data-path]")].find((n) => n.dataset.path === next.path);
    if (node) node.scrollIntoView({ block: "nearest" });
  };
  // The letters are matched by key position, so they work in any layout.
  if (ctrl && e.code === "KeyA") selectAll();
  else if (ctrl && e.code === "KeyC") toClip(list, false);
  else if (ctrl && e.code === "KeyX") toClip(list, true);
  else if (ctrl && e.code === "KeyV") paste();
  else if (ctrl && e.code === "KeyT") addTab(fm.dev, fm.res.path);
  else if (ctrl && e.code === "KeyW") closeTab(fm.tabs.indexOf(fm.tab));
  else if (ctrl && e.code === "KeyL") {
    fm.pathEdit = true;
    drawBar();
  } else if (ctrl && e.code === "KeyF") {
    const filter = q(".fm-filter");
    if (filter && filter.offsetParent) filter.focus();
    else searchPrompt();
  } else if (ctrl && e.code === "KeyH") {
    fm.hidden = !fm.hidden;
    saveUI();
    for (const tab of shown()) inTab(tab, drawContent);
  } else if (e.key === "F3") toggleSplit();
  else if (e.key === "Tab" && fm.panes[1] !== null) activate(1 - fm.side);
  else if (e.key === "Delete") removeEntries(list);
  else if (e.key === "F2" && list.length === 1) rename(list[0]);
  else if (e.key === "F5") fmReload();
  else if (e.key === "Enter" && e.altKey && list.length === 1) properties(list[0]);
  else if (e.key === "Enter" && list.length === 1) openEntry(list[0]);
  else if (e.key === "Backspace" || (e.altKey && e.key === "ArrowUp")) fm.res.parent && fmGo(fm.dev, fm.res.parent);
  else if (e.altKey && e.key === "ArrowLeft") fmStep(fm.back, fm.forward);
  else if (e.altKey && e.key === "ArrowRight") fmStep(fm.forward, fm.back);
  else if (e.key === "ArrowDown" || e.key === "ArrowRight") move(1);
  else if (e.key === "ArrowUp" || e.key === "ArrowLeft") move(-1);
  else if (e.key === "Escape") {
    if (fm.res.search) return fmReload();
    fm.selected.clear();
    syncSelection();
  } else return;
  e.preventDefault();
});
