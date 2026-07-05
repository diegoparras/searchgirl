/* Searchgirl — SPA mínima sobre /api. Sin frameworks, sin CDN. Todo el
   contenido remoto entra por textContent (nunca innerHTML con datos). */
"use strict";

const $ = (sel) => document.querySelector(sel);

const CATS = [
  ["general", "General"], ["news", "Noticias"], ["images", "Imágenes"],
  ["videos", "Videos"], ["science", "Ciencia"], ["it", "IT"],
  ["files", "Archivos"], ["map", "Mapas"], ["music", "Música"],
];

const state = { q: "", category: "general", language: "", timeRange: "", safe: "0", page: 1, loading: false };
let config = null;

/* ---------- init ---------- */

async function init() {
  wireChrome();
  buildTabs();
  wireSearch();

  // /auth/me primero: es la ÚNICA ruta que el gate deja libre. Con sesión
  // pendiente, mostramos el login y no seguimos (el resto de /api está 401).
  let me = null;
  try { me = await (await fetch("/auth/me")).json(); } catch { /* red caída: seguimos */ }
  if (me && me.enabled && !me.authenticated) {
    showLoginGate(me.mode);
    return;
  }
  if (me && me.email) {
    const u = $("#menuUser");
    u.textContent = me.email + (me.role ? " · " + me.role : "");
    u.classList.remove("hidden");
    $("#logoutSep").classList.remove("hidden");
    $("#logoutBtn").classList.remove("hidden");
  }

  try {
    config = await (await fetch("/api/config")).json();
  } catch { config = {}; }
  $("#aboutVersion").textContent = config.version || "—";

  const savedLang = localStorage.getItem("searchgirl.lang") || config.default_language || "es";
  state.language = savedLang;
  $("#langDefault").value = savedLang;
  $("#fLang").value = savedLang;

  if (config.llm && config.llm.available) {
    $("#aiBtn").classList.remove("hidden");
    $("#aiBtn").addEventListener("click", runAnswer);
  }
  // Los paneles de admin (Modelo IA, Conexión) solo los ve un admin.
  if (config.llm && config.llm.can_configure) {
    $("#settingsBtn").classList.remove("hidden");
    wireSettings();
    $("#connBtn").classList.remove("hidden");
    wireConn();
  }

  // Estado desde la URL (compartible / botón atrás).
  readURL();
  if (state.q) runSearch({ push: false });
  window.addEventListener("popstate", () => { readURL(); state.q ? runSearch({ push: false }) : showHome(); });
}

/* ---------- login gate ---------- */

// Federado: solo el botón SSO. Local (usuario del .env): el form con ojito.
function showLoginGate(mode) {
  $("#loginGate").classList.remove("hidden");
  if (mode !== "local") {
    $("#loginSso").classList.remove("hidden");
    return;
  }
  const form = $("#loginForm");
  form.classList.remove("hidden");
  $("#loginUser").focus();

  const pass = $("#loginPass");
  $("#loginPassToggle").addEventListener("click", () => {
    pass.type = pass.type === "password" ? "text" : "password";
    pass.focus();
  });

  form.addEventListener("submit", async (e) => {
    e.preventDefault();
    const err = $("#loginErr");
    err.textContent = "";
    try {
      const r = await fetch("/auth/login", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ user: $("#loginUser").value.trim(), password: pass.value }),
      });
      if (r.ok) { location.reload(); return; }
      const data = await r.json().catch(() => ({}));
      err.textContent = data.error || "No se pudo iniciar sesión";
      pass.value = "";
      pass.focus();
    } catch {
      err.textContent = "No se pudo conectar con el servidor";
    }
  });
}

/* ---------- chrome: kebab, tema, modal ---------- */

