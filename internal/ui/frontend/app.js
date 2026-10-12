const api = window.go ? window.go.main.App : httpApi();
const $ = (id) => document.getElementById(id);

const icon = (paths) =>
  `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">${paths}</svg>`;
const icons = {
  phone: icon('<rect x="7" y="2" width="10" height="20" rx="2.5"/><path d="M11 18h2"/>'),
  laptop: icon('<rect x="4" y="5" width="16" height="11" rx="2"/><path d="M2 20h20"/>'),
  desktop: icon('<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>'),
  remote: icon('<path d="M5 3l14 8-6 2-2 6z"/>'),
  file: icon('<path d="M12 16V4M7 9l5-5 5 5M5 20h14"/>'),
  ping: icon('<path d="M6 9a6 6 0 0 1 12 0c0 6 2 7 2 7H4s2-1 2-7M10 20a2 2 0 0 0 4 0"/>'),
  ring: icon('<path d="M4 9v6h4l5 4V5L8 9zM16.5 8.5a5 5 0 0 1 0 7M19 6a8.5 8.5 0 0 1 0 12"/>'),
  cloud: icon('<path d="M7 18a4 4 0 0 1-.6-7.96A6 6 0 0 1 18 9.5a4.25 4.25 0 0 1-.5 8.5z"/>'),
  settings: icon('<circle cx="12" cy="12" r="3"/><path d="M12 2v3M12 19v3M2 12h3M19 12h3M4.9 4.9l2.1 2.1M17 17l2.1 2.1M19.1 4.9L17 7M7 17l-2.1 2.1"/>'),
  refresh: icon('<path d="M20 11a8 8 0 0 0-14.3-4.3L4 9M4 4v5h5M4 13a8 8 0 0 0 14.3 4.3L20 15M20 20v-5h-5"/>'),
  trash: icon('<path d="M4 7h16M9 7V4h6v3M6 7l1 13h10l1-13"/>'),
  folder: icon('<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/>'),
  doc: icon('<path d="M7 3h7l5 5v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM14 3v5h5"/>'),
  download: icon('<path d="M12 4v12M7 11l5 5 5-5M5 20h14"/>'),
  rename: icon('<path d="M4 20h4L19 9l-4-4L4 16zM13.5 6.5l4 4"/>'),
  move: icon('<path d="M5 12h14M13 6l6 6-6 6"/>'),
  up: icon('<path d="M12 19V5M6 11l6-6 6 6"/>'),
  back: icon('<path d="M19 12H5M11 6l-6 6 6 6"/>'),
  palette: icon('<path d="M12 3a9 9 0 1 0 0 18c1.2 0 2-.9 2-2 0-1.5 1-2 2.2-2H18a3 3 0 0 0 3-3c0-6-4-11-9-11z"/><circle cx="8" cy="11" r="1"/><circle cx="12" cy="8" r="1"/><circle cx="16" cy="11" r="1"/>'),
};

// h builds an element. Names and other texts that come from the network only
// ever go in as text, never as markup.
function h(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (key === "class") node.className = value;
    else if (key === "icon") node.innerHTML = icons[value] || icons.desktop;
    else if (key.startsWith("on")) node.addEventListener(key.slice(2), value);
    else if (value !== false && value !== undefined) node.setAttribute(key, value === true ? "" : value);
  }
  node.append(...children.filter((c) => c !== null && c !== undefined && c !== false));
  return node;
}

let state = null;
let snapshot = "";
// On a narrow screen the list of devices and the page of one are two
// screens rather than two columns; "home" is the list.
const narrow = window.matchMedia("(max-width: 720px)");
let view = narrow.matches ? { kind: "home" } : { kind: "device", id: null };
let failures = 0;

// Every device has its own colour: the one the user chose, or one picked
// from the palette by the device id so that it stays the same.
const palette = ["#5aa9ff", "#4cd08a", "#f2c14e", "#ff8f4d", "#ff6b6b", "#f17ac6", "#a78bfa", "#3fd0c9", "#e6e8ea"];
let colors = {};
let pickerOpen = false;

function colorOf(id) {
  if (colors[id]) return colors[id];
  let sum = 0;
  for (const ch of id) sum = (sum * 31 + ch.charCodeAt(0)) >>> 0;
  return palette[sum % (palette.length - 1)];
}

