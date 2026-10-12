// The interface runs in two places. In the desktop window its calls are
// bound straight to the application. Anywhere else, such as the phone app, it
// talks to the daemon's local web API; the token that admits it comes in the
// address, after the #.
function httpApi() {
  const token = location.hash.slice(1);

  async function call(method, path, body) {
    const form = body instanceof FormData;
    const res = await fetch("/api" + path, {
      method,
      headers: { "X-Perekat-Token": token },
      body: body === undefined || form ? body : JSON.stringify(body),
    });
    const text = (await res.text()).trim();
    if (!res.ok) throw text || res.statusText;
    return text ? JSON.parse(text) : null;
  }
  const of = (id) => "?device=" + encodeURIComponent(id);

  function pickFiles() {
    return new Promise((resolve) => {
      const input = document.createElement("input");
      input.type = "file";
      input.multiple = true;
      input.addEventListener("change", () => resolve([...input.files]));
      input.addEventListener("cancel", () => resolve([]));
      input.click();
    });
  }

  return {
    // web marks the calls that have no window behind them.
    web: true,
    async State() {
      const [self, devices, transfers, relay] = await Promise.all([call("GET", "/self"), call("GET", "/devices"), call("GET", "/transfers"), call("GET", "/relay")]);
      return { self, devices, transfers, relay };
    },
    Pair: (id) => call("POST", "/pair" + of(id)),
    Unpair: (id) => call("POST", "/unpair" + of(id)),
    Remote: (id) => call("POST", "/remote" + of(id)),
    RemoteOff: () => call("POST", "/remote/off"),
    Ping: (id) => call("POST", "/ping" + of(id)),
    Ring: (id) => call("POST", "/ring" + of(id)),
    Run: (id, command) => call("POST", "/run" + of(id) + "&command=" + encodeURIComponent(command)),
    Refresh: () => call("POST", "/refresh"),
    async SendFiles(id) {
      const picked = await pickFiles();
      if (!picked.length) return 0;
      const form = new FormData();
      for (const file of picked) form.append("file", file, file.name);
      await call("POST", "/upload" + of(id), form);
      return picked.length;
    },
    FS: (id, request) => call("POST", "/fs" + of(id), request),
    FSPaste: (src, paths, dst, dir, move) => call("POST", "/fs/paste", { src, paths, dst, dir, move }),
    FSPreview: (id, path) => call("POST", "/fs/preview" + of(id) + "&path=" + encodeURIComponent(path)),
    InitialPath: async () => new URLSearchParams(location.search).get("open") || "",
    FSOpen: (id, path) => call("POST", "/fs/open" + of(id) + "&path=" + encodeURIComponent(path)),
    FSUploadForm: (id, dir, form) => call("POST", "/fs/upload" + of(id) + "&dir=" + encodeURIComponent(dir), form),
    Settings: () => call("GET", "/settings"),
    SaveSettings: (settings) => call("PUT", "/settings", settings),
    Commands: () => call("GET", "/commands"),
    SaveCommands: (commands) => call("PUT", "/commands", commands),
    async SetDeviceColor(id, color) {
      const settings = await call("GET", "/settings");
      settings.colors[id] = color;
      return call("PUT", "/settings", settings);
    },
    ChooseFolder: async () => "",
    RelaySetup(address, user, password) {
      const [host, port] = address.trim().split(":");
      return call("POST", "/relay/setup", { host, port: Number(port) || 22, user: user.trim() || "root", password });
    },
    RelayJoin: (code) => call("POST", "/relay/join", code),
    RelayLeave: () => call("POST", "/relay/leave"),
    Copy: (text) => navigator.clipboard.writeText(text),
    Quit: () => window.Android && window.Android.quit(),
    InitialView: async () => new URLSearchParams(location.search).get("view") || "",
  };
}