function wireChrome() {
  const menu = $("#menu"), kebab = $("#kebab");
  kebab.addEventListener("click", (e) => { e.stopPropagation(); menu.classList.toggle("hidden"); });
  document.addEventListener("click", (e) => { if (!menu.contains(e.target)) menu.classList.add("hidden"); });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape") { menu.classList.add("hidden"); $("#aboutModal").classList.add("hidden"); $("#settingsModal").classList.add("hidden"); $("#connModal").classList.add("hidden"); }
  });

  const toggle = $("#themeToggle");
  toggle.checked = document.documentElement.dataset.theme === "dark";
  toggle.addEventListener("change", () => {
    if (toggle.checked) { document.documentElement.dataset.theme = "dark"; localStorage.setItem("searchgirl.theme", "dark"); }
    else { delete document.documentElement.dataset.theme; localStorage.removeItem("searchgirl.theme"); }
  });

  $("#langDefault").addEventListener("change", (e) => {
    localStorage.setItem("searchgirl.lang", e.target.value);
    state.language = e.target.value;
    $("#fLang").value = e.target.value;
  });

  $("#aboutBtn").addEventListener("click", () => { menu.classList.add("hidden"); $("#aboutModal").classList.remove("hidden"); });
  $("#aboutClose").addEventListener("click", () => $("#aboutModal").classList.add("hidden"));
  $("#aboutModal").addEventListener("click", (e) => { if (e.target === $("#aboutModal")) $("#aboutModal").classList.add("hidden"); });

  $("#homeLink").addEventListener("click", (e) => { e.preventDefault(); showHome(); history.pushState({}, "", "/"); });
}

/* ---------- tabs y filtros ---------- */

function buildTabs() {
  const nav = $("#catTabs");
  for (const [key, label] of CATS) {
    const b = document.createElement("button");
    b.className = "cat-tab" + (key === state.category ? " active" : "");
    b.dataset.cat = key;
    b.textContent = label;
    b.addEventListener("click", () => { state.category = key; runSearch(); });
    nav.appendChild(b);
  }
  $("#fLang").addEventListener("change", (e) => { state.language = e.target.value; runSearch(); });
  $("#fTime").addEventListener("change", (e) => { state.timeRange = e.target.value; runSearch(); });
  $("#fSafe").addEventListener("change", (e) => { state.safe = e.target.value; runSearch(); });
  $("#moreBtn").addEventListener("click", () => { state.page += 1; runSearch({ append: true, push: false }); });
}

function paintTabs() {
  document.querySelectorAll(".cat-tab").forEach((b) => b.classList.toggle("active", b.dataset.cat === state.category));
}

/* ---------- búsqueda ---------- */

function wireSearch() {
  for (const [form, input, sugs] of [["#homeForm", "#homeInput", "#homeSugs"], ["#topForm", "#topInput", "#topSugs"]]) {
    $(form).addEventListener("submit", (e) => {
      e.preventDefault();
      const q = $(input).value.trim();
      if (!q) return;
      state.q = q; state.page = 1;
      hideSugs();
      runSearch();
    });
    wireSuggest($(input), $(sugs));
  }
}

function readURL() {
  const p = new URLSearchParams(location.search);
  state.q = (p.get("q") || "").trim();
  state.category = CATS.some(([k]) => k === p.get("c")) ? p.get("c") : "general";
  state.page = 1;
}

function writeURL() {
  const p = new URLSearchParams();
  p.set("q", state.q);
  if (state.category !== "general") p.set("c", state.category);
  history.pushState({}, "", "/?" + p.toString());
}

async function runSearch(opts = {}) {
  const { append = false, push = true } = opts;
  if (!state.q || state.loading) return;
  if (!append) state.page = 1;
  state.loading = true;

  showResults();
  paintTabs();
  $("#topInput").value = state.q;
  document.title = state.q + " — Searchgirl";
  if (push) writeURL();
  if (!append) {
    renderSkeleton();
    $("#rSide").replaceChildren();
    $("#rEmpty").classList.add("hidden");
    $("#aiCard").classList.add("hidden");
    $("#aiCard").replaceChildren();
  }
  $("#rError").classList.add("hidden");

  const p = new URLSearchParams({ q: state.q, category: state.category, safesearch: state.safe, page: String(state.page) });
  if (state.language) p.set("language", state.language);
  if (state.timeRange) p.set("time_range", state.timeRange);

  try {
    const r = await fetch("/api/search?" + p.toString());
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || "error " + r.status);
    render(data, append);
  } catch (err) {
    if (!append) $("#rList").replaceChildren();
    const box = $("#rError");
    box.textContent = "No se pudo buscar: " + err.message;
    box.classList.remove("hidden");
    $("#moreBtn").classList.add("hidden");
  } finally {
    state.loading = false;
  }
}