function tinted(node, id) {
  node.style.setProperty("--accent", colorOf(id));
  return node;
}

function toast(text, bad) {
  const node = h("div", { class: "toast" + (bad ? " bad" : "") }, text);
  $("toasts").append(node);
  setTimeout(() => node.classList.add("gone"), bad ? 6000 : 2800);
  setTimeout(() => node.remove(), bad ? 6300 : 3100);
}

// run performs an action, reports its failure and, if given, its success.
async function run(action, done) {
  try {
    const result = await action();
    const message = typeof done === "function" ? done(result) : done;
    if (message) toast(message);
  } catch (err) {
    toast(String(err), true);
  }
  refresh();
}

function formatSize(bytes) {
  const units = language === "ru" ? ["Б", "КБ", "МБ", "ГБ"] : ["B", "KB", "MB", "GB"];
  let i = 0;
  while (bytes >= 1024 && i < units.length - 1) {
    bytes /= 1024;
    i++;
  }
  return (i === 0 ? bytes : bytes.toFixed(1)) + " " + units[i];
}

const formatKey = (key) => key.slice(0, 4) + " " + key.slice(4);

function shortStatus(d) {
  if (d.pairing === "incoming") return t("wantsPair");
  if (d.pairing === "requested") return t("waitingPair");
  if (!d.connected) return t("offline");
  const status = !d.paired ? t("notPaired") : d.relayed ? t("online") + ", " + t("viaServer") : t("online");
  return d.foreign ? status + " · KDE Connect" : status;
}

function longStatus(d) {
  const parts = [shortStatus(d)];
  if (d.connected && !d.relayed && d.addr) parts.push(d.addr);
  if (d.battery) parts.push(d.battery.currentCharge + "%" + (d.battery.isCharging ? ", " + t("charging") : ""));
  return parts.join(" · ");
}

const dotClass = (d) => "dot" + (d.pairing ? " wait" : d.connected && d.paired ? " on" : "");

// Sidebar

function renderSidebar() {
  $("devices-label").textContent = t("devices");
  $("refresh").innerHTML = icons.refresh;
  $("refresh").title = t("refresh");
  $("no-devices").textContent = t("noDevices");
  $("self").textContent = state ? t("thisDevice", state.self.deviceName) : "";

  const relay = state && state.relay;
  $("nav-relay").replaceChildren(h("span", { class: "nav-icon", icon: "cloud" }), h("span", { class: "nav-text" }, t("remoteAccess")));
  if (relay && relay.configured) $("nav-relay").append(h("i", { class: "dot" + (relay.connected ? " on" : " bad") }));
  $("nav-relay").classList.toggle("active", view.kind === "relay");
  $("nav-settings").replaceChildren(h("span", { class: "nav-icon", icon: "settings" }), h("span", { class: "nav-text" }, t("settings")));
  $("nav-settings").classList.toggle("active", view.kind === "settings");

  const devices = state ? state.devices : [];
  $("no-devices").hidden = devices.length > 0;
  $("device-list").replaceChildren(
    ...devices.map((d) =>
      h(
        "button",
        { class: "device" + ((view.kind === "device" || view.kind === "files") && view.id === d.id ? " active" : ""), onclick: () => show({ kind: "device", id: d.id }) },
        tinted(h("span", { class: "device-icon" + (d.paired ? " paired" : ""), icon: d.type }), d.id),
        h("span", { class: "device-text" }, h("span", { class: "device-name" }, d.name), h("span", { class: "device-status" }, h("i", { class: dotClass(d) }), shortStatus(d))),
      ),
    ),
  );
}

// Device view

function tile(iconName, title, sub, action, primary) {
  return h(
    "button",
    { class: "tile" + (primary ? " primary" : ""), onclick: action },
    h("span", { class: "tile-icon", icon: iconName }),
    h("span", { class: "tile-title" }, title),
    h("span", { class: "tile-sub" }, sub),
  );
}

function notice(title, text, ...actions) {
  return h("div", { class: "notice" }, h("h2", {}, title), h("p", { class: "muted" }, text), actions.length ? h("div", { class: "row" }, ...actions) : null);
}

