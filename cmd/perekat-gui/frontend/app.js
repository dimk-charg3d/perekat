const api = window.go.main.App;

const icons = {
  phone: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="7" y="2" width="10" height="20" rx="2"/><path d="M11 18h2"/></svg>',
  laptop: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="4" y="5" width="16" height="11" rx="2"/><path d="M2 20h20"/></svg>',
  desktop: '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/></svg>',
};

const $ = (id) => document.getElementById(id);
let lastState = "";
let remoteActive = false;

// Device names come from the network, so they are only ever set as text.
function el(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function button(label, className, action) {
  const b = el("button", className, label);
  b.addEventListener("click", () => run(action));
  return b;
}

async function run(action) {
  try {
    await action();
    $("error").hidden = true;
  } catch (err) {
    showError(err);
  }
  refresh();
}

function showError(err) {
  $("error").textContent = String(err);
  $("error").hidden = false;
}

function formatKey(key) {
  return key.slice(0, 4) + " " + key.slice(4);
}

function formatSize(bytes) {
  const units = ["Б", "КБ", "МБ", "ГБ"];
  let i = 0;
  while (bytes >= 1024 && i < units.length - 1) {
    bytes /= 1024;
    i++;
  }
  return (i === 0 ? bytes : bytes.toFixed(1)) + " " + units[i];
}

function statusOf(d) {
  if (d.pairing === "incoming") return ["Хочет сопряжение · код ", formatKey(d.key)];
  if (d.pairing === "requested") return ["Ждём подтверждения · код ", formatKey(d.key)];
  if (d.paired && d.connected) {
    const battery = d.battery ? " · " + d.battery.currentCharge + "%" + (d.battery.isCharging ? " ⚡" : "") : "";
    return ["В сети · " + d.addr + battery];
  }
  if (d.paired) return ["Не в сети"];
  return ["Не сопряжено · " + d.addr];
}

function actionsOf(d) {
  if (d.pairing === "incoming") {
    return [
      button("Принять", "primary", () => api.Pair(d.id)),
      button("Отклонить", "", () => api.Unpair(d.id)),
    ];
  }
  if (d.pairing === "requested") return [button("Отменить", "", () => api.Unpair(d.id))];
  if (!d.paired) return [button("Сопрячь", "primary", () => api.Pair(d.id))];
  const out = [];
  if (d.canRemote) out.push(button("Пульт", "primary", () => api.Remote(d.id)));
  out.push(button("Забыть", "ghost", () => api.Unpair(d.id)));
  return out;
}

// chipsOf lists what can be done with a paired device that is online.
function chipsOf(d) {
  if (!d.paired || !d.connected) return [];
  const out = [
    button("Отправить файл", "", () => api.SendFiles(d.id)),
    button("Пинг", "", () => api.Ping(d.id)),
  ];
  if (d.canRing) out.push(button("Позвонить", "", () => api.Ring(d.id)));
  for (const c of d.commands || []) {
    const b = button(c.name, "command", () => api.Run(d.id, c.key));
    b.title = c.command;
    out.push(b);
  }
  return out;
}

function renderTransfers(transfers) {
  const list = $("transfers");
  list.replaceChildren();
  for (const t of transfers) {
    const row = el("div", "transfer " + t.state);
    row.append(el("div", "name", (t.incoming ? "↓ " : "↑ ") + t.name));
    let status = (t.incoming ? "от " : "для ") + t.device + " · " + formatSize(t.size);
    if (t.state === "failed") status = "Не удалось: " + t.error;
    if (t.state === "done" && t.path) status += " · " + t.path;
    row.append(el("div", "status", status));
    if (t.state === "active") {
      const bar = el("div", "bar");
      const fill = el("div");
      fill.style.width = (t.size ? (100 * t.done) / t.size : 0) + "%";
      bar.append(fill);
      row.append(bar);
    }
    list.append(row);
  }
  $("transfers-section").hidden = transfers.length === 0;
}

function render(state) {
  $("self").textContent = "Это устройство: " + state.self.deviceName;

  const list = $("devices");
  list.replaceChildren();
  for (const d of state.devices) {
    const card = el("div", "device" + (d.paired ? " paired" : "") + (d.connected ? "" : " offline"));
    const row = el("div", "row");

    const icon = el("div", "icon");
    icon.innerHTML = icons[d.type] || icons.desktop;

    const info = el("div", "info");
    info.append(el("div", "name", d.name));
    const [text, key] = statusOf(d);
    const status = el("div", "status");
    status.append(el("span", "dot"), text);
    if (key) status.append(el("span", "key", key));
    info.append(status);

    const actions = el("div", "actions");
    actions.append(...actionsOf(d));

    row.append(icon, info, actions);
    card.append(row);
    const chips = chipsOf(d);
    if (chips.length) {
      const strip = el("div", "chips");
      strip.append(...chips);
      card.append(strip);
    }
    list.append(card);
  }
  $("empty").hidden = state.devices.length > 0;
  renderTransfers(state.transfers || []);

  const controlled = state.devices.find((d) => d.remote);
  remoteActive = Boolean(controlled);
  $("remote").hidden = !remoteActive;
  if (controlled) $("remote-name").textContent = controlled.name;
}

async function refresh() {
  try {
    const state = await api.State();
    const snapshot = JSON.stringify(state);
    if (snapshot !== lastState) {
      lastState = snapshot;
      render(state);
    }
  } catch (err) {
    showError(err);
  }
}

// While the mouse is captured the keyboard is the only way back.
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && remoteActive) run(() => api.RemoteOff());
});

$("quit").addEventListener("click", () => api.Quit());
$("refresh").addEventListener("click", () => run(() => api.Refresh()));

refresh();
setInterval(refresh, 500);