/* ---------- render ---------- */

function renderSkeleton() {
  const list = $("#rList");
  list.className = "rlist";
  list.replaceChildren();
  for (let i = 0; i < 5; i++) {
    const it = el("div", "rit skel");
    it.append(el("div", "rit-dom", "········"), el("a", "rit-tit", "························"), el("p", "rit-snip", "········································"));
    list.appendChild(it);
  }
}

function render(data, append) {
  const grid = state.category === "images" || state.category === "videos";
  const list = $("#rList");
  list.className = "rlist" + (grid ? " grid" : "");
  if (!append) list.replaceChildren();

  // Blindaje: un shape inesperado en cualquier lista del backend no debe
  // tumbar toda la búsqueda (degradación elegante).
  const results = arr(data.results);
  for (const res of results) list.appendChild(grid ? cardOf(res) : itemOf(res));

  const none = !append && results.length === 0;
  $("#rEmpty").classList.toggle("hidden", !none);
  $("#moreBtn").classList.toggle("hidden", results.length === 0);

  const meta = data.meta || {};
  const stats = [`${meta.total || 0} resultados`, `${meta.took_ms || 0} ms`];
  $("#rStats").textContent = stats.join(" · ");

  if (!append) {
    renderSide(data);
    renderCorrections(data);
    renderFoot(data);
  }
}

function itemOf(res) {
  const it = el("div", "rit");
  const dom = el("div", "rit-dom");
  dom.append(el("span", "", res.domain || ""));
  if (res.engines && res.engines.length) dom.append(el("span", "eng", "· " + res.engines.join(", ")));
  const a = el("a", "rit-tit", res.title || res.url);
  a.href = res.url; a.target = "_blank"; a.rel = "noopener noreferrer";
  const snip = el("p", "rit-snip");
  if (res.published) snip.append(el("span", "rit-date", res.published));
  snip.append(document.createTextNode(res.snippet || ""));
  it.append(dom, a, snip);
  return it;
}

function cardOf(res) {
  const a = el("a", "rcard");
  a.href = res.url; a.target = "_blank"; a.rel = "noopener noreferrer";
  if (res.thumbnail && /^(https?:)?\/\//.test(res.thumbnail)) {
    const img = document.createElement("img");
    img.loading = "lazy"; img.alt = "";
    // Vía el proxy propio: el navegador nunca toca los hosts de los motores.
    img.src = "/thumb?u=" + encodeURIComponent(res.thumbnail);
    img.addEventListener("error", () => img.remove());
    a.appendChild(img);
  }
  a.append(el("span", "rc-tit", res.title || res.url), el("span", "rc-dom", res.domain || ""));
  return a;
}

function renderSide(data) {
  const side = $("#rSide");
  side.replaceChildren();
  for (const ans of arr(data.answers)) {
    const c = el("div", "side-card");
    c.append(el("div", "side-kicker", "Respuesta directa"), el("p", "", ans));
    side.appendChild(c);
  }
  for (const ib of arr(data.infoboxes)) {
    const c = el("div", "side-card");
    c.append(el("div", "side-kicker", "Infobox"));
    if (ib.title) c.append(el("h3", "", ib.title));
    if (ib.content) c.append(el("p", "", ib.content));
    const isWikidataID = (s) => /^[QP]\d+$/.test((s || "").trim());
    const attrs = (ib.attributes || []).filter((at) => !isWikidataID(at.value) && !isWikidataID(at.label));
    if (attrs.length) {
      const dl = el("dl", "ib-rows");
      for (const at of attrs) dl.append(el("dt", "", at.label), el("dd", "", at.value));
      c.append(dl);
    }
    const urls = (ib.urls || []).filter((u) => !isWikidataID(u.title));
    if (urls.length) {
      const links = el("div", "ib-links");
      for (const u of urls) {
        const a = el("a", "link", u.title || u.url);
        a.href = u.url; a.target = "_blank"; a.rel = "noopener noreferrer";
        links.appendChild(a);
      }
      c.append(links);
    }
    side.appendChild(c);
  }
}