function codeNotice(title, d, ...actions) {
  return h("div", { class: "notice" }, h("h2", {}, title), h("div", { class: "code" }, formatKey(d.key)), h("p", { class: "muted" }, t("codeText")), h("div", { class: "row" }, ...actions));
}

const button = (label, cls, action) => h("button", { class: "button " + cls, onclick: action }, label);

function transferRow(tr) {
  let status = (tr.incoming ? t("from") : t("to")) + " · " + formatSize(tr.size);
  if (tr.state === "failed") status = t("failed", tr.error);
  else if (tr.state === "done" && tr.path) status += " · " + tr.path;
  else if (tr.state === "active") status = Math.round(tr.size ? (100 * tr.done) / tr.size : 0) + "% · " + formatSize(tr.size);
  const fill = h("div", {});
  fill.style.width = (tr.size ? (100 * tr.done) / tr.size : 0) + "%";
  return h(
    "div",
    { class: "transfer " + tr.state },
    h("span", { class: "transfer-arrow" }, tr.incoming ? "↓" : "↑"),
    h("div", { class: "transfer-text" }, h("div", { class: "transfer-name" }, tr.name), h("div", { class: "transfer-status" }, status), tr.state === "active" ? h("div", { class: "bar" }, fill) : null),
  );
}

function renderDevice() {
  const d = state.devices.find((x) => x.id === view.id);
  if (!d) {
    return h("div", { class: "empty" }, h("h1", {}, t("pick")), h("p", { class: "muted" }, t("pickText")));
  }
  const head = h(
    "header",
    {},
    h("span", { class: "head-icon" + (d.paired ? " paired" : ""), icon: d.type }),
    h("div", { class: "head-text" }, h("h1", {}, d.name), h("p", { class: "muted" }, h("i", { class: dotClass(d) }), longStatus(d))),
    h("button", {
      class: "icon-button",
      title: t("color"),
      icon: "palette",
      onclick: () => {
        pickerOpen = !pickerOpen;
        render();
      },
    }),
    d.paired ? h("button", { class: "icon-button", title: t("forget"), icon: "trash", onclick: () => run(() => api.Unpair(d.id), t("forgotten")) }) : null,
  );

  const body = h("div", { class: "body" });
  if (pickerOpen) {
    body.append(
      h(
        "div",
        { class: "swatches" },
        ...palette.map((color) => {
          const swatch = h("button", {
            class: "swatch" + (colorOf(d.id) === color ? " active" : ""),
            onclick: () =>
              run(async () => {
                await api.SetDeviceColor(d.id, color);
                colors[d.id] = color;
                pickerOpen = false;
                render();
              }),
          });
          swatch.style.background = color;
          return swatch;
        }),
      ),
    );
  }
  if (d.pairing === "incoming") {
    body.append(codeNotice(t("incomingTitle"), d, button(t("accept"), "primary", () => run(() => api.Pair(d.id))), button(t("reject"), "", () => run(() => api.Unpair(d.id)))));
  } else if (d.pairing === "requested") {
    body.append(codeNotice(t("requestedTitle"), d, button(t("cancel"), "", () => run(() => api.Unpair(d.id)))));
  } else if (!d.paired) {
    body.append(notice(t("pairTitle"), t("pairText"), button(t("pair"), "primary", () => run(() => api.Pair(d.id), t("pairSent")))));
  } else if (!d.connected) {
    body.append(h("p", { class: "muted" }, t("offlineText")));
  } else {
    const tiles = h("div", { class: "tiles" });
    if (d.canRemote && !api.web) tiles.append(tile("remote", t("actRemote"), t("actRemoteSub"), () => run(() => api.Remote(d.id)), true));
    tiles.append(
      tile("file", t("actFile"), t("actFileSub"), () =>
        run(
          () => api.SendFiles(d.id, t("chooseFiles")),
          (count) => count > 0 && t("filesSent", count),
        ),
      ),
      tile("ping", t("actPing"), t("actPingSub"), () => run(() => api.Ping(d.id), t("pingSent"))),
    );
    if (d.canBrowse) tiles.append(tile("folder", t("actBrowse"), t("actBrowseSub"), () => show({ kind: "files", id: d.id, path: "" })));
    if (d.canRing) tiles.append(tile("ring", t("actRing"), t("actRingSub"), () => run(() => api.Ring(d.id), t("ringSent"))));
    body.append(tiles);

    const commands = d.commands || [];
    body.append(
      h("h3", {}, t("commands")),
      commands.length
        ? h("div", { class: "chips" }, ...commands.map((c) => h("button", { class: "chip", title: c.command, onclick: () => run(() => api.Run(d.id, c.key), t("commandSent", c.name)) }, c.name)))
        : h("p", { class: "muted small" }, t("noCommands")),
    );
  }

  const transfers = (state.transfers || []).filter((tr) => tr.deviceId === d.id);
  if (transfers.length) body.append(h("h3", {}, t("files")), h("div", { class: "transfers" }, ...transfers.map(transferRow)));
  return tinted(h("div", { class: "page" }, head, body), d.id);
}

