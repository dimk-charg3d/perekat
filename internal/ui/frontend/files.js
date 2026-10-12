// The file manager: the files of this device and of every paired one, laid
// out the way desktop file managers are: places on the left, a path bar and
// a toolbar on top, the folder in the middle, a status bar below.
//
// It is built once per visit and its parts are redrawn on their own, so that
// selecting a file does not disturb the scroll position or a typed filter.

const SELF = "self";
const coarse = window.matchMedia("(pointer: coarse)");

const fm = {
  dev: null,
  res: null,
  places: [],
  ownShares: [],
  back: [],
  forward: [],
  selected: new Set(),
  anchor: null,
  selecting: false,
  sort: { key: "name", dir: 1 },
  mode: "list",
  hidden: false,
  filter: "",
  clip: null,
  renaming: null,
  active: 0,
  denied: false,
};
try {
  fm.mode = localStorage.getItem("fm.mode") || "list";
} catch (err) {
  // No storage: the list it is.
}

const sepOf = (path) => (path.includes("\\") && !path.includes("/") ? "\\" : "/");
const joinPath = (dir, name) => dir.replace(/[\\/]+$/, "") + sepOf(dir) + name;
const extOf = (name) => (name.includes(".") ? name.slice(name.lastIndexOf(".") + 1).toLowerCase() : "");

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
                class: "fm-menu-item" + (item.danger ? " danger" : "") + (item.on ? " on" : ""),
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

// ask shows a small form and resolves to its values, or to null when dismissed.
function ask({ title, text, fields, ok, danger }) {
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
      ...inputs,
      h("div", { class: "row between" }, h("button", { type: "button", class: "button", onclick: () => done(null) }, t("cancel")), h("button", { class: "button primary" + (danger ? " danger" : "") }, ok)),
    );
    const back = h("div", { class: "fm-dialog-back", onpointerdown: (e) => e.target === back && done(null) }, form);
    back.addEventListener("keydown", (e) => e.key === "Escape" && done(null));
    document.body.append(back);
    const first = inputs.find((i) => i.tagName === "INPUT") || form.querySelector("button.primary");
    first.focus();
    if (first.select) first.select();
  });
}

// Navigation

async function fmGo(dev, path, record) {
  const from = fm.res && fm.dev ? { dev: fm.dev, path: fm.res.path } : null;
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
  if (dev !== fm.dev) {
    fm.places = [];
    api
      .FS(dev, { op: "places" })
      .then((p) => {
        if (fm.dev !== dev) return;
        fm.places = p.entries || [];
        if (res.denied && p.shares && p.shares.length) fmGo(dev, p.shares[0].path, false);
        drawBar();
        drawPlaces();
      })
      .catch(() => {});
  }
  if (record !== false && from && (from.dev !== dev || from.path !== res.path)) {
    fm.back.push(from);
    fm.forward = [];
  }
  fm.dev = dev;
  fm.res = res;
  fm.selected.clear();
  fm.selecting = false;
  fm.renaming = null;
  fm.filter = "";
  view.id = dev;
  view.path = res.path;
  drawAll();
  return true;
}

const fmReload = () => fm.res && fmGo(fm.dev, fm.res.path, false);