function renderCorrections(data) {
  const box = $("#corrections");
  box.replaceChildren();
  const all = [...(data.corrections || []), ...(data.suggestions || []).slice(0, 3)];
  if (!all.length) { box.classList.add("hidden"); return; }
  box.append(document.createTextNode("Relacionado: "));
  all.forEach((c, i) => {
    if (i) box.append(document.createTextNode(" · "));
    const a = el("a", "link", c);
    a.href = "#";
    a.addEventListener("click", (e) => { e.preventDefault(); state.q = c; state.page = 1; runSearch(); });
    box.appendChild(a);
  });
  box.classList.remove("hidden");
}

function renderFoot(data) {
  const foot = $("#rFoot");
  foot.replaceChildren();
  let engines = new Set();
  for (const r of arr(data.results)) arr(r.engines).forEach((e) => engines.add(e));
  const parts = [`Resultados vía SearXNG · ${engines.size} motores`];
  if (data.meta.engines_failed && data.meta.engines_failed.length) {
    parts.push(`sin respuesta: ${data.meta.engines_failed.join(", ")}`);
  }
  foot.append(document.createTextNode(parts.join(" · ") + " · "));
  const a = el("a", "", "searxng.org");
  a.href = "https://searxng.org"; a.target = "_blank"; a.rel = "noopener noreferrer";
  foot.appendChild(a);
}

/* ---------- Respuesta IA ---------- */

async function runAnswer() {
  if (!state.q) return;
  const card = $("#aiCard"), btn = $("#aiBtn");
  card.replaceChildren(el("div", "side-kicker", "Respuesta IA"), el("p", "ai-load", "Buscando y sintetizando con fuentes…"));
  card.classList.remove("hidden");
  btn.disabled = true;
  try {
    const r = await fetch("/api/answer", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: state.q, category: state.category, language: state.language }),
    });
    const data = await r.json();
    if (!r.ok) throw new Error(data.error || "error " + r.status);
    renderAnswer(data);
  } catch (err) {
    card.replaceChildren(el("div", "side-kicker", "Respuesta IA"), el("p", "ai-err", "No se pudo sintetizar: " + err.message));
  } finally {
    btn.disabled = false;
  }
}

function renderAnswer(data) {
  const card = $("#aiCard");
  card.replaceChildren(el("div", "side-kicker", "Respuesta IA"));

  const srcByN = {};
  for (const s of data.sources) srcByN[s.n] = s;

  const body = el("div", "ai-body");
  for (const para of data.answer.split(/\n{2,}/)) {
    const p = el("p");
    // Solo dos construcciones: citas [n] → link, y **negrita**.
    for (const chunk of para.split(/(\[\d{1,2}\]|\*\*[^*]+\*\*)/)) {
      if (!chunk) continue;
      const cite = chunk.match(/^\[(\d{1,2})\]$/);
      if (cite && srcByN[+cite[1]]) {
        const a = el("a", "ai-cite", chunk);
        a.href = srcByN[+cite[1]].url; a.target = "_blank"; a.rel = "noopener noreferrer";
        a.title = srcByN[+cite[1]].title;
        p.appendChild(a);
      } else if (/^\*\*[^*]+\*\*$/.test(chunk)) {
        p.appendChild(el("strong", "", chunk.slice(2, -2)));
      } else {
        p.appendChild(document.createTextNode(chunk.replace(/\n/g, " ")));
      }
    }
    body.appendChild(p);
  }
  card.appendChild(body);

  const srcs = el("div", "ai-srcs");
  for (const s of data.sources) {
    const row = el("div", "ai-src");
    row.append(document.createTextNode(`[${s.n}] `));
    const a = el("a", "", `${s.title} — ${s.domain}`);
    a.href = s.url; a.target = "_blank"; a.rel = "noopener noreferrer";
    row.appendChild(a);
    srcs.appendChild(row);
  }
  card.appendChild(srcs);
  card.appendChild(el("div", "ai-meta", `Sintetizado por ${data.model} · ${data.took_ms} ms · puede contener errores: verificá las fuentes`));
}

/* ---------- sugerencias ---------- */