// File browser: the files of another device. It is not redrawn by the
// periodic refresh, only when a folder is opened or changed.

const files = { result: null, showHidden: false, renaming: null, moving: null };

async function openFolder(path) {
  try {
    files.result = await api.FS(view.id, "list", path, "");
    view.path = files.result.path;
    files.renaming = null;
  } catch (err) {
    toast(String(err), true);
    if (!files.result) return show({ kind: "device", id: view.id });
  }
  if (view.kind === "files") $("view").replaceChildren(renderFiles());
}

// fileAction changes something on the device and shows the folder again.
async function fileAction(op, path, to, done) {
  try {
    await api.FS(view.id, op, path, to);
    if (done) toast(done);
  } catch (err) {
    toast(String(err), true);
  }
  openFolder(view.path);
}

// A phone has no trash can, so deleting there cannot be undone and takes a
// second press to confirm.
const noTrash = () => (state.devices.find((d) => d.id === view.id) || {}).type === "phone";
let armed = null;

function removeFile(e) {
  if (!noTrash()) return fileAction("delete", e.path, "", t("trashed"));
  if (armed !== e.path) {
    armed = e.path;
    setTimeout(() => (armed = null), 4000);
    return toast(t("confirmDelete", e.name), true);
  }
  armed = null;
  fileAction("delete", e.path, "", t("deleted"));
}

const iconButton = (name, title, action) => h("button", { class: "icon-button", title, icon: name, onclick: action });

function fileRow(e) {
  const open = () => e.dir && openFolder(e.path);
  let name = h("span", { class: "file-name" + (e.dir ? " dir" : ""), onclick: open }, e.name);
  if (files.renaming === e.path) {
    name = h("input", {
      class: "file-rename",
      value: e.name,
      spellcheck: "false",
      onkeydown: (ev) => {
        if (ev.key === "Enter" && ev.target.value.trim() && ev.target.value !== e.name) fileAction("rename", e.path, ev.target.value.trim(), t("renamed"));
        if (ev.key === "Enter" || ev.key === "Escape") {
          files.renaming = null;
          if (ev.key === "Escape" || ev.target.value === e.name) $("view").replaceChildren(renderFiles());
          ev.stopPropagation();
        }
      },
    });
    setTimeout(() => name.select(), 0);
  }
  return h(
    "div",
    { class: "file-row" },
    h("span", { class: "file-icon" + (e.dir ? " dir" : ""), icon: e.dir ? "folder" : "doc", onclick: open }),
    name,
    h("span", { class: "file-meta" }, e.dir ? "" : formatSize(e.size)),
    h("span", { class: "file-meta" }, new Date(e.modified * 1000).toLocaleDateString(language, { day: "2-digit", month: "short", year: "numeric" })),
    h(
      "span",
      { class: "file-actions" },
      e.dir ? null : iconButton("download", t("download"), () => fileAction("download", e.path, "", t("downloading"))),
      iconButton("rename", t("rename"), () => {
        files.renaming = e.path;
        $("view").replaceChildren(renderFiles());
      }),
      iconButton("move", t("move"), () => {
        files.moving = { path: e.path, name: e.name };
        toast(t("moveHint"));
        $("view").replaceChildren(renderFiles());
      }),
      iconButton("trash", t(noTrash() ? "deleteForever" : "toTrash"), () => removeFile(e)),
    ),
  );
}

