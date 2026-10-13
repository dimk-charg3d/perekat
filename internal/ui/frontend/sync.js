// What is shared with one device: the clipboard, files, notifications, and
// for two computers a mouse and keyboard that serve both.

async function renderSync(d) {
  const s = await api.Sync(d.id);
  const page = h("div", { class: "page" });
  // Every change is saved at once; there is nothing to confirm.
  const save = async () => {
    try {
      await api.SaveSync(d.id, s);
    } catch (err) {
      toast(String(err), true);
    }
    draw();
  };
  const toggle = (key, title, hint, disabled) =>
    h(
      "label",
      { class: "switch-line" + (disabled ? " off" : "") },
      h("span", {}, title, hint ? h("span", { class: "muted small block" }, hint) : null),
      h("input", {
        type: "checkbox",
        class: "switch",
        checked: s[key],
        disabled: !!disabled,
        onchange: (e) => {
          s[key] = e.target.checked;
          save();
        },
      }),
    );
  const why = () => {
    const text = s.inputWhy || "";
    if (text.includes("main device")) return t("inputWhyMain");
    if (text.includes("not connected")) return t("inputWhyOffline");
    if (text.includes("two computers")) return t("inputWhyPhone");
    if (text.includes("this computer")) return t("inputWhySelf");
    if (text.includes("other computer")) return t("inputWhyPeer", d.name);
    return text;
  };

  function draw() {
    const self = state.self.deviceName;
    const main = s.mainSelf ? self : d.name;
    const other = s.mainSelf ? d.name : self;
    const place = (side) =>
      h(
        "button",
        {
          type: "button",
          class: "desk-side " + side + (s.side === side ? " active" : ""),
          title: t("side_" + side),
          onclick: () => {
            s.side = side;
            save();
          },
        },
        s.side === side ? other : "",
      );
    const input = h(
      "div",
      { class: "card" },
      toggle("input", t("inputTitle"), s.canInput ? t("inputHint") : why(), !s.canInput && !s.input),
      s.input
        ? h(
            "div",
            { class: "desk-setup" },
            h("p", { class: "muted small" }, t("inputMain", main)),
            h("p", { class: "muted small" }, t("inputWhere", other, main)),
            h("div", { class: "desk" }, place("top"), place("left"), h("div", { class: "desk-main" }, main), place("right"), place("bottom")),
            h("p", { class: "muted small" }, t("inputHow", other)),
          )
        : null,
    );
    const shared = h(
      "div",
      { class: "card" },
      h("h2", {}, t("syncShared")),
      toggle("clipboard", t("syncClipboard"), t("syncClipboardHint")),
      toggle("files", t("syncFiles"), t("syncFilesHint", d.name)),
      toggle("fileAccess", t("syncAccess"), t("syncAccessHint", d.name)),
      d.type === "phone" ? toggle("notifications", t("syncNotify"), t("syncNotifyHint")) : null,
    );
    page.replaceChildren(
      h("header", {}, h("span", { class: "head-icon paired", icon: d.type }), h("div", { class: "head-text" }, h("h1", {}, d.name), h("p", { class: "muted" }, t("syncTitle")))),
      h("div", { class: "body" }, input, shared),
    );
  }
  draw();
  return tinted(page, d.id);
}