async function fmStep(from, to) {
  const target = from.pop();
  if (!target) return;
  const here = { dev: fm.dev, path: fm.res.path };
  if (await fmGo(target.dev, target.path, false)) to.push(here);
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

// Entries and selection

function fmEntries() {
  const needle = fm.filter.toLowerCase();
  const list = fm.res.entries.filter((e) => (fm.hidden || !e.name.startsWith(".")) && (!needle || e.name.toLowerCase().includes(needle)));
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
  for (const node of $("fm-content").querySelectorAll("[data-path]")) node.classList.toggle("sel", fm.selected.has(node.dataset.path));
  if (!fm.selected.size) fm.selecting = false;
  $("fm").classList.toggle("selecting", fm.selecting);
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

// Actions

async function act(action, done) {
  try {
    await action();
    if (done) toast(done);
  } catch (err) {
    toast(String(err), true);
  }
  fmReload();
}

function openEntry(e) {
  if (e.dir) return fmGo(fm.dev, e.path);
  const kind = kindOf(e);
  // The window plays no video itself; the system's player does it better.
  if (kind !== "doc" && (api.web || (kind !== "video" && kind !== "audio"))) return preview(e);
  if (!api.web) return act(() => api.FSOpen(fm.dev, e.path), fm.dev === SELF ? "" : t("opening", e.name));
  toast(t("noPreview"));
}

function download(entries) {
  act(() => api.FSPaste(fm.dev, entries.map((e) => e.path), SELF, "", false), t("downloading"));
}

function toClip(entries, cut) {
  if (!entries.length) return;
  fm.clip = { dev: fm.dev, paths: entries.map((e) => e.path), cut };
  toast(t(cut ? "cutDone" : "copyDone", entries.length));
  drawStatus();
}

function paste() {
  const clip = fm.clip;
  if (!clip || !fm.res.path) return;
  if (clip.cut) fm.clip = null;
  act(() => api.FSPaste(clip.dev, clip.paths, fm.dev, fm.res.path, clip.cut), t("pasting"));
}

async function removeEntries(entries) {
  if (!entries.length) return;
  const d = fmDevice(fm.dev) || {};
  // A phone has no trash can: what is deleted there is gone.
  if (d.type === "phone") {
    const sure = await ask({ title: t("deleteTitle"), text: t("deleteText", entries.length === 1 ? entries[0].name : t("items", entries.length)), ok: t("deleteForever"), danger: true });
    if (!sure) return;
  }
  act(async () => {
    for (const e of entries) await fmCall("delete", e.path);
  }, t(d.type === "phone" ? "deleted" : "trashed"));
}

async function newFolder() {
  const out = await ask({ title: t("newFolder"), fields: [{ key: "name", label: t("folderName"), value: t("newFolder") }], ok: t("create") });
  if (out && out.name) act(() => fmCall("mkdir", joinPath(fm.res.path, out.name)));
}

async function upload() {
  if (!api.web) {
    return act(
      () => api.FSUpload(fm.dev, fm.res.path, t("chooseFiles")),
      "",
    );
  }
  const input = h("input", { type: "file", multiple: true });
  input.addEventListener("change", () => {
    const form = new FormData();
    for (const file of input.files) form.append("file", file, file.name);
    if (input.files.length) act(() => api.FSUploadForm(fm.dev, fm.res.path, form), t("uploaded"));
  });
  input.click();
}

function rename(e) {
  fm.renaming = e.path;
  drawContent();
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
  menu(x, y, [
    one && { label: t("open"), icon: e.dir ? "folder" : "open", action: () => openEntry(e) },
    fm.dev !== SELF && { label: t("download"), icon: "download", action: () => download(list) },
    "-",
    { label: t("copy"), icon: "copy", action: () => toClip(list, false) },
    { label: t("cut"), icon: "cut", action: () => toClip(list, true) },
    one && e.dir && fm.clip && { label: t("pasteInto"), icon: "paste", action: () => act(() => api.FSPaste(fm.clip.dev, fm.clip.paths, fm.dev, e.path, fm.clip.cut), t("pasting")) },
    "-",
    one && { label: t("rename"), icon: "rename", action: () => rename(e) },
    { label: t(fmDevice(fm.dev) && fmDevice(fm.dev).type === "phone" ? "deleteForever" : "toTrash"), icon: "trash", danger: true, action: () => removeEntries(list) },
  ]);
}

function folderMenu(x, y) {
  menu(x, y, [
    { label: t("paste"), icon: "paste", disabled: !fm.clip, action: paste },
    "-",
    { label: t("newFolder"), icon: "folderPlus", action: newFolder },
    { label: t("upload"), icon: "upload", action: upload },
    "-",
    { label: t("selectAll"), icon: "check", action: selectAll },
    { label: t("reload"), icon: "refresh", action: fmReload },
  ]);
}

function selectAll() {
  for (const e of fmEntries()) fm.selected.add(e.path);
  fm.selecting = coarse.matches;
  syncSelection();
}

function sortMenu(ev) {
  const box = ev.currentTarget.getBoundingClientRect();
  const by = (key, label) => ({
    label,
    icon: fm.sort.key === key ? "check" : "blank",
    action: () => {
      fm.sort = { key, dir: fm.sort.key === key ? -fm.sort.dir : 1 };
      drawContent();
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
        drawContent();
      },
    },
  ]);
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

const iconButton = (name, title, action, extra) => h("button", { class: "icon-button" + (extra || ""), title, icon: name, onclick: action });

function drawBar() {
  const res = fm.res;
  const d = fmDevice(fm.dev) || { name: "" };
  const crumbs = h("div", { class: "fm-crumbs" });
  crumbs.append(h("button", { class: "fm-crumb device", onclick: () => fmGo(fm.dev, "") }, h("span", { class: "fm-crumb-icon", icon: d.self ? "home" : d.type }), d.name));
  if (res.path) {
    const sep = sepOf(res.path);
    let at = res.path.startsWith(sep) ? sep : "";
    const parts = res.path.split(sep).filter(Boolean);
    // Inside a network folder the path starts at the folder, not at the disk.
    const share = [...fm.ownShares.map((s) => ({ ...s, dev: SELF })), ...state.devices.flatMap((x) => (x.shares || []).map((s) => ({ ...s, dev: x.id })))].find(
      (s) => s.dev === fm.dev && (res.path === s.path || res.path.startsWith(s.path + sep)),
    );
    // And inside the home folder it starts at home, which the device stands for.
    const home = fm.places.find((p) => p.kind === "home" && (res.path === p.path || res.path.startsWith(p.path + sep)));
    const skip = share ? share.path.split(sep).filter(Boolean).length - 1 : home ? home.path.split(sep).filter(Boolean).length : 0;
    parts.forEach((part, i) => {
      at = at && !at.endsWith(sep) ? at + sep + part : at + part;
      if (i < skip) return;
      const target = at;
      crumbs.append(h("span", { class: "fm-crumb-sep" }, "›"), h("button", { class: "fm-crumb" + (i === parts.length - 1 ? " last" : ""), onclick: () => fmGo(fm.dev, target) }, part));
    });
  }
  const filter = h("input", {
    class: "fm-filter",
    placeholder: t("filter"),
    value: fm.filter,
    spellcheck: "false",
    oninput: (e) => {
      fm.filter = e.target.value;
      drawContent();
    },
  });
  $("fm-bar").replaceChildren(
    iconButton("back", t("back"), () => fmStep(fm.back, fm.forward), fm.back.length ? "" : " off"),
    iconButton("forward", t("forward"), () => fmStep(fm.forward, fm.back), (fm.forward.length ? "" : " off") + " fm-wide"),
    iconButton("up", t("up"), () => res.parent && fmGo(fm.dev, res.parent), res.parent ? "" : " off"),
    crumbs,
    filter,
    iconButton(fm.mode === "list" ? "grid" : "list", t(fm.mode === "list" ? "viewIcons" : "viewList"), () => {
      fm.mode = fm.mode === "list" ? "grid" : "list";
      try {
        localStorage.setItem("fm.mode", fm.mode);
      } catch (err) {
        // Not remembered, then.
      }
      drawBar();
      drawContent();
    }),
    h("button", { class: "icon-button", title: t("sort"), icon: "sort", onclick: sortMenu }),
    iconButton("folderPlus", t("newFolder"), newFolder, res.path ? "" : " off"),
    iconButton("upload", t("upload"), upload, res.path ? "" : " off"),
  );
  crumbs.scrollLeft = crumbs.scrollWidth;
}

function placeButton(iconName, label, active, action, onmenu) {
  return h("button", { class: "fm-place" + (active ? " active" : ""), onclick: action, oncontextmenu: onmenu || ((e) => e.preventDefault()) }, h("span", { class: "fm-place-icon", icon: iconName }), h("span", { class: "fm-place-text" }, label));
}

function drawPlaces() {
  if (!$("fm-places") || !fm.res) return;
  const here = fm.res.path;
  const out = [];
  if (fm.places.length) {
    out.push(h("div", { class: "fm-places-head" }, t("places")));
    for (const p of fm.places) out.push(placeButton(placeIcons[p.kind] || "folder", p.kind === "home" ? t("homeFolder") : p.name, here === p.path, () => fmGo(fm.dev, p.path)));
  }

  const shares = [...fm.ownShares.map((s) => ({ ...s, dev: SELF, owner: "" })), ...state.devices.filter((d) => d.connected).flatMap((d) => (d.shares || []).map((s) => ({ ...s, dev: d.id, owner: d.name })))];
  out.push(h("div", { class: "fm-places-head" }, t("shares"), iconButton("plus", t("newShare"), newShare)));
  if (!shares.length) out.push(h("p", { class: "fm-places-empty" }, t("noShares")));
  for (const s of shares) {
    out.push(
      placeButton("share", s.owner ? s.name + " · " + s.owner : s.name, fm.dev === s.dev && here === s.path, () => fmGo(s.dev, s.path), (e) => {
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
      }),
    );
  }

  out.push(h("div", { class: "fm-places-head" }, t("devices")));
  out.push(tinted(placeButton(state.self.deviceType === "phone" ? "phone" : "desktop", t("thisDeviceShort", state.self.deviceName), fm.dev === SELF, () => fmGo(SELF, "")), state.self.deviceId));
  for (const d of state.devices) if (d.connected && d.canBrowse) out.push(tinted(placeButton(d.type, d.name, fm.dev === d.id, () => fmGo(d.id, "")), d.id));
  $("fm-places").replaceChildren(...out);
}

function nameNode(e) {
  if (fm.renaming !== e.path) return h("span", { class: "fm-name" }, e.name);
  const input = h("input", {
    class: "fm-rename",
    value: e.name,
    spellcheck: "false",
    onclick: (ev) => ev.stopPropagation(),
    ondblclick: (ev) => ev.stopPropagation(),
    onkeydown: (ev) => {
      ev.stopPropagation();
      if (ev.key !== "Enter" && ev.key !== "Escape") return;
      const name = ev.target.value.trim();
      fm.renaming = null;
      if (ev.key === "Enter" && name && name !== e.name) act(() => fmCall("rename", e.path, { to: name }));
      else drawContent();
    },
    onblur: () => {
      if (fm.renaming === e.path) {
        fm.renaming = null;
        drawContent();
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
  const kind = kindOf(e);
  const iconName = typeIcons[kind];
  let picture = h("span", { class: "fm-icon " + kind, icon: iconName });
  if (fm.mode === "grid" && thumbable.includes(extOf(e.name))) {
    const img = h("img", { class: "fm-thumb", loading: "lazy", alt: "", src: fileURL("thumb", fm.dev, e.path, "&size=192&m=" + e.modified) });
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
      onclick: (ev) => {
        ev.stopPropagation();
        if (coarse.matches && !fm.selecting) return openEntry(e);
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
          fm.selecting = true;
          if (!fm.selected.has(e.path)) fm.selected.add(e.path);
          fm.anchor = e.path;
          return syncSelection();
        }
        if (!fm.selected.has(e.path)) select(e);
        entryMenu(e, ev.clientX, ev.clientY);
      },
    },
    h("span", { class: "fm-mark", icon: "check" }),
    picture,
    nameNode(e),
  );
  if (fm.mode === "list") node.append(h("span", { class: "fm-meta" }, e.dir ? "" : formatSize(e.size)), h("span", { class: "fm-meta date" }, date));
  return node;
}

function drawContent() {
  const entries = fmEntries();
  const content = $("fm-content");
  content.className = "fm-content " + fm.mode;
  const head = (key, label, extra) =>
    h(
      "button",
      {
        class: "fm-col" + (extra || "") + (fm.sort.key === key ? " on" : ""),
        onclick: (ev) => {
          ev.stopPropagation();
          fm.sort = { key, dir: fm.sort.key === key ? -fm.sort.dir : 1 };
          drawContent();
        },
      },
      label,
      fm.sort.key === key ? (fm.sort.dir > 0 ? " ↑" : " ↓") : "",
    );
  const out = [];
  if (fm.mode === "list") out.push(h("div", { class: "fm-head" }, h("span", {}), head("name", t("byName")), head("size", t("bySize"), " right"), head("modified", t("byDate"), " right date")));
  if (fm.res.denied) out.push(h("p", { class: "muted fm-empty" }, t("accessOff")));
  else if (!entries.length) out.push(h("p", { class: "muted fm-empty" }, t(fm.filter ? "nothingFound" : "emptyFolder")));
  else out.push(h("div", { class: "fm-items" }, ...entries.map(entryNode)));
  if (fm.res.truncated) out.push(h("p", { class: "muted small fm-empty" }, t("truncated")));
  content.replaceChildren(...out);
  drawStatus();
}

function drawStatus() {
  if (!$("fm-status")) return;
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
  if (list.length) {
    if (fm.dev !== SELF) actions.append(iconButton("download", t("download"), () => download(list)));
    actions.append(iconButton("copy", t("copy"), () => toClip(list, false)), iconButton("cut", t("cut"), () => toClip(list, true)));
    if (list.length === 1) actions.append(iconButton("rename", t("rename"), () => rename(list[0])));
    actions.append(
      iconButton("trash", t("delete"), () => removeEntries(list), " danger"),
      iconButton("close", t("deselect"), () => {
        fm.selected.clear();
        syncSelection();
      }),
    );
  } else if (fm.clip && fm.res.path) {
    actions.append(
      h("button", { class: "button primary small", onclick: paste }, h("span", { class: "button-icon", icon: "paste" }), t(fm.clip.cut ? "moveHere" : "pasteHere", fm.clip.paths.length)),
      iconButton("close", t("cancel"), () => {
        fm.clip = null;
        drawStatus();
      }),
    );
  }
  out.push(actions);
  $("fm-status").replaceChildren(...out);
}

function drawAll() {
  if (!$("fm")) return;
  tinted($("fm"), fm.dev === SELF ? state.self.deviceId : fm.dev);
  drawBar();
  drawPlaces();
  drawContent();
  $("fm").classList.toggle("selecting", fm.selecting);
}

// Entry points for the rest of the interface

async function fmShow(dev, path) {
  const root = h(
    "div",
    { id: "fm", class: "fm" },
    h("div", { class: "fm-top" }, iconButton("close", t("close"), () => show(narrow.matches ? { kind: "home" } : { kind: "device", id: fm.dev === SELF ? null : fm.dev }), " fm-leave"), h("div", { id: "fm-bar", class: "fm-bar" })),
    h(
      "div",
      { class: "fm-main" },
      h("div", { id: "fm-places", class: "fm-places" }),
      h("div", {
        id: "fm-content",
        class: "fm-content",
        onclick: () => {
          fm.selected.clear();
          syncSelection();
        },
        oncontextmenu: (e) => {
          e.preventDefault();
          if (fm.res.path) folderMenu(e.clientX, e.clientY);
        },
      }),
    ),
    h("div", { id: "fm-status", class: "fm-status" }),
  );
  fm.dev = null;
  fm.res = null;
  fm.back = [];
  fm.forward = [];
  $("view").replaceChildren(root);
  loadOwnShares();
  // Asked to show a file: open its folder and the file on top.
  let file = null;
  if (path) {
    try {
      const stat = await api.FS(dev, { op: "stat", path });
      if (stat.entry && !stat.entry.dir) {
        file = stat.entry;
        const cut = Math.max(path.lastIndexOf("/"), path.lastIndexOf("\\"));
        path = path.slice(0, cut) || path.slice(0, cut + 1);
      }
    } catch (err) {
      // The listing below reports it.
    }
  }
  if (!(await fmGo(dev, path || "", false)) && view.kind === "files") return show({ kind: narrow.matches ? "home" : "device", id: null });
  if (file) preview(file);
}

// fmState follows the state of the daemon: a finished transfer may have
// changed the folder on screen.
function fmState() {
  if (view.kind !== "files" || !fm.res) return;
  const active = (state.transfers || []).filter((tr) => tr.state === "active").length;
  if (active < fm.active) fmReload();
  else {
    drawPlaces();
    drawStatus();
  }
  fm.active = active;
}

// fmBack is the phone's Back button inside the file manager.
function fmBack() {
  const open = document.querySelector(".fm-preview, .fm-dialog-back, .fm-menu");
  if (open) open.remove();
  else if (fm.selected.size) {
    fm.selected.clear();
    syncSelection();
  } else if (fm.back.length) fmStep(fm.back, fm.forward);
  else return false;
  return true;
}

document.addEventListener("keydown", (e) => {
  if (typeof view === "undefined" || view.kind !== "files" || !fm.res) return;
  if (document.querySelector(".fm-dialog-back, .fm-preview") || /^(INPUT|TEXTAREA|SELECT)$/.test(e.target.tagName)) return;
  const list = picked();
  const ctrl = e.ctrlKey || e.metaKey;
  const entries = fmEntries();
  const move = (by) => {
    const at = entries.findIndex((x) => x.path === fm.anchor);
    const next = entries[Math.max(0, Math.min(entries.length - 1, at < 0 ? 0 : at + by))];
    if (!next) return;
    select(next, e.shiftKey ? e : null);
    const node = [...$("fm-content").querySelectorAll("[data-path]")].find((n) => n.dataset.path === next.path);
    if (node) node.scrollIntoView({ block: "nearest" });
  };
  // The letters are matched by key position, so they work in any layout.
  if (ctrl && e.code === "KeyA") selectAll();
  else if (ctrl && e.code === "KeyC") toClip(list, false);
  else if (ctrl && e.code === "KeyX") toClip(list, true);
  else if (ctrl && e.code === "KeyV") paste();
  else if (ctrl && e.code === "KeyH") {
    fm.hidden = !fm.hidden;
    drawContent();
  } else if (e.key === "Delete") removeEntries(list);
  else if (e.key === "F2" && list.length === 1) rename(list[0]);
  else if (e.key === "F5") fmReload();
  else if (e.key === "Enter" && list.length === 1) openEntry(list[0]);
  else if (e.key === "Backspace" || (e.altKey && e.key === "ArrowUp")) fm.res.parent && fmGo(fm.dev, fm.res.parent);
  else if (e.altKey && e.key === "ArrowLeft") fmStep(fm.back, fm.forward);
  else if (e.altKey && e.key === "ArrowRight") fmStep(fm.forward, fm.back);
  else if (e.key === "ArrowDown" || e.key === "ArrowRight") move(1);
  else if (e.key === "ArrowUp" || e.key === "ArrowLeft") move(-1);
  else if (e.key === "Escape") {
    fm.selected.clear();
    syncSelection();
  } else return;
  e.preventDefault();
});