function renderFiles() {
  const d = state.devices.find((x) => x.id === view.id);
  const res = files.result;
  const entries = res.entries.filter((e) => files.showHidden || !e.name.startsWith("."));

  const toolbar = h(
    "div",
    { class: "row toolbar" },
    h("button", { class: "button", disabled: !res.parent, onclick: () => openFolder(res.parent) }, h("span", { class: "button-icon", icon: "up" }), t("up")),
    h("button", { class: "button", onclick: () => openFolder(view.path) }, h("span", { class: "button-icon", icon: "refresh" }), t("reload")),
    h(
      "button",
      {
        class: "button" + (files.showHidden ? " on" : ""),
        onclick: () => {
          files.showHidden = !files.showHidden;
          $("view").replaceChildren(renderFiles());
        },
      },
      t("hidden"),
    ),
  );
  if (files.moving) {
    const moving = files.moving;
    toolbar.append(
      h(
        "button",
        {
          class: "button primary",
          onclick: () => {
            files.moving = null;
            fileAction("move", moving.path, view.path, t("moved"));
          },
        },
        t("pasteHere", moving.name),
      ),
      h(
        "button",
        {
          class: "button",
          onclick: () => {
            files.moving = null;
            $("view").replaceChildren(renderFiles());
          },
        },
        t("cancel"),
      ),
    );
  }

  return tinted(
    h(
      "div",
      { class: "page wide" },
      h(
        "header",
        {},
        iconButton("back", t("backToDevice"), () => show({ kind: "device", id: view.id })),
        h("div", { class: "head-text" }, h("h1", {}, d ? d.name : ""), h("p", { class: "muted path" }, res.path)),
      ),
      h(
        "div",
        { class: "body" },
        toolbar,
        entries.length ? h("div", { class: "files" }, ...entries.map(fileRow)) : h("p", { class: "muted folder-empty" }, t("emptyFolder")),
        res.truncated ? h("p", { class: "muted small" }, t("truncated")) : null,
      ),
    ),
    view.id,
  );
}

// Remote access view. Its forms are built once, so typing is not disturbed
// by the periodic refresh; only the status part follows the state.

const field = (id, placeholder, extra) => h("input", { id, placeholder, autocomplete: "off", spellcheck: "false", ...(extra || {}) });

function renderRelay() {
  const setup = h(
    "form",
    {
      class: "card",
      onsubmit: async (e) => {
        e.preventDefault();
        const b = $("setup-button");
        b.disabled = true;
        b.textContent = t("settingUp");
        await run(() => api.RelaySetup($("relay-address").value, $("relay-user").value, $("relay-password").value), t("joined"));
        if ($("setup-button")) {
          $("relay-password").value = "";
          b.disabled = false;
          b.textContent = t("setup");
        }
      },
    },
    h("h2", {}, t("setupTitle")),
    h("p", { class: "muted small" }, t("setupText")),
    field("relay-address", t("address"), { required: true }),
    h("div", { class: "row" }, field("relay-user", t("login"), { value: "root" }), field("relay-password", t("password"), { type: "password" })),
    h("button", { class: "button primary", id: "setup-button" }, t("setup")),
  );
  const join = h(
    "form",
    {
      class: "card",
      onsubmit: (e) => {
        e.preventDefault();
        run(() => api.RelayJoin($("relay-code").value.trim()), t("joined"));
      },
    },
    h("h2", {}, t("joinTitle")),
    h("p", { class: "muted small" }, t("joinText")),
    h("div", { class: "row" }, field("relay-code", t("code"), { required: true }), h("button", { class: "button" }, t("join"))),
  );
  return h(
    "div",
    { class: "page" },
    h("header", {}, h("span", { class: "head-icon", icon: "cloud" }), h("div", { class: "head-text" }, h("h1", {}, t("remoteAccess")), h("p", { class: "muted" }, t("relayIntro")))),
    h("div", { class: "body" }, h("div", { id: "relay-status" }), h("div", { id: "relay-forms" }, setup, join)),
  );
}

function updateRelay() {
  if (!$("relay-status")) return;
  const relay = state.relay;
  $("relay-forms").hidden = relay.configured;
  if (!relay.configured) {
    $("relay-status").replaceChildren();
    return;
  }
  $("relay-status").replaceChildren(
    h(
      "div",
      { class: "card" },
      h("h2", {}, t("relayServer", relay.host)),
      h("p", { class: relay.connected ? "good" : "bad" }, h("i", { class: "dot" + (relay.connected ? " on" : " bad") }), relay.connected ? t("relayOn") : t("relayDown")),
      h("p", { class: "muted small" }, t("relayAdd")),
      h("div", { class: "code-line" }, relay.code),
      h("div", { class: "row" }, button(t("copyCode"), "primary", () => run(() => api.Copy(relay.code), t("copied"))), button(t("leave"), "", () => run(() => api.RelayLeave(), t("left")))),
    ),
  );
}

