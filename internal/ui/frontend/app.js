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
  forward: icon('<path d="M5 12h14M13 6l6 6-6 6"/>'),
  grid: icon('<rect x="4" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="13.5" y="4" width="6.5" height="6.5" rx="1.5"/><rect x="4" y="13.5" width="6.5" height="6.5" rx="1.5"/><rect x="13.5" y="13.5" width="6.5" height="6.5" rx="1.5"/>'),
  list: icon('<path d="M8 6h12M8 12h12M8 18h12M4 6h.01M4 12h.01M4 18h.01"/>'),
  sort: icon('<path d="M4 7h16M7 12h10M10 17h4"/>'),
  plus: icon('<path d="M12 5v14M5 12h14"/>'),
  folderPlus: icon('<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM12 11v5M9.5 13.5h5"/>'),
  upload: icon('<path d="M12 16V4M7 9l5-5 5 5M5 20h14"/>'),
  copy: icon('<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/>'),
  cut: icon('<circle cx="6" cy="18" r="2.5"/><circle cx="18" cy="18" r="2.5"/><path d="M7.5 16L18 4M16.5 16L6 4"/>'),
  paste: icon('<path d="M9 4h6v3H9zM9 5H6a1 1 0 0 0-1 1v13a1 1 0 0 0 1 1h12a1 1 0 0 0 1-1V6a1 1 0 0 0-1-1h-3"/>'),
  home: icon('<path d="M4 11l8-7 8 7v8a1 1 0 0 1-1 1h-4v-6h-6v6H5a1 1 0 0 1-1-1z"/>'),
  image: icon('<rect x="3" y="5" width="18" height="14" rx="2"/><circle cx="8.5" cy="10" r="1.5"/><path d="M21 16l-5-5-8 8"/>'),
  video: icon('<rect x="3" y="5" width="18" height="14" rx="2"/><path d="M10 9.5v5l4.5-2.500z"/>'),
  audio: icon('<path d="M9 18V6l10-2v12"/><circle cx="6.500" cy="18" r="2.500"/><circle cx="16.500" cy="16" r="2.500"/>'),
  share: icon('<path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v3M3 7v10a2 2 0 0 0 2 2h6"/><circle cx="18" cy="17" r="3.200"/><path d="M14.800 17h6.400M18 13.800c1.200 1 1.200 5.400 0 6.400M18 13.800c-1.200 1-1.200 5.400 0 6.400"/>'),
  disk: icon('<rect x="3" y="13" width="18" height="7" rx="2"/><path d="M5.500 13l2-8h9l2 8M7 16.500h.01"/>'),
  close: icon('<path d="M6 6l12 12M18 6L6 18"/>'),
  check: icon('<path d="M5 12.500l4.500 4.500L19 7.500"/>'),
  open: icon('<path d="M14 4h6v6M20 4l-9 9M18 14v4a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h4"/>'),
  pdf: icon('<path d="M7 3h7l5 5v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM14 3v5h5M8.500 16.500c2-1 3.500-4 3.500-6 0 3 2 5 4 5.500-2.500 0-5 .500-7.500 .500z"/>'),
  office: icon('<path d="M7 3h7l5 5v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM14 3v5h5M9 12h6M9 15h6M9 18h4"/>'),
  archive: icon('<path d="M5 4h14a1 1 0 0 1 1 1v14a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V5a1 1 0 0 1 1-1zM12 4v2M12 8v2M12 12v2M10.500 16h3v2h-3z"/>'),
  code: icon('<path d="M7 3h7l5 5v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2zM14 3v5h5M10.500 12.500L8.500 15l2 2.500M13.500 12.500l2 2.500-2 2.500"/>'),
  search: icon('<circle cx="11" cy="11" r="6.500"/><path d="M16 16l4.500 4.500"/>'),
  star: icon('<path d="M12 3.500l2.600 5.400 5.900 .800-4.300 4.100 1 5.900-5.200-2.800-5.200 2.800 1-5.900-4.300-4.100 5.900-.800z"/>'),
  info: icon('<circle cx="12" cy="12" r="9"/><path d="M12 11v5.500M12 7.500v.010"/>'),
  split: icon('<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16"/>'),
  play: icon('<path d="M6 4.500v15l8-7.500zM16 5v14M20 5v14"/>'),
  next: icon('<path d="M5 5v14l10-7zM19 5v14"/>'),
  prev: icon('<path d="M19 5v14L9 12zM5 5v14"/>'),
  volup: icon('<path d="M4 9v6h4l5 4V5L8 9zM17 9v6M14 12h6"/>'),
  voldown: icon('<path d="M4 9v6h4l5 4V5L8 9zM15 12h5"/>'),
  mute: icon('<path d="M4 9v6h4l5 4V5L8 9zM16 9.500l5 5M21 9.500l-5 5"/>'),
  blank: icon(""),
  palette: icon('<path d="M12 3a9 9 0 1 0 0 18c1.2 0 2-.9 2-2 0-1.5 1-2 2.2-2H18a3 3 0 0 0 3-3c0-6-4-11-9-11z"/><circle cx="8" cy="11" r="1"/><circle cx="12" cy="8" r="1"/><circle cx="16" cy="11" r="1"/>'),
};

