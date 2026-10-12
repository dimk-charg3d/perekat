// The clipboard history: what was copied lately on any of the devices.
// Touching an entry copies it again, everywhere.

const clip = { entries: [], query: "", timer: null };

async function loadClips() {
  try {
    clip.entries = (await api.Clips()) || [];
  } catch (err) {
    clip.entries = [];
  }
  drawClips();
}

function drawClips() {
  const list = $("clip-list");
  if (!list) return clearInterval(clip.timer);
  const needle = clip.query.toLowerCase();
  const shown = clip.entries.filter((e) => !needle || e.text.toLowerCase().includes(needle)).sort((a, b) => (b.pinned ? 1 : 0) - (a.pinned ? 1 : 0));
  if (!shown.length) return list.replaceChildren(h("p", { class: "muted folder-empty" }, t(clip.entries.length ? "nothingFound" : "clipEmpty")));
  const act = async (action, e, done) => {
    try {
      await api.ClipAction(action, String(e.id));
      if (done) toast(done);
    } catch (err) {
      toast(String(err), true);
    }
    loadClips();
  };
  list.replaceChildren(
    ...shown.map((e) =>
      h(
        "div",
        { class: "clip" + (e.pinned ? " pinned" : ""), onclick: () => act("use", e, t("copied")) },
        h("div", { class: "clip-text" }, e.text.length > 600 ? e.text.slice(0, 600) + "…" : e.text),
        h(
          "div",
          { class: "clip-meta" },
          h("span", {}, e.from + " · " + new Date(e.time).toLocaleString(language, { day: "2-digit", month: "short", hour: "2-digit", minute: "2-digit" }) + (e.text.length > 600 ? " · " + formatSize(e.text.length) : "")),
          h("button", {
            class: "icon-button" + (e.pinned ? " on" : ""),
            title: t(e.pinned ? "clipUnpin" : "clipPin"),
            icon: "star",
            onclick: (ev) => {
              ev.stopPropagation();
              act("pin", e);
            },
          }),
          h("button", {
            class: "icon-button danger",
            title: t("delete"),
            icon: "trash",
            onclick: (ev) => {
              ev.stopPropagation();
              act("drop", e);
            },
          }),
        ),
      ),
    ),
  );
}

function renderClips() {
  const search = h("input", {
    class: "clip-search",
    placeholder: t("clipSearch"),
    spellcheck: "false",
    oninput: (e) => {
      clip.query = e.target.value;
      drawClips();
    },
  });
  clip.query = "";
  clearInterval(clip.timer);
  clip.timer = setInterval(() => (view.kind === "clipboard" ? loadClips() : clearInterval(clip.timer)), 1500);
  loadClips();
  setTimeout(() => search.focus(), 0);
  return h(
    "div",
    { class: "page wide clips" },
    h("header", {}, h("span", { class: "head-icon", icon: "paste" }), h("div", { class: "head-text" }, h("h1", {}, t("clipTitle")), h("p", { class: "muted" }, t("clipIntro")))),
    h(
      "div",
      { class: "body" },
      h(
        "div",
        { class: "row" },
        search,
        h(
          "button",
          {
            class: "button",
            onclick: async () => {
              await api.ClipAction("drop", "0");
              loadClips();
            },
          },
          t("clipClear"),
        ),
      ),
      h("div", { id: "clip-list", class: "clip-list" }),
    ),
  );
}

// qrImage makes a picture of a QR code for a text.
async function qrImage(text) {
  const img = h("img", { class: "qr", alt: "QR" });
  img.src = api.QR ? await api.QR(text) : "";
  return img;
}