// Settings view, also built once per visit.

function commandRow(name, command) {
  const row = h(
    "div",
    { class: "row command-row" },
    h("input", { class: "cmd-name", placeholder: t("cmdName"), value: name, spellcheck: "false" }),
    h("input", { class: "cmd-line", placeholder: t("cmdLine"), value: command, spellcheck: "false" }),
    h("button", { class: "icon-button", type: "button", title: t("remove"), icon: "trash", onclick: () => row.remove() }),
  );
  return row;
}

async function renderSettings() {
  const [settings, commands] = await Promise.all([api.Settings(), api.Commands()]);

  const langButton = (code, label) =>
    h(
      "button",
      {
        type: "button",
        class: "segment" + (settings.language === code ? " active" : ""),
        onclick: () =>
          run(async () => {
            settings.language = code;
            await api.SaveSettings(settings);
            language = code;
            document.documentElement.lang = code;
            show(view);
          }),
      },
      label,
    );

  const general = h(
    "form",
    {
      class: "card",
      onsubmit: (e) => {
        e.preventDefault();
        run(async () => {
          settings.name = $("set-name").value;
          settings.downloadDir = $("set-folder").value;
          settings.clipboard = $("set-clipboard").checked;
          settings.kdeConnect = $("set-kde").checked;
          settings.fileAccess = $("set-files").checked;
          await api.SaveSettings(settings);
        }, t("saved"));
      },
    },
    h("h2", {}, t("general")),
    h("label", {}, t("name"), h("input", { id: "set-name", value: settings.name, spellcheck: "false", required: true }), h("span", { class: "muted small" }, t("nameHint"))),
    h(
      "label",
      {},
      t("folder"),
      h(
        "div",
        { class: "row" },
        h("input", { id: "set-folder", value: settings.downloadDir, spellcheck: "false", required: true }),
        !api.web &&
          h(
          "button",
          {
            type: "button",
            class: "button",
            onclick: () =>
              run(async () => {
                const dir = await api.ChooseFolder($("set-folder").value, t("folder"));
                if (dir) $("set-folder").value = dir;
              }),
          },
          t("choose"),
        ),
      ),
    ),
    h(
      "label",
      { class: "switch-line" },
      h("span", {}, t("clipboard"), h("span", { class: "muted small block" }, t("clipboardHint"))),
      h("input", { id: "set-clipboard", type: "checkbox", class: "switch", checked: settings.clipboard }),
    ),
    h(
      "label",
      { class: "switch-line" },
      h("span", {}, t("kde"), h("span", { class: "muted small block" }, t("kdeHint"))),
      h("input", { id: "set-kde", type: "checkbox", class: "switch", checked: settings.kdeConnect }),
    ),
    h(
      "label",
      { class: "switch-line" },
      h("span", {}, t("fileAccess"), h("span", { class: "muted small block" }, t("fileAccessHint"))),
      h("input", { id: "set-files", type: "checkbox", class: "switch", checked: settings.fileAccess }),
    ),
    h("div", { class: "row between" }, h("div", { class: "segments" }, langButton("ru", "Русский"), langButton("en", "English")), h("button", { class: "button primary" }, t("save"))),
  );

  const rows = h("div", { id: "command-rows" }, ...Object.entries(commands).sort().map(([name, command]) => commandRow(name, command)));
  const commandsCard = h(
    "form",
    {
      class: "card",
      onsubmit: (e) => {
        e.preventDefault();
        run(async () => {
          const out = {};
          for (const row of rows.children) {
            const name = row.querySelector(".cmd-name").value.trim();
            const line = row.querySelector(".cmd-line").value.trim();
            if (name || line) out[name] = line;
          }
          await api.SaveCommands(out);
        }, t("saved"));
      },
    },
    h("h2", {}, t("myCommands")),
    h("p", { class: "muted small" }, t("myCommandsHint")),
    rows,
    h("div", { class: "row between" }, h("button", { type: "button", class: "button", onclick: () => rows.append(commandRow("", "")) }, t("addCommand")), h("button", { class: "button primary" }, t("save"))),
  );

  const quit = h("div", { class: "card row between" }, h("p", { class: "muted small" }, t("quitHint")), button(t("quit"), "", () => api.Quit()));

  return h(
    "div",
    { class: "page" },
    h("header", {}, h("span", { class: "head-icon", icon: "settings" }), h("div", { class: "head-text" }, h("h1", {}, t("settings")))),
    h("div", { class: "body" }, general, commandsCard, quit),
  );
}