function wireSuggest(input, box) {
  let timer = null, items = [], active = -1;

  const paint = () => {
    box.replaceChildren();
    active = -1;
    if (!items.length) { box.classList.add("hidden"); return; }
    items.forEach((s) => {
      const d = el("div", "sug");
      const ic = document.createElementNS("http://www.w3.org/2000/svg", "svg");
      ic.setAttribute("width", "13"); ic.setAttribute("height", "13"); ic.setAttribute("viewBox", "0 0 24 24");
      ic.setAttribute("fill", "none"); ic.setAttribute("stroke", "currentColor"); ic.setAttribute("stroke-width", "2.2");
      ic.innerHTML = '<circle cx="11" cy="11" r="7"/><path d="M16.5 16.5 21 21" stroke-linecap="round"/>';
      d.append(ic, document.createTextNode(s));
      d.addEventListener("mousedown", (e) => { e.preventDefault(); input.value = s; state.q = s; state.page = 1; hideSugs(); runSearch(); });
      box.appendChild(d);
    });
    box.classList.remove("hidden");
  };

  input.addEventListener("input", () => {
    clearTimeout(timer);
    const q = input.value.trim();
    if (q.length < 2) { items = []; paint(); return; }
    timer = setTimeout(async () => {
      try {
        const r = await fetch("/api/suggest?q=" + encodeURIComponent(q));
        const data = await r.json();
        items = (data.suggestions || []).slice(0, 8);
      } catch { items = []; }
      paint();
    }, 200);
  });

  input.addEventListener("keydown", (e) => {
    const kids = [...box.children];
    if (!kids.length || box.classList.contains("hidden")) return;
    if (e.key === "ArrowDown" || e.key === "ArrowUp") {
      e.preventDefault();
      active = e.key === "ArrowDown" ? (active + 1) % kids.length : (active - 1 + kids.length) % kids.length;
      kids.forEach((k, i) => k.classList.toggle("active", i === active));
      input.value = kids[active].textContent;
    } else if (e.key === "Escape") {
      items = []; paint();
    }
  });

  input.addEventListener("blur", () => setTimeout(() => { items = []; paint(); }, 150));
}

function hideSugs() { $("#homeSugs").classList.add("hidden"); $("#topSugs").classList.add("hidden"); }

/* ---------- vistas ---------- */

function showHome() {
  $("#results").classList.add("hidden");
  $("#home").classList.remove("hidden");
  $("#homeInput").value = "";
  document.title = "Searchgirl";
  state.q = "";
  $("#homeInput").focus();
}

function showResults() {
  $("#home").classList.add("hidden");
  $("#results").classList.remove("hidden");
}

/* ---------- panel Modelo IA (solo admin) ---------- */

const LLM_PRESETS = {
  ollama: "http://ollama:11434/v1",
  openrouter: "https://openrouter.ai/api/v1",
  custom: "",
};

function wireSettings() {
  const m = $("#settingsModal");
  $("#settingsBtn").addEventListener("click", openSettings);
  $("#settingsClose").addEventListener("click", () => m.classList.add("hidden"));
  m.addEventListener("click", (e) => { if (e.target === m) m.classList.add("hidden"); });

  const key = $("#setKey");
  $("#setKeyToggle").addEventListener("click", () => { key.type = key.type === "password" ? "text" : "password"; });

  $("#setProvider").addEventListener("change", (e) => {
    const p = e.target.value;
    if (p && p in LLM_PRESETS) {
      $("#setBase").value = LLM_PRESETS[p];
      if (p === "ollama") key.value = ""; // Ollama no usa key
    }
  });

  $("#setLoadModels").addEventListener("click", loadModels);
  $("#setModelSelect").addEventListener("change", (e) => { if (e.target.value) $("#setModel").value = e.target.value; });

  $("#setTest").addEventListener("click", async () => {
    await saveSettings();
    const st = $("#setStatus");
    st.textContent = "probando…"; st.className = "set-status";
    try {
      const r = await (await fetch("/api/settings/test", { method: "POST" })).json();
      st.textContent = r.ok ? ("conecta" + (r.name ? " — " + r.name : "")) : ("no conecta: " + r.error);
      st.className = "set-status " + (r.ok ? "ok" : "bad");
    } catch (e) { st.textContent = "error: " + e.message; st.className = "set-status bad"; }
  });

  $("#setSave").addEventListener("click", async () => {
    await saveSettings();
    m.classList.add("hidden");
    // Refrescar el estado del LLM en la UI (botón Respuesta IA).
    try {
      config = await (await fetch("/api/config")).json();
      $("#aiBtn").classList.toggle("hidden", !(config.llm && config.llm.available));
    } catch { /* noop */ }
  });
}