// h builds an element. Names and other texts that come from the network only
// ever go in as text, never as markup.
function h(tag, props, ...children) {
  const node = document.createElement(tag);
  for (const [key, value] of Object.entries(props || {})) {
    if (key === "class") node.className = value;
    else if (key === "icon") node.innerHTML = icons[value] || icons.desktop;
    else if (value === null) continue;
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
// build is the version of perekat, shown under the name of this device.
let build = "";

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
  $("self").textContent = state ? t("thisDevice", state.self.deviceName) + (build ? " · " + build : "") : "";

  const relay = state && state.relay;
  $("nav-relay").replaceChildren(h("span", { class: "nav-icon", icon: "cloud" }), h("span", { class: "nav-text" }, t("remoteAccess")));
  if (relay && relay.configured) $("nav-relay").append(h("i", { class: "dot" + (relay.connected ? " on" : " bad") }));
  $("nav-relay").classList.toggle("active", view.kind === "relay");
  $("nav-files").replaceChildren(h("span", { class: "nav-icon", icon: "folder" }), h("span", { class: "nav-text" }, t("actBrowse")));
  $("nav-files").classList.toggle("active", view.kind === "files");
  $("nav-settings").replaceChildren(h("span", { class: "nav-icon", icon: "settings" }), h("span", { class: "nav-text" }, t("settings")));
  $("nav-settings").classList.toggle("active", view.kind === "settings");

  const devices = state ? state.devices : [];
  $("no-devices").hidden = devices.length > 0;
  $("device-list").replaceChildren(
    ...devices.map((d) =>
      h(
        "button",
        { class: "device" + (view.kind === "device" && view.id === d.id ? " active" : ""), onclick: () => show({ kind: "device", id: d.id }) },
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
    if (d.canRemote && api.web) tiles.append(tile("remote", t("actPad"), t("actPadSub"), () => show({ kind: "pad", id: d.id }), true));
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
    h("button", { class: "icon-button", type: "button", title: t("remove"), icon: "trash", onclick: () => {
        // Removing a command that was saved takes effect at once, without Save.
        const form = row.closest("form");
        row.remove();
        if (name && form) form.requestSubmit();
      },
    }),
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
          if ($("set-autostart")) settings.autostart = $("set-autostart").checked;
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
    settings.canAutostart
      ? h(
          "label",
          { class: "switch-line" },
          h("span", {}, t("autostart"), h("span", { class: "muted small block" }, t("autostartHint"))),
          h("input", { id: "set-autostart", type: "checkbox", class: "switch", checked: settings.autostart }),
        )
      : null,
    h("div", { class: "row between" }, h("div", { class: "segments" }, langButton("ru", "Русский"), langButton("en", "English")), h("button", { class: "button primary" }, t("save"))),
  );

  const phoneCard =
    window.Android && window.Android.notificationAccess
      ? h(
          "div",
          { class: "card" },
          h("h2", {}, t("notifyTitle")),
          h("p", { class: "muted small" }, t("notifyHint")),
          h("div", { class: "row" }, h("button", { type: "button", class: "button", onclick: () => window.Android.notificationAccess() }, t("notifyOpen"))),
        )
      : null;

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
    h("div", { class: "body" }, general, phoneCard, commandsCard, quit),
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
    await fmShow(view.id, view.path);
  } else if (view.kind === "pad") {
    const d = state.devices.find((x) => x.id === view.id);
    if (!d || !d.connected) return show({ kind: "device", id: view.id });
    $("view").replaceChildren(renderPad(d));
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
  if (view.kind === "files" && view.id !== SELF && !state.devices.some((d) => d.id === view.id && d.connected)) {
    show({ kind: "files", id: SELF, path: "" });
  }
  fmState();
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
  if (view.kind === "pad") show({ kind: "device", id: view.id });
  else if (view.kind === "files") {
    if (!fmBack()) show(narrow.matches ? { kind: "home" } : { kind: "device", id: view.id === SELF ? null : view.id });
  } else if (view.kind !== "home" && narrow.matches) show({ kind: "home" });
  else return false;
  return true;
};

// While the mouse is captured the keyboard is the only way back.
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && state && state.devices.some((d) => d.remote)) run(() => api.RemoteOff());
});
$("refresh").addEventListener("click", () => run(() => api.Refresh()));
$("nav-files").addEventListener("click", () => show({ kind: "files", id: SELF, path: "" }));
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
  api
    .Version()
    .then((v) => (build = v === "dev" ? "" : v))
    .catch(() => {});
  document.documentElement.lang = language;
  await refresh();
  const first = await api.InitialView();
  if (first === "relay" || first === "settings") show({ kind: first });
  const usable = first === "device" && state && state.devices.find((d) => d.paired && d.connected);
  if (usable) show({ kind: "device", id: usable.id });
  if (first === "files") show({ kind: "files", id: SELF, path: (await api.InitialPath()) || "" });
  setInterval(refresh, 500);
})();