// Navigation and refresh

const backHome = () => h("button", { class: "back-home", onclick: () => show({ kind: "home" }) }, h("span", { class: "button-icon", icon: "back" }), t("devices"));

async function show(next) {
  view = next;
  document.body.classList.toggle("detail", view.kind !== "home");
  renderSidebar();
  if (view.kind === "home") return $("view").replaceChildren();
  if (view.kind === "relay") {
    $("view").replaceChildren(renderRelay());
    updateRelay();
  } else if (view.kind === "files") {
    files.result = null;
    files.moving = null;
    await openFolder(view.path);
  } else if (view.kind === "settings") {
    try {
      $("view").replaceChildren(await renderSettings());
    } catch (err) {
      toast(String(err), true);
    }
  } else {
    $("view").replaceChildren(renderDevice());
  }
  if (view.kind !== "files") $("view").prepend(backHome());
}

function render() {
  // Keep a device selected: the chosen one, else the most useful one.
  if (view.kind === "device" && !state.devices.some((d) => d.id === view.id) && !narrow.matches) {
    const best = state.devices.find((d) => d.connected && d.paired) || state.devices.find((d) => d.connected) || state.devices[0];
    view.id = best ? best.id : null;
  }
  // The browser closes when its device goes away.
  if (view.kind === "files" && !state.devices.some((d) => d.id === view.id && d.connected)) {
    view = { kind: "device", id: view.id };
  }
  renderSidebar();
  if (view.kind === "device") $("view").replaceChildren(backHome(), renderDevice());
  if (view.kind === "relay") updateRelay();

  const controlled = state.devices.find((d) => d.remote);
  $("remote").hidden = !controlled;
  if (controlled) tinted($("remote"), controlled.id);
  if (controlled) {
    $("remote-title").textContent = t("remoteTitle");
    $("remote-text").textContent = t("remoteText", controlled.name);
    $("remote-hint").textContent = t("remoteHint");
  }
}

async function refresh() {
  try {
    const next = await api.State();
    failures = 0;
    const text = JSON.stringify(next);
    if (text !== snapshot) {
      snapshot = text;
      state = next;
      render();
    }
  } catch (err) {
    // The daemon restarts after some settings change; only a lasting loss is worth a message.
    if (++failures === 6) toast(String(err), true);
  }
}

// The phone's Back button goes up one level here before it leaves the app.
window.perekatBack = () => {
  if (view.kind === "files") show({ kind: "device", id: view.id });
  else if (view.kind !== "home" && narrow.matches) show({ kind: "home" });
  else return false;
  return true;
};

// While the mouse is captured the keyboard is the only way back.
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && state && state.devices.some((d) => d.remote)) run(() => api.RemoteOff());
});
$("refresh").addEventListener("click", () => run(() => api.Refresh()));
$("nav-relay").addEventListener("click", () => show({ kind: "relay" }));
$("nav-settings").addEventListener("click", () => show({ kind: "settings" }));

(async () => {
  try {
    const settings = await api.Settings();
    language = settings.language;
    colors = settings.colors || {};
  } catch (err) {
    // The default language stays.
  }
  document.documentElement.lang = language;
  await refresh();
  const first = await api.InitialView();
  if (first === "relay" || first === "settings") show({ kind: first });
  const usable = first === "device" && state && state.devices.find((d) => d.paired && d.connected);
  if (usable) show({ kind: "device", id: usable.id });
  const browsable = first === "files" && state && state.devices.find((d) => d.canBrowse && d.connected);
  if (browsable) show({ kind: "files", id: browsable.id, path: "" });
  setInterval(refresh, 500);
})();