async function openSettings() {
  $("#menu").classList.add("hidden");
  let s = {};
  try { s = await (await fetch("/api/settings")).json(); } catch { /* noop */ }
  $("#setBase").value = s.base_url || "";
  $("#setModel").value = s.model || "";
  $("#setKey").value = "";
  $("#setKey").placeholder = s.has_key ? "•••• guardada — vacío = no cambiar" : "tu API key";
  $("#setProvider").value = "";
  $("#setModelSelect").classList.add("hidden");
  $("#setModelHint").textContent = "";
  const st = $("#setStatus");
  st.textContent = s.configured ? ("activo: " + s.name) : "apagado";
  st.className = "set-status " + (s.configured ? "ok" : "");
  if (!s.persisted) {
    $("#setModelHint").textContent = "Sin volumen /config: la elección no persiste tras reiniciar.";
    $("#setModelHint").className = "model-hint";
  }
  $("#settingsModal").classList.remove("hidden");
}

async function saveSettings() {
  const body = JSON.stringify({ base_url: $("#setBase").value.trim(), model: $("#setModel").value.trim(), api_key: $("#setKey").value });
  return (await fetch("/api/settings", { method: "POST", headers: { "Content-Type": "application/json" }, body })).json();
}

async function loadModels() {
  const hint = $("#setModelHint"), sel = $("#setModelSelect"), btn = $("#setLoadModels");
  const base = $("#setBase").value.trim(), key = $("#setKey").value;
  if (!base) { hint.textContent = "Primero poné el servidor (base URL)."; hint.className = "model-hint bad"; return; }
  btn.disabled = true; hint.textContent = "buscando modelos…"; hint.className = "model-hint";
  let r;
  try {
    r = await (await fetch("/api/settings/models", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ base_url: base, api_key: key }) })).json();
  } catch (e) { btn.disabled = false; hint.textContent = "error de red: " + e.message; hint.className = "model-hint bad"; return; }
  btn.disabled = false;
  if (!r.ok) { hint.textContent = "No pude listar modelos (" + (r.error || "?") + "). Escribí el nombre a mano."; hint.className = "model-hint bad"; sel.classList.add("hidden"); return; }
  sel.replaceChildren();
  const ph = el("option", null, "— elegí un modelo —"); ph.value = ""; sel.appendChild(ph);
  const group = (label, arr) => {
    if (!arr.length) return;
    const g = document.createElement("optgroup"); g.label = label;
    arr.forEach((mo) => { const o = el("option", null, mo.id); o.value = mo.id; g.appendChild(o); });
    sel.appendChild(g);
  };
  const rec = r.models.filter((mo) => mo.recommended), rest = r.models.filter((mo) => !mo.recommended);
  group("★ Recomendados", rec);
  group("Todos los modelos", rest);
  sel.classList.remove("hidden");
  hint.className = "model-hint ok";
  hint.textContent = r.count + " modelos" + (rec.length ? " · recomendados: " + rec.slice(0, 3).map((mo) => mo.id).join(", ") : "");
}

/* ---------- panel Conexión · API y MCP (solo admin) ---------- */

function wireConn() {
  const m = $("#connModal");
  $("#connBtn").addEventListener("click", openConn);
  $("#connClose").addEventListener("click", () => m.classList.add("hidden"));
  m.addEventListener("click", (e) => { if (e.target === m) m.classList.add("hidden"); });
  $("#tkIssue").addEventListener("click", issueToken);
  // Copiar de cualquier botón con data-copy (URLs).
  m.addEventListener("click", (e) => {
    const btn = e.target.closest("[data-copy]");
    if (btn) copyText($("#" + btn.dataset.copy).textContent, btn);
  });
}

async function openConn() {
  $("#menu").classList.add("hidden");
  const base = location.origin;
  $("#connMcpUrl").textContent = base + "/mcp";
  $("#connApiUrl").textContent = base + "/api/search?q=...";
  $("#tkReveal").classList.add("hidden");
  $("#tkReveal").replaceChildren();
  $("#tkLabel").value = "";
  await loadTokens();
  $("#connModal").classList.remove("hidden");
}

