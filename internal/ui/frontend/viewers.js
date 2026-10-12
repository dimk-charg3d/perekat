// What the preview shows files with: a PDF reader, code with line numbers
// and colours, Markdown laid out, the list of an archive, and a frame for
// the plain view of office documents.

const KEYWORDS = new Set(
  (
    "if else for while return function func def class import from package const let var type struct interface switch case break continue default try catch finally " +
    "throw new delete in of is not and or async await yield public private protected static void int string bool true false null nil None True False self this super " +
    "extends implements enum match fn use mod pub impl trait go defer chan map range select with as lambda pass raise except elif do then fi done echo export local"
  ).split(" "),
);
const HASH_COMMENTS = ["py", "sh", "yml", "yaml", "toml", "ini", "conf", "rb", "desktop", "mk"];
const PLAIN_TEXT = ["txt", "log", "csv", ""];

// codeView shows source code: numbered lines, long lines scroll sideways.
function codeView(text, ext) {
  const code = h("pre", { class: "code-text" });
  const lines = text.split("\n").length;
  if (PLAIN_TEXT.includes(ext)) {
    return h("pre", { class: "plain-text" }, text);
  }
  const re = HASH_COMMENTS.includes(ext)
    ? /(#[^\n]*)|("(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*')|(\b\d[\w.]*\b)|(\b[A-Za-z_]\w*\b)/g
    : /(\/\*[\s\S]*?\*\/|\/\/[^\n]*)|("(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|`[^`]*`)|(\b\d[\w.]*\b)|(\b[A-Za-z_]\w*\b)/g;
  let at = 0;
  for (const m of text.matchAll(re)) {
    const kind = m[1] ? "c" : m[2] ? "s" : m[3] ? "n" : KEYWORDS.has(m[4]) ? "k" : "";
    if (!kind) continue;
    if (m.index > at) code.append(text.slice(at, m.index));
    code.append(h("span", { class: "tok-" + kind }, m[0]));
    at = m.index + m[0].length;
  }
  code.append(text.slice(at));
  const gutter = h("pre", { class: "code-gutter" }, Array.from({ length: lines }, (_, i) => i + 1).join("\n"));
  return h("div", { class: "code" }, gutter, code);
}

// inline lays out the marks inside a line of Markdown.
function inline(text) {
  const out = [];
  const re = /(`[^`]+`)|(\*\*[^*]+\*\*|__[^_]+__)|(\*[^*\s][^*]*\*|\b_[^_]+_\b)|(!?\[[^\]]*\]\([^)]*\))/g;
  let at = 0;
  for (const m of text.matchAll(re)) {
    if (m.index > at) out.push(text.slice(at, m.index));
    if (m[1]) out.push(h("code", {}, m[1].slice(1, -1)));
    else if (m[2]) out.push(h("b", {}, ...inline(m[2].slice(2, -2))));
    else if (m[3]) out.push(h("i", {}, ...inline(m[3].slice(1, -1))));
    else {
      const label = m[4].slice(m[4].indexOf("[") + 1, m[4].indexOf("]("));
      const target = m[4].slice(m[4].indexOf("](") + 2, -1);
      out.push(m[4].startsWith("!") ? h("span", { class: "md-link" }, "[" + label + "]") : h("span", { class: "md-link", title: target }, label));
    }
    at = m.index + m[0].length;
  }
  out.push(text.slice(at));
  return out;
}

// markdownView lays out a Markdown document.
function markdownView(text) {
  const root = h("div", { class: "md" });
  const lines = text.replace(/\r/g, "").split("\n");
  let para = [];
  let list = null;
  const flush = () => {
    if (para.length) root.append(h("p", {}, ...inline(para.join(" "))));
    para = [];
  };
  for (let i = 0; i < lines.length; i++) {
    // Markup written straight into the document is dropped: its text stays.
    const line = lines[i].startsWith("```") ? lines[i] : lines[i].replace(/<\/?[a-zA-Z][^>]*>/g, "");
    if (line !== lines[i] && !line.trim()) continue;
    const item = line.match(/^\s*(?:[-*+]|\d+[.)])\s+(.*)$/);
    if (!item) list = null;
    if (line.startsWith("```")) {
      flush();
      const body = [];
      for (i++; i < lines.length && !lines[i].startsWith("```"); i++) body.push(lines[i]);
      root.append(codeView(body.join("\n"), line.slice(3).trim() || "code"));
    } else if (/^#{1,6}\s/.test(line)) {
      flush();
      const level = line.match(/^#+/)[0].length;
      root.append(h("h" + Math.min(level + 1, 6), { class: "md-h" + level }, ...inline(line.slice(level).trim())));
    } else if (/^\s*([-*_])\s*\1\s*\1[\s\-*_]*$/.test(line)) {
      flush();
      root.append(h("hr", {}));
    } else if (item) {
      flush();
      if (!list) root.append((list = h(/^\s*\d/.test(line) ? "ol" : "ul", {})));
      const box = item[1].match(/^\[( |x|X)\]\s+(.*)$/);
      list.append(h("li", { class: box ? "md-task" + (box[1] === " " ? "" : " done") : "" }, ...inline(box ? box[2] : item[1])));
    } else if (line.startsWith(">")) {
      flush();
      root.append(h("blockquote", {}, ...inline(line.replace(/^>\s?/, ""))));
    } else if (line.includes("|") && i + 1 < lines.length && /^\s*\|?[\s:|-]+\|[\s:|-]*$/.test(lines[i + 1])) {
      flush();
      const cells = (row) =>
        row
          .trim()
          .replace(/^\||\|$/g, "")
          .split("|")
          .map((c) => c.trim());
      const table = h("table", {}, h("tr", {}, ...cells(line).map((c) => h("th", {}, ...inline(c)))));
      for (i += 2; i < lines.length && lines[i].includes("|"); i++) table.append(h("tr", {}, ...cells(lines[i]).map((c) => h("td", {}, ...inline(c)))));
      i--;
      root.append(table);
    } else if (!line.trim()) flush();
    else para.push(line.trim());
  }
  flush();
  return root;
}

// archiveView lists what is inside an archive.
function archiveView(res) {
  const entries = res.entries.slice().sort((a, b) => a.path.localeCompare(b.path));
  const total = entries.reduce((sum, e) => sum + (e.dir ? 0 : e.size), 0);
  return h(
    "div",
    { class: "archive" },
    h("p", { class: "muted small" }, t("archiveInfo", entries.filter((e) => !e.dir).length, formatSize(total)) + (res.truncated ? " " + t("archiveMore") : "")),
    ...entries.map((e) => {
      const depth = e.name.split("/").length - 1;
      const row = h("div", { class: "archive-row" }, h("span", { class: "fm-icon " + (e.dir ? "folder" : ""), icon: e.dir ? "folder" : "doc" }), h("span", { class: "fm-name" }, e.name.split("/").pop()), h("span", { class: "fm-meta" }, e.dir ? "" : formatSize(e.size)));
      row.style.paddingLeft = 10 + Math.min(depth, 8) * 18 + "px";
      return row;
    }),
  );
}

// htmlView shows a page made by a device. It is kept in a frame without
// scripts: the page is somebody else's text.
function htmlView(html) {
  const style =
    "<style>body{margin:0;padding:28px 32px;background:#fff;color:#1b1d1f;font:15px/1.6 'Inter','Noto Sans',system-ui,sans-serif;overflow-wrap:anywhere}" +
    "h1{font-size:26px}h2{font-size:21px}h3,h4{font-size:17px}p{margin:.5em 0;min-height:.4em}" +
    "table{border-collapse:collapse;margin:1em 0;max-width:100%}td{border:1px solid #c9ced3;padding:5px 9px;vertical-align:top}td p{margin:0}" +
    "section{position:relative;margin:0 0 18px;padding:22px 24px 22px 60px;border:1px solid #dde1e5;border-radius:10px;background:#fafbfc;overflow-x:auto}" +
    "section>p:first-of-type{font-size:19px;font-weight:600}.slide-no{position:absolute;left:18px;top:22px;width:28px;height:28px;border-radius:50%;background:#1f2225;color:#fff;font:600 13px/28px sans-serif;text-align:center}</style>";
  return h("iframe", { class: "doc-frame", sandbox: "", srcdoc: "<!doctype html><meta charset=utf-8>" + style + html });
}

let pdfLoading = null;
function loadPdfJs() {
  if (!pdfLoading) {
    pdfLoading = new Promise((resolve, reject) => {
      const script = h("script", { src: "vendor/pdf.min.js" });
      script.addEventListener("load", () => {
        window.pdfjsLib.GlobalWorkerOptions.workerSrc = "vendor/pdf.worker.min.js";
        resolve(window.pdfjsLib);
      });
      script.addEventListener("error", () => reject(new Error("pdf.js")));
      document.head.append(script);
    });
  }
  return pdfLoading;
}

// pdfView draws a PDF page by page; a page is drawn when it scrolls into
// sight, so a long document opens as fast as a short one.
function pdfView(url, onInfo) {
  const root = h("div", { class: "pdf" }, h("p", { class: "muted pdf-wait" }, t("loading")));
  (async () => {
    const lib = await loadPdfJs();
    // The window serves files whole; a browser can fetch them piece by piece.
    const doc = await lib.getDocument({ url, disableRange: !api.web, disableStream: !api.web, disableAutoFetch: api.web }).promise;
    root.replaceChildren();
    const width = Math.min(root.clientWidth || 800, 1100);
    const ratio = Math.min(window.devicePixelRatio || 1, 2);
    const drawn = new Set();
    const watcher = new IntersectionObserver(
      (seen) => {
        for (const s of seen) {
          const n = Number(s.target.dataset.page);
          if (!s.isIntersecting || drawn.has(n)) continue;
          drawn.add(n);
          doc.getPage(n).then((page) => {
            const view = page.getViewport({ scale: (width / page.getViewport({ scale: 1 }).width) * ratio });
            const canvas = h("canvas", { width: Math.floor(view.width), height: Math.floor(view.height) });
            s.target.replaceChildren(canvas);
            s.target.style.aspectRatio = "";
            page.render({ canvasContext: canvas.getContext("2d"), viewport: view });
          });
        }
      },
      { root: root.parentElement, rootMargin: "600px 0px" },
    );
    const first = await doc.getPage(1);
    const size = first.getViewport({ scale: 1 });
    for (let n = 1; n <= doc.numPages; n++) {
      const holder = h("div", { class: "pdf-page", "data-page": n });
      holder.style.width = width + "px";
      holder.style.aspectRatio = size.width + " / " + size.height;
      root.append(holder);
      watcher.observe(holder);
    }
    if (onInfo) onInfo(t("pages", doc.numPages));
  })().catch((err) => root.replaceChildren(h("p", { class: "bad" }, t("previewFailed") + " " + String(err && err.message ? err.message : err))));
  return root;
}
