// The remote control: the phone's screen as a touchpad for a computer,
// with a line to type and the keys for music.

function renderPad(d) {
  let queue = [];
  let busy = false;
  // Movements pile up while a request is on its way and leave together.
  const flush = async () => {
    if (busy || !queue.length) return;
    busy = true;
    const batch = queue;
    queue = [];
    try {
      await api.Input(d.id, batch);
    } catch (err) {
      toast(String(err), true);
      queue = [];
    }
    busy = false;
    flush();
  };
  const send = (...events) => {
    for (const e of events) {
      const last = queue[queue.length - 1];
      const motion = (x) => x && !x.button && !x.key && !x.text;
      if (motion(e) && motion(last)) {
        last.dx = (last.dx || 0) + (e.dx || 0);
        last.dy = (last.dy || 0) + (e.dy || 0);
        last.wheel = (last.wheel || 0) + (e.wheel || 0);
      } else queue.push(e);
    }
    flush();
  };
  const click = (button) => send({ button, pressed: true }, { button, pressed: false });
  const key = (name) => () => send({ key: name });

  const touches = new Map();
  let moved = 0;
  let most = 0;
  let began = 0;
  let scroll = 0;
  let rest = { x: 0, y: 0 };
  const area = h("div", { class: "pad-area" }, h("span", { class: "muted" }, t("padHint")));
  area.addEventListener("pointerdown", (e) => {
    area.setPointerCapture(e.pointerId);
    if (!touches.size) {
      moved = 0;
      most = 0;
      began = Date.now();
    }
    touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
    most = Math.max(most, touches.size);
  });
  area.addEventListener("pointermove", (e) => {
    const was = touches.get(e.pointerId);
    if (!was) return;
    const dx = e.clientX - was.x;
    const dy = e.clientY - was.y;
    touches.set(e.pointerId, { x: e.clientX, y: e.clientY });
    moved += Math.abs(dx) + Math.abs(dy);
    if (touches.size >= 2) {
      // Two fingers scroll, the way a touchpad does.
      scroll += dy / touches.size;
      const steps = Math.trunc(scroll / 24);
      if (steps) {
        scroll -= steps * 24;
        send({ wheel: steps });
      }
      return;
    }
    // Faster strokes carry the pointer further.
    const speed = 1.4 + Math.min(2.2, Math.hypot(dx, dy) / 12);
    rest.x += dx * speed;
    rest.y += dy * speed;
    const sx = Math.trunc(rest.x);
    const sy = Math.trunc(rest.y);
    rest.x -= sx;
    rest.y -= sy;
    if (sx || sy) send({ dx: sx, dy: sy });
  });
  const lift = (e) => {
    if (!touches.delete(e.pointerId) || touches.size) return;
    if (moved < 12 && Date.now() - began < 350) click(most >= 2 ? "right" : "left");
  };
  area.addEventListener("pointerup", lift);
  area.addEventListener("pointercancel", lift);

  const text = h("input", { class: "pad-text", placeholder: t("padType"), autocomplete: "off", autocapitalize: "off", spellcheck: "false" });
  const typed = h(
    "form",
    {
      class: "row pad-typing",
      onsubmit: (e) => {
        e.preventDefault();
        if (text.value) send({ text: text.value });
        else send({ key: "enter" });
        text.value = "";
      },
    },
    text,
    h("button", { class: "button primary" }, t("padSend")),
  );
  const keyButton = (label, name, iconName) => h("button", { class: "pad-key", type: "button", title: label, onclick: key(name) }, iconName ? h("span", { class: "pad-key-icon", icon: iconName }) : label);

  return tinted(
    h(
      "div",
      { class: "page pad" },
      h("header", {}, h("span", { class: "head-icon paired", icon: d.type }), h("div", { class: "head-text" }, h("h1", {}, d.name), h("p", { class: "muted" }, t("padTitle")))),
      area,
      h("div", { class: "pad-buttons" }, h("button", { class: "pad-button", onclick: () => click("left") }, t("padLeft")), h("button", { class: "pad-button", onclick: () => click("right") }, t("padRight"))),
      typed,
      h("div", { class: "pad-keys" }, keyButton("Esc", "esc"), keyButton("Tab", "tab"), keyButton("⌫", "backspace"), keyButton("↵", "enter"), keyButton("←", "left"), keyButton("↑", "up"), keyButton("↓", "down"), keyButton("→", "right")),
      h("div", { class: "pad-keys media" }, keyButton(t("padPrev"), "prev", "prev"), keyButton(t("padPlay"), "playpause", "play"), keyButton(t("padNext"), "next", "next"), keyButton(t("padQuieter"), "voldown", "voldown"), keyButton(t("padMute"), "mute", "mute"), keyButton(t("padLouder"), "volup", "volup")),
    ),
    d.id,
  );
}