async function loadTokens() {
  const list = $("#tkList");
  list.replaceChildren();
  let data = { tokens: [], persisted: true };
  try { data = await (await fetch("/api/tokens")).json(); } catch { /* noop */ }
  for (const t of (data.tokens || [])) {
    const row = el("div", "tk-item");
    const info = el("span");
    info.append(el("span", "tk-lbl", t.label));
    let meta = " · creado " + t.created;
    if (t.expires) meta += " · vence " + t.expires;
    if (t.last_used) meta += " · usado " + t.last_used;
    info.append(el("span", "tk-meta", meta));
    const revoke = el("button", "mini ghost tk-revoke", "revocar");
    revoke.addEventListener("click", () => revokeToken(t.id));
    row.append(info, el("span", "sp"), revoke);
    list.appendChild(row);
  }
  if (data.persisted === false) {
    const w = el("div", "tk-meta", "Sin volumen /config: los tokens no persisten tras reiniciar.");
    list.appendChild(w);
  }
}

async function issueToken() {
  const label = $("#tkLabel").value.trim();
  const expires = parseInt($("#tkExpires").value, 10) || 0;
  const btn = $("#tkIssue");
  btn.disabled = true;
  let r;
  try {
    r = await (await fetch("/api/tokens", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ label, expires_days: expires }) })).json();
  } catch (e) { btn.disabled = false; return; }
  btn.disabled = false;
  if (!r.ok) return;

  // Mostrar el secreto UNA vez + la config MCP lista para copiar.
  const base = location.origin;
  const cfg = JSON.stringify({ mcpServers: { searchgirl: { type: "http", url: base + "/mcp", headers: { Authorization: "Bearer " + r.token } } } }, null, 2);
  const cmd = `claude mcp add --transport http searchgirl ${base}/mcp --header "Authorization: Bearer ${r.token}"`;

  const rev = $("#tkReveal");
  rev.replaceChildren();
  rev.append(el("div", "tk-reveal-lbl", "Copiá este token ahora — no se vuelve a mostrar:"));
  rev.appendChild(tkRow(r.token));
  rev.append(el("div", "tk-reveal-lbl", "Config para el .mcp.json de Claude Code / Desktop:"));
  rev.appendChild(preCopy(cfg));
  rev.append(el("div", "tk-reveal-lbl", "…o el comando de Claude Code:"));
  rev.appendChild(preCopy(cmd));
  rev.classList.remove("hidden");

  $("#tkLabel").value = "";
  await loadTokens();
}

async function revokeToken(id) {
  try { await fetch("/api/tokens?id=" + encodeURIComponent(id), { method: "DELETE" }); } catch { /* noop */ }
  await loadTokens();
}

// tkRow: una línea con un valor en <code> y botón copiar.
function tkRow(value) {
  const row = el("div", "tk-row");
  const code = el("code", null, value);
  const btn = el("button", "mini ghost", "copiar");
  btn.addEventListener("click", () => copyText(value, btn));
  row.append(code, btn);
  return row;
}

// preCopy: un bloque <pre> con botón copiar.
function preCopy(value) {
  const wrap = el("div");
  const pre = el("pre", "tk-cfg", value);
  const btn = el("button", "mini ghost", "copiar");
  btn.style.marginTop = "6px";
  btn.addEventListener("click", () => copyText(value, btn));
  wrap.append(pre, btn);
  return wrap;
}

function copyText(text, btn) {
  navigator.clipboard.writeText(text).then(() => {
    if (!btn) return;
    const prev = btn.textContent;
    btn.textContent = "copiado ✓";
    setTimeout(() => { btn.textContent = prev; }, 1400);
  }).catch(() => { /* clipboard bloqueado: sin feedback */ });
}

/* ---------- util ---------- */

function el(tag, cls, text) {
  const n = document.createElement(tag);
  if (cls) n.className = cls;
  if (text !== undefined) n.textContent = text;
  return n;
}

// arr coacciona cualquier valor a un array iterable: si el backend devuelve
// null/objeto/undefined en una lista, la vista degrada en vez de romperse.
function arr(v) { return Array.isArray(v) ? v : []; }

init();
