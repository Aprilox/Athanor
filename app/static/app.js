/* Athanor — interface. Vanilla JS, no dependency. All strings go through t(). */
"use strict";

const CSRF = document.querySelector('meta[name="csrf"]').content;
const $ = (s, r = document) => r.querySelector(s);
const h = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
// pilot phase (STATE.json) -> status dot class
const classePhase = (x) => !x || !x.actif ? "" : ({ travail: "travaille", quota: "quota", attente_vous: "quota", erreur: "erreur", connexion: "erreur", entre_cycles: "entre_cycles" }[x.phase] || "travaille");
// escaped text with `code` spans rendered (the plan and requests are Markdown-ish)
const md = (s) => h(s).replace(/`([^`]+)`/g, "<code>$1</code>");
// a milestone as the pilot reports it is the full plan line: find its short title in the plan
// Milestone state for the plan lists: done, in progress (the first unchecked one, or in parallel mode
// every milestone a worker is building), or to do.
function etatDuJalon(j, premierLibre) {
  if (j.fait) return "coche";
  const ouvriers = (etat.vue && etat.vue.actif && etat.vue.etat && etat.vue.etat.ouvriers || "").split(/\s+/).filter(Boolean);
  if (ouvriers.length) {
    const m = j.titre.match(/^([A-Za-z]+\d+(?:[.\-]?[A-Za-z0-9]+)?)/);
    const id = m ? m[1].replace(/[^A-Za-z0-9_]/g, "_").slice(0, 20) : "";
    return id && ouvriers.includes(id) ? "en-cours" : "a-faire";
  }
  return premierLibre() ? "en-cours" : "a-faire";
}

function titreJalon(texte) {
  if (!texte) return "";
  texte = texte.replace(/^- \[[ xX]\] +/, "");
  const j = ((etat.vue && etat.vue.plan) || []).find((x) => x.complet === texte || texte.startsWith(x.complet) || x.complet.startsWith(texte));
  if (j) return j.titre;
  const coupe = texte.split(/ — | – /)[0];
  return coupe.length > 90 ? coupe.slice(0, 88) + "…" : coupe;
}
// A milestone: one line with its title; a click opens the details (files, dependencies, acceptance,
// notes) and the actions (move, remove) that edit PLAN.md. `etat.ouverts` remembers which are open.
function rendreJalon(j, etatJalon, compact) {
  const ico = j.fait ? "✔" : etatJalon === "en-cours" ? "▶" : "○";
  const cle = "jalon:" + (j.id || j.titre), ouvert = etat.ouverts.has(cle);
  const peutEditer = !j.fait && !(etatJalon === "en-cours" && etat.vue && etat.vue.actif); // "next up" while stopped is still editable
  const details = ouvert ? `<div class="jalon-details">
      ${j.detail ? `<div>${md(j.detail)}</div>` : ""}
      ${j.fichiers ? `<div><span class="muet">${t("Files")}</span> <code>${h(j.fichiers)}</code></div>` : ""}
      ${j.depend && j.depend.length ? `<div><span class="muet">${t("Depends on")}</span> ${j.depend.map((d) => `<code>${h(d)}</code>`).join(" ")}</div>` : ""}
      ${j.acceptation ? `<div><span class="muet">${t("Acceptance")}</span> ${md(j.acceptation)}</div>` : ""}
      ${j.notes && j.notes.length ? `<ul class="jalon-notes">${j.notes.map((n) => `<li>${md(n)}</li>`).join("")}</ul>` : ""}
      ${j.id ? `<div class="ligne jalon-actions">
        ${peutEditer ? `<button class="btn btn-petit" data-jalon="${h(j.id)}" data-jalon-action="monter" title="${h(t("Move up"))}">↑</button><button class="btn btn-petit" data-jalon="${h(j.id)}" data-jalon-action="descendre" title="${h(t("Move down"))}">↓</button><button class="btn btn-petit btn-danger" data-jalon="${h(j.id)}" data-jalon-action="retirer">${t("Remove from the plan")}</button>` : `<span class="muet petit">${j.fait ? t("Done: it stays in the plan.") : t("Being built: stop after the cycle to change it.")}</span>`}
      </div>` : ""}
    </div>` : "";
  return `${j.vague ? `<li class="vague">${h(j.vague)}</li>` : ""}<li class="${etatJalon} ${ouvert ? "ouvert" : ""}" data-jalon-cle="${h(cle)}"><span class="jalon-ico">${ico}</span><div class="jalon-corps"><div class="jalon-titre">${md(j.titre)}<span class="jalon-chevron">${ouvert ? "▾" : "▸"}</span></div>${details}</div></li>`;
}

// The plan's list: the milestones the pilot archived (done long ago) are folded into one line that opens them.
function listePlan(plan, compact) {
  let premier = true;
  const archives = plan.filter((j) => j.archive), reste = plan.filter((j) => !j.archive);
  const ouvert = etat.ouverts.has("plan:archives");
  const ligne = (j) => rendreJalon(j, etatDuJalon(j, () => premier ? (premier = false, true) : false), compact);
  const tete = archives.length ? `<li class="coche plan-archives" data-plan-archives><span class="jalon-ico">✔</span><div class="jalon-corps"><div class="jalon-titre">${t("{0} milestone(s) done earlier", [archives.length])}<span class="jalon-chevron">${ouvert ? "▾" : "▸"}</span></div></div></li>` : "";
  return tete + (ouvert ? archives.map(ligne).join("") : "") + reste.map(ligne).join("");
}

// ----------------------------------------------------------------------------- state
const etat = {
  pieces: {}, // files waiting to be attached, per form
  pvTic: 0, pvAnnonces: {}, pvSignature: "",
  pvForm: {}, // answers being written on the For you page (text and secret values, per task; memory only)
  page: "suivi", projet: "", vue: null, verrouille: false, langue: "en", theme: "", accent: "", assistant: -1, visite: -1, demarrage: {},
  dernierJournal: 0, evenements: [], ouverts: new Set(),
  filtres: { pilote: true, texte: true, outil: true, resultat: false, sous_agent: true, brut: false }, recherche: "",
  tacheChoisie: null, verifChoisie: null, envSecrets: false, suiviAutoScroll: true, explorateur: "", selecteurOuvert: false, rechercheProjet: "",
};

// ----------------------------------------------------------------------------- api
function messageDe(r) {
  if (!r) return "";
  if (r.message_key) return t(r.message_key, r.args || []);
  return r.message || "";
}
async function api(chemin, methode = "GET", corps) {
  if (etat.projet) chemin += (chemin.includes("?") ? "&" : "?") + "p=" + encodeURIComponent(etat.projet);
  const r = await fetch("/api/" + chemin, {
    method: methode, headers: { "X-Athanor": CSRF, "Content-Type": "application/json" },
    body: corps === undefined ? undefined : JSON.stringify(corps),
  });
  const data = await r.json().catch(() => ({}));
  if (r.status === 401 && data.erreur_key === "Locked") { etat.verrouille = true; afficherVerrou(); throw new Error(t("Locked")); }
  if (!r.ok) throw new Error(data.erreur_key ? t(data.erreur_key, data.args || []) : (data.erreur || t("Error {0}", [r.status])));
  return data;
}
let toastTimer;
function toast(msg, genre = "") {
  const el = $("#toast");
  el.textContent = msg; el.className = "toast " + genre; el.hidden = false;
  clearTimeout(toastTimer); toastTimer = setTimeout(() => (el.hidden = true), 6000);
}
async function action(promesse, genreOk = "ok") {
  try { const r = await promesse; const m = messageDe(r); if (m) toast(m, genreOk); return r; }
  catch (e) { if (e.message !== t("Locked")) toast(e.message, "erreur"); return null; }
}

// choisir(message, [{ id, texte, primaire, danger }], { titre }) → Promise<id | null> (Escape: null)
function choisir(message, boutons, options = {}) {
  const fond = $("#choix");
  $("#choix-texte").textContent = message;
  $("#choix-titre").textContent = options.titre || ""; $("#choix-titre").hidden = !options.titre;
  $("#choix-boutons").innerHTML = boutons.map((b) => `<button type="button" class="btn ${b.primaire ? "btn-primaire" : b.danger ? "btn-danger" : ""}" data-choix="${h(b.id)}">${h(b.texte)}</button>`).join("");
  const avant = document.activeElement;
  fond.hidden = false;
  const premier = $("#choix-boutons .btn-primaire") || $("#choix-boutons button"); if (premier) premier.focus();
  return new Promise((resoudre) => {
    const fin = (v) => { fond.hidden = true; fond.onclick = null; document.removeEventListener("keydown", clavier, true); if (avant && avant.focus) avant.focus(); resoudre(v); };
    const clavier = (e) => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); fin(null); } };
    fond.onclick = (e) => { const b = e.target.closest("[data-choix]"); if (b) fin(b.dataset.choix); else if (e.target === fond) fin(null); };
    document.addEventListener("keydown", clavier, true);
  });
}

// Start: always there. When the tooling was not seen installed or the request is empty, the user is asked
// first (install, open the check, or start anyway): nobody stays stuck on the first steps.
async function demarrerPilote(b) {
  const v = etat.vue || {};
  if (v.demande_vide) {
    const c = await choisir(t("The project has no request yet: Claude would wait for one. Write what you want first."), [
      { id: "demande", texte: t("Write the request"), primaire: true }, { id: "", texte: t("Cancel") }]);
    if (c === "demande") location.hash = "#demandes";
    return;
  }
  if (!v.kit_installe) {
    const mode = !v.windows ? t("the tooling (Claude Code and the system sandbox)") : v.natif ? t("the tooling of native Windows (Git for Windows, Node.js, Claude Code)") : t("the tooling of WSL (WSL2, Claude Code, the system sandbox)");
    const c = await choisir(t("Athanor has not seen {0} installed yet. Did you install it?", [mode]), [
      { id: "installer", texte: t("Install it now") }, { id: "sante", texte: t("Open the check") }, { id: "demarrer", texte: t("Start anyway"), primaire: true }],
      { titre: t("Before starting") });
    if (c === "installer") { location.hash = "#sante"; return action(api("sante/installer", "POST", { mode: v.windows ? (v.natif ? "natif" : "wsl") : "" })); }
    if (c === "sante") { location.hash = "#sante"; return; }
    if (c !== "demarrer") return;
  }
  if (b) b.disabled = true;
  etat.demarrage[etat.projet] = Date.now() + 25000;
  await action(api("pilote/demarrer", "POST"));
  setTimeout(rafraichir, 800);
}

// ----------------------------------------------------------------------------- confirmation dialog
// The app's own dialog: the browser's confirm()/alert() would show the page address and look like a web page.
// confirmer(message, { ok, annuler, danger }) → Promise<boolean>
function confirmer(message, options = {}) {
  const fond = $("#dialogue"), ok = $("#dialogue-ok"), annuler = $("#dialogue-annuler");
  $("#dialogue-texte").textContent = message;
  $("#dialogue-titre").textContent = options.titre || "";
  $("#dialogue-titre").hidden = !options.titre;
  ok.textContent = options.ok || t("OK");
  annuler.textContent = options.annuler || t("Cancel");
  ok.className = "btn " + (options.danger ? "btn-danger" : "btn-primaire");
  const avant = document.activeElement;
  fond.hidden = false;
  ok.focus();
  return new Promise((resoudre) => {
    const fin = (valeur) => {
      fond.hidden = true;
      ok.onclick = annuler.onclick = fond.onclick = null;
      document.removeEventListener("keydown", clavier, true);
      if (avant && avant.focus) avant.focus();
      resoudre(valeur);
    };
    const clavier = (e) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); fin(false); }
      else if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); fin(document.activeElement !== annuler); }
      else if (e.key === "Tab") { e.preventDefault(); (document.activeElement === ok ? annuler : ok).focus(); }
    };
    ok.onclick = () => fin(true);
    annuler.onclick = () => fin(false);
    fond.onclick = (e) => { if (e.target === fond) fin(false); };
    document.addEventListener("keydown", clavier, true);
  });
}

// Desktop app, not a web page: the context menu (copy, paste…) only on text fields, logs and selected text.
document.addEventListener("contextmenu", (e) => {
  const selection = String(window.getSelection ? window.getSelection() : "").trim();
  if (!e.target.closest("input, textarea, [contenteditable], .journal, pre, code") && !selection) e.preventDefault();
});
// Progress chart: a bubble over a milestone point, its details on click (rendreAvancement).
function bulleAvancement() {
  let b = $("#av-bulle");
  if (!b) { b = document.createElement("div"); b.id = "av-bulle"; b.className = "av-bulle"; document.body.appendChild(b); }
  return b;
}
// the milestone points under the pointer (16 px around it), nearest first: points close together on the
// time axis stay reachable (a click on several opens a list of them)
const RAYON_AV = 16;
function pointsAvancement(e) {
  const svg = e.target.closest && e.target.closest("svg.avancement");
  const infos = etat.avInfos || [];
  if (!svg || !infos.length || !svg.getScreenCTM) return [];
  const m = svg.getScreenCTM();
  if (!m) return [];
  const pt = svg.createSVGPoint(); pt.x = e.clientX; pt.y = e.clientY;
  const q = pt.matrixTransform(m.inverse()), k = Math.abs(m.a || 1);
  return infos.map((p, i) => [i, Math.hypot(p.cx - q.x, p.cy - q.y) * k]).filter(([, d]) => d <= RAYON_AV).sort((a, b) => a[1] - b[1]).map(([i]) => i);
}
function pointAvancement(e) { const l = pointsAvancement(e); return l.length ? l[0] : -1; }
function fermerListeAvancement() { const l = $("#av-liste"); if (l) l.remove(); }
function listeAvancement(indices, e) {
  fermerListeAvancement();
  const infos = etat.avInfos || [];
  const d = document.createElement("div");
  d.id = "av-liste"; d.className = "av-liste";
  d.innerHTML = `<div class="muet petit">${h(t("{0} milestones here", [indices.length]))}</div>` + indices.slice().sort((a, b) => a - b)
    .map((i) => `<button type="button" data-av-liste="${i}"><b>${md(infos[i].titre)}</b> <span class="muet petit">${h(infos[i].quand)}</span></button>`).join("");
  document.body.appendChild(d);
  const l = d.offsetWidth, hl = d.offsetHeight;
  d.style.left = Math.max(8, Math.min(window.innerWidth - l - 8, e.clientX - l / 2)) + "px";
  d.style.top = (e.clientY + hl + 20 < window.innerHeight ? e.clientY + 14 : Math.max(8, e.clientY - hl - 14)) + "px";
}
function marquerSurvol(i) {
  document.querySelectorAll("svg.avancement .av-point").forEach((c) => c.classList.toggle("survol", Number(c.dataset.av) === i));
  const svg = $("svg.avancement"); if (svg) svg.style.cursor = i >= 0 ? "pointer" : "";
}
document.addEventListener("mousemove", (e) => {
  const sous = pointsAvancement(e), i = sous.length ? sous[0] : -1;
  const p = i >= 0 ? (etat.avInfos || [])[i] : null;
  const b = $("#av-bulle");
  if ($("svg.avancement")) marquerSurvol(i);
  if (!p || $("#av-liste")) { if (b) b.style.display = "none"; return; }
  const bulle = bulleAvancement();
  bulle.innerHTML = sous.length > 1 ? `<b>${h(t("{0} milestones here", [sous.length]))}</b><div class="muet petit">${h(t("Click to choose one"))}</div>`
    : `<b>${md(p.titre)}</b><div class="muet petit">${h(p.quand)} · ${p.cout.toFixed(2)} $</div>`;
  bulle.style.display = "block";
  const l = bulle.offsetWidth, hb = bulle.offsetHeight;
  bulle.style.left = Math.max(8, Math.min(window.innerWidth - l - 8, e.clientX - l / 2)) + "px";
  bulle.style.top = Math.max(8, e.clientY - hb - 14) + "px";
});
document.addEventListener("click", (e) => {
  const f = e.target.closest("[data-av-fermer]"), pas = e.target.closest("[data-av-pas]"), dansListe = e.target.closest("[data-av-liste]");
  if (!dansListe && !e.target.closest("#av-liste")) fermerListeAvancement();
  const sous = f || pas || dansListe ? [] : pointsAvancement(e);
  if (sous.length > 1) { const b = $("#av-bulle"); if (b) b.style.display = "none"; listeAvancement(sous, e); return; }
  const i = dansListe ? Number(dansListe.dataset.avListe) : sous.length ? sous[0] : -1;
  if (!f && !pas && i < 0) return;
  fermerListeAvancement();
  const infos = etat.avInfos || [];
  if (dansListe) etat.avChoix = infos[i] ? infos[i].cle : null;
  else if (pas) {
    // previous or next milestone: the way through points too close together to aim at
    const j = infos.findIndex((p) => p.cle === etat.avChoix) + Number(pas.dataset.avPas);
    if (j < 0 || j >= infos.length) return;
    etat.avChoix = infos[j].cle;
  } else if (!dansListe) {
    const p = infos[i];
    etat.avChoix = f || !p || etat.avChoix === p.cle ? null : p.cle;
  }
  const b = $("#av-bulle"); if (b) b.style.display = "none";
  rendrePage();
});
document.addEventListener("keydown", (e) => {
  if (e.key === "Escape" && $("#av-liste")) { fermerListeAvancement(); return; }
  if (!etat.avChoix || !["ArrowLeft", "ArrowRight", "Escape"].includes(e.key) || e.target.closest("input, textarea, select, [contenteditable]")) return;
  const infos = etat.avInfos || [];
  const j = e.key === "Escape" ? -2 : infos.findIndex((p) => p.cle === etat.avChoix) + (e.key === "ArrowLeft" ? -1 : 1);
  if (j === -2) etat.avChoix = null; else if (j >= 0 && j < infos.length) etat.avChoix = infos[j].cle; else return;
  e.preventDefault();
  rendrePage();
});

// …and none of the browser's own shortcuts (reload, print, find, view source, developer tools, downloads, new window).
document.addEventListener("keydown", (e) => {
  const k = e.key.toLowerCase(), ctrl = e.ctrlKey || e.metaKey;
  if (["f3", "f5", "f7", "f12"].includes(k) || (ctrl && ["r", "p", "f", "g", "h", "j", "u", "o", "s", "n", "t"].includes(k)) || (ctrl && e.shiftKey && ["i", "j", "c"].includes(k))) e.preventDefault();
}, true);
// One window only: Ctrl, Shift or middle click on a link would open a browser-like window; it acts as a
// plain click instead. Dragging a link or dropping a file on the window does not navigate either.
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[href]");
  if (!a || !(e.ctrlKey || e.shiftKey || e.metaKey || e.altKey)) return;
  e.preventDefault();
  const cible = a.getAttribute("href");
  if (cible.startsWith("#") && location.hash !== cible) location.hash = cible;
}, true);
document.addEventListener("auxclick", (e) => { if (e.target.closest("a[href], a[data-href]")) e.preventDefault(); }, true);
// Internal links carry their target in data-href, not href: a link under the mouse is never previewed at
// the bottom of the window (which would show a web page), whatever window shows the app (WebView2, Edge…).
function sansHref(racine) {
  const liens = racine.matches && racine.matches('a[href^="#"]') ? [racine] : [...(racine.querySelectorAll ? racine.querySelectorAll('a[href^="#"]') : [])];
  for (const a of liens) { a.dataset.href = a.getAttribute("href"); a.removeAttribute("href"); if (!a.hasAttribute("tabindex")) a.tabIndex = 0; }
}
new MutationObserver((ms) => { for (const m of ms) for (const n of m.addedNodes) if (n.nodeType === 1) sansHref(n); }).observe(document.documentElement, { childList: true, subtree: true });
sansHref(document);
document.addEventListener("click", (e) => {
  const a = e.target.closest("a[data-href]");
  if (!a || e.defaultPrevented || a.getAttribute("aria-disabled") === "true") return;
  e.preventDefault();
  if (location.hash !== a.dataset.href) location.hash = a.dataset.href;
});
document.addEventListener("keydown", (e) => { if (e.key === "Enter" && e.target.matches && e.target.matches("a[data-href]")) e.target.click(); });
document.addEventListener("mousedown", (e) => { if (e.button === 1) e.preventDefault(); }, true);
document.addEventListener("dragstart", (e) => { if (e.target.closest && e.target.closest("a, img")) e.preventDefault(); });
const fichiersGlisses = (e) => e.dataTransfer && [...(e.dataTransfer.types || [])].includes("Files");
document.addEventListener("dragover", (e) => { if (fichiersGlisses(e) || !e.target.closest("input, textarea")) e.preventDefault(); });
document.addEventListener("drop", (e) => { if (fichiersGlisses(e) || !e.target.closest("input, textarea")) e.preventDefault(); });
window.open = () => null;
document.addEventListener("focusin", (e) => { if (e.target.matches && e.target.matches("input, textarea") && !e.target.hasAttribute("autocomplete")) e.target.setAttribute("autocomplete", "off"); });

// A pilot that ended on its own (delivery, maximum duration…) is announced once, inside the app, for
// every project in the list; the end stays announced until the user has seen it (even after a restart).
const finsAnnoncees = new Set();
// a new task for the user in the current project: a dialog (once per task and round), unless already on the page
async function annoncerTaches(v) {
  if (v.verrouille || !v.projet || !$("#dialogue").hidden || !$("#assistant").hidden) return;
  const n = v.pour_vous || 0, cle = v.projet.nom;
  const avant = etat.pvAnnonces[cle];
  etat.pvAnnonces[cle] = n;
  if (avant === undefined || n <= avant || etat.page === "pourvous") return;
  if (await confirmer(t("Claude needs you for something it cannot do itself (an account, a payment, a key…). Open the For you page?"), { titre: t("“{0}”: a task for you", [cle]), ok: t("Open"), annuler: t("Later") })) location.hash = "#pourvous";
}
async function annoncerFins(v) {
  if (v.verrouille || !$("#dialogue").hidden || !$("#assistant").hidden) return;
  const p = (v.projets || []).find((x) => x.fin && !finsAnnoncees.has(x.nom + "|" + x.fin.cle));
  if (!p) return;
  finsAnnoncees.add(p.nom + "|" + p.fin.cle);
  const livre = p.fin.livre || /^DONE/.test(p.fin.message || "");
  const texte = livre ? t("The deliverable and the final report are in the project folder.")
    : /^Maximum duration reached/.test(p.fin.message || "") ? t("It reached the maximum duration (MAX_HOURS in the settings). The work in progress is in the project folder; start it again to go on.")
    : t(p.fin.message || "");
  const ouvrir = await confirmer(texte, { titre: livre ? t("“{0}” is finished", [p.nom]) : t("The pilot of “{0}” has stopped", [p.nom]), ok: t("Open the folder"), annuler: t("Close") });
  await api("fin-vue?p=" + encodeURIComponent(p.nom), "POST", { cle: p.fin.cle }).catch(() => {});
  if (ouvrir) await api("ouvrir?p=" + encodeURIComponent(p.nom), "POST", { quoi: "projet" }).catch(() => {});
}

// ----------------------------------------------------------------------------- "Improve with Claude"
// Under a request text area: Claude rewrites the request (clearer, richer, same intent); the user's
// version is kept and both can be swapped at will. Edits made to either version are kept too.
const versionsDemande = new WeakMap(); // textarea → { mienne, claude, vue: "mienne" | "claude" }
function barreAmeliorer(id, genre = "") {
  const aide = genre === "agent" ? t("Claude rewrites these instructions: clearer, more complete, same intent. You can go back to yours.") : t("Claude rewrites your request: clearer, more complete, same intent. You can go back to yours.");
  return `<div class="ameliorer" data-pour="${id}">
    <button type="button" class="btn btn-petit" data-ameliorer="${id}" data-genre="${genre}" title="${h(aide)}">✨ ${t("Improve with Claude")}</button>
    <button type="button" class="btn btn-petit" data-version="${id}" hidden></button>
    <span class="muet petit" data-etat-ameliorer="${id}"></span>
  </div>`;
}
function majBarreAmeliorer(id) {
  const zone = document.getElementById(id), v = zone && versionsDemande.get(zone);
  const bascule = document.querySelector(`[data-version="${id}"]`), etiquette = document.querySelector(`[data-etat-ameliorer="${id}"]`);
  if (!bascule || !etiquette) return;
  bascule.hidden = !v;
  if (!v) { etiquette.textContent = ""; return; }
  bascule.textContent = v.vue === "claude" ? "↩ " + t("Back to my version") : "✨ " + t("Use Claude's version");
  etiquette.textContent = v.vue === "claude" ? t("Claude's version") : t("Your version");
}
document.addEventListener("input", (ev) => {
  const v = versionsDemande.get(ev.target);
  if (v) v[v.vue] = ev.target.value;
});
document.addEventListener("click", async (ev) => {
  const bAm = ev.target.closest("[data-ameliorer]"), bVe = ev.target.closest("[data-version]");
  if (bVe) {
    const zone = document.getElementById(bVe.dataset.version), v = versionsDemande.get(zone);
    if (!v) return;
    v.vue = v.vue === "claude" ? "mienne" : "claude";
    zone.value = v[v.vue];
    majBarreAmeliorer(zone.id);
    return;
  }
  if (!bAm) return;
  const zone = document.getElementById(bAm.dataset.ameliorer);
  if (!zone) return;
  const agent = bAm.dataset.genre === "agent";
  if (!zone.value.trim()) { toast(agent ? t("Write its instructions first") : t("Write your request first"), "erreur"); zone.focus(); return; }
  const avant = versionsDemande.get(zone);
  const mienne = avant ? avant.mienne : zone.value; // always rewrite from the user's own text
  const etiquette = document.querySelector(`[data-etat-ameliorer="${zone.id}"]`);
  const libelleBouton = bAm.innerHTML;
  bAm.disabled = true; zone.readOnly = true; zone.classList.add("en-attente");
  bAm.textContent = agent ? t("Claude is rewriting the instructions…") : t("Claude is rewriting your request…");
  if (etiquette) etiquette.textContent = t("about 30 seconds");
  try {
    const r = await api("ameliorer", "POST", agent ? { texte: mienne, genre: "agent", nom: ($("#ag-nom") || {}).value || "", description: ($("#ag-description") || {}).value || "" } : { texte: mienne });
    versionsDemande.set(zone, { mienne, claude: r.texte, vue: "claude" });
    zone.value = r.texte;
    toast(agent ? t("Instructions improved: check them, edit them, or go back to yours.") : t("Request improved: check it, edit it, or go back to yours."), "ok");
  } catch (e) {
    if (e.message !== t("Locked")) toast(e.message, "erreur");
  } finally {
    bAm.disabled = false; zone.readOnly = false; zone.classList.remove("en-attente");
    bAm.innerHTML = libelleBouton;
    majBarreAmeliorer(zone.id);
  }
});

// ----------------------------------------------------------------------------- helpers
function fmtTokens(n) { n = Number(n) || 0; return n >= 1e6 ? (n / 1e6).toFixed(1) + " M" : n >= 1000 ? Math.round(n / 1000) + " k" : String(n); }
// "2026-10-16T19:21:07" → "19:21:07" today, or the local date and time otherwise.
function heureMaj(iso) {
  if (!iso) return "—";
  const d = new Date(iso);
  if (isNaN(d)) return iso;
  const lang = document.documentElement.lang || "en";
  return d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString(lang) : d.toLocaleString(lang);
}

function depuis(iso) {
  if (!iso) return "";
  const d = new Date(iso.replace(" ", "T")); if (isNaN(d)) return "";
  const s = Math.max(0, (Date.now() - d.getTime()) / 1000);
  return s < 3600 ? t("{0} min", [Math.floor(s / 60)]) : t("{0} h {1} min", [Math.floor(s / 3600), String(Math.floor((s % 3600) / 60)).padStart(2, "0")]);
}
function phraseEtat(v) {
  const e = v.etat || {};
  if (!v.kit_valide) return { classe: "", titre: t("Tool folder not found"), sous: t("Set it in Options.") };
  if (!v.projet) return { classe: "", titre: v.kit_installe ? t("No project yet") : t("Ready to install"), sous: v.kit_installe ? t("Add a project: its development folder, and what you want.") : t("Follow the steps below: everything happens in this window.") };
  if (!v.actif) {
    if (!v.kit_installe) return { classe: "", titre: t("Ready to install"), sous: t("Follow the steps below: everything happens in this window.") };
    if (v.demande_vide) return { classe: "", titre: t("Waiting for your first request"), sous: t("Describe what you want in Requests, then start.") };
    return { classe: "", titre: t("Pilot stopped"), sous: e.dernier_commit ? t("Last saved work: {0}", [e.dernier_commit]) : t("Click Start to resume.") };
  }
  switch (e.phase) {
    // the pilot has just stopped (its last heartbeat is still recent): same wording as a stopped pilot
    case "arrete": return { classe: "", titre: t("Pilot stopped"), sous: e.dernier_commit ? t("Last saved work: {0}", [e.dernier_commit]) : t("Click Start to resume.") };
    case "travail": return { classe: "travaille", titre: e.ouvriers ? t("{0} workers in parallel: {1}", [e.ouvriers.split(/\s+/).length, e.ouvriers.split(/\s+/).join(", ")]) : /^Merging/.test(e.etat || "") ? t("The lead merges the workers' branches") : e.jalon_en_cours ? t("Claude is working on {0}", [titreJalon(e.jalon_en_cours)]) : t("Claude is working"), sous: [e.debut_cycle && t("for {0}", [depuis(e.debut_cycle)]), t("cycle {0}", [e.cycle]), e.template, v.contexte && t("context {0} tokens", [fmtTokens(v.contexte)])].filter(Boolean).join(" · ") };
    case "quota": return { classe: "quota", titre: t("Usage limit reached, Claude is waiting"), sous: t("Resumes {0} · cycle {1}", [e.reprise_prevue || t("as soon as possible"), e.cycle]) };
    case "attente_vous": return { classe: "quota", titre: t("Claude is waiting for you"), sous: t("{0} task(s) on the For you page: the pilot resumes as soon as you answer one.", [v.pour_vous || 0]) };
    case "entre_cycles": return { classe: "entre_cycles", titre: t("Between two cycles"), sous: t("Cycle {0} is done, the next one starts with a fresh context.", [e.cycle]) };
    case "erreur": return { classe: "erreur", titre: t("Error, retry scheduled"), sous: e.etat };
    case "connexion": return { classe: "connexion", titre: t("Connection problem"), sous: t("Check the token in Settings, or go through Setup & health.") };
    case "termine": return { classe: "", titre: t("Project finished"), sous: t("The deliverable and the final report are in the project folder.") };
    default: return { classe: "", titre: e.etat ? t(e.etat) : t("Pilot started"), sous: t("cycle {0}", [e.cycle || 0]) };
  }
}

// ----------------------------------------------------------------------------- the window's own title bar
// In Athanor's Windows window only (Go says so with window.__athanorCadre, see cadre_windows.go): the page
// draws the bar, Windows moves, resizes and snaps the window. The three buttons are under invisible
// windows of Go's that answer Windows (snap layouts on "maximize"): Go sends their hover and pressed
// states. Elsewhere (a browser), nothing changes.
const cadre = { actif: false, etat: {} };
const CLIC_BARRE = "button, a, input, select, textarea";

function rendreBarre(v) {
  if (!cadre.actif) return;
  const p = v.projet;
  const projet = p ? `<span class="dot ${classePhase(p)}"></span><span>${h(p.nom)}</span>` : "";
  const b = v.bureau || {}, act = [];
  if (b.disponible && b.ecran) act.push(`<button type="button" class="fb-action" data-ecran title="${h(t("Go and see what Claude is showing (windows on the “Claude” desktop)"))}">${t("View Claude's screen")}</button>`);
  else {
    if (b.disponible) act.push(`<button type="button" class="fb-action" data-bureau="voir" title="${h(t("Go and see what Claude is showing (windows on the “Claude” desktop)"))}">${t("View Claude's desktop")}</button>`);
    if (b.isole && !v.natif) act.push(`<button type="button" class="fb-action" data-ecran title="${h(t("The Linux windows Claude opens (a game under test, Godot, a browser) go to a private screen: never on yours, never taking the focus."))}">${t("Claude's Linux screen")}</button>`);
  }
  const html = act.join("");
  if ($("#fb-projet").innerHTML !== projet) $("#fb-projet").innerHTML = projet;
  if ($("#fb-actions").dataset.html !== html) { $("#fb-actions").dataset.html = html; $("#fb-actions").innerHTML = html; }
}

function peindreCadre() {
  const e = cadre.etat;
  document.documentElement.classList.toggle("fenetre-inactive", e.actif === false);
  const max = $('[data-fenetre="agrandir"]');
  max.textContent = e.max ? "\uE923" : "\uE922";
  const noms = { reduire: t("Minimize"), agrandir: e.max ? t("Restore") : t("Maximize"), fermer: t("Close") };
  document.querySelectorAll("[data-fenetre]").forEach((b) => {
    const n = b.dataset.fenetre;
    b.classList.toggle("survol", e.survol === n);
    b.classList.toggle("appui", e.appui === n);
    b.setAttribute("aria-label", noms[n]); b.title = noms[n];
  });
}

// Go's state: hover, pressed, maximized, focus; {cadre: false} gives the bar back to Windows
window.athanorCadre = (e) => {
  if (!e) return;
  if (e.cadre === false) { cadre.actif = false; document.documentElement.classList.remove("cadre"); $("#fenetre-barre").hidden = true; if (etat.vue) rendreBandeau(etat.vue); return; }
  cadre.etat = e; peindreCadre();
};

function envoyerZones() {
  if (!cadre.actif) return;
  const z = (n) => { const r = $(`[data-fenetre="${n}"]`).getBoundingClientRect(); return [window.innerWidth - r.left, r.top, r.width, r.height]; };
  window.athanorZones({ largeur: window.innerWidth, min: z("reduire"), max: z("agrandir"), fermer: z("fermer") }).catch(() => {}); // Go answers with athanorCadre
}

function initCadre() {
  if (!window.__athanorCadre || typeof window.athanorFenetre !== "function" || typeof window.athanorZones !== "function") return;
  cadre.actif = true;
  document.documentElement.classList.add("cadre");
  const barre = $("#fenetre-barre");
  barre.hidden = false;
  peindreCadre();
  envoyerZones();
  let attente = 0;
  window.addEventListener("resize", () => { cancelAnimationFrame(attente); attente = requestAnimationFrame(envoyerZones); });
  // dragging the bar moves the window (from 3 pixels on, so that a double click still maximizes)
  let depart = null;
  barre.addEventListener("mousedown", (e) => {
    const libre = !e.target.closest(CLIC_BARRE);
    if (libre) e.preventDefault(); // like Windows' bar: the field being typed in keeps the focus
    depart = e.button === 0 && e.detail === 1 && libre ? [e.screenX, e.screenY] : null;
  });
  barre.addEventListener("dragstart", (e) => e.preventDefault());
  window.addEventListener("mousemove", (e) => {
    if (!depart) return;
    if (!(e.buttons & 1)) { depart = null; return; }
    if (Math.abs(e.screenX - depart[0]) + Math.abs(e.screenY - depart[1]) < 3) return;
    depart = null; window.athanorFenetre("deplacer");
  });
  window.addEventListener("mouseup", () => { depart = null; });
  barre.addEventListener("dblclick", (e) => { if (!e.target.closest(CLIC_BARRE)) window.athanorFenetre("agrandir"); });
  barre.addEventListener("contextmenu", (e) => { e.preventDefault(); if (!e.target.closest(CLIC_BARRE)) window.athanorFenetre("menu"); });
  barre.addEventListener("click", async (e) => {
    const b = e.target.closest("button"); if (!b) return;
    if (b.dataset.fenetre) return window.athanorFenetre(b.dataset.fenetre); // the keyboard (the mouse goes through Go)
    if (b.dataset.bureau) return action(api("bureau/" + b.dataset.bureau, "POST"));
    if (b.hasAttribute("data-ecran")) return ouvrirEcran();
  });
}
initCadre();

// Closing Athanor's window while native pilots run (fermeture_windows.go): the question in our own dialog.
// Go waits for the answer: they go on in the background, they stop, or the window stays open.
window.athanorFermeture = (noms) => {
  if (typeof window.athanorFermer !== "function") return;
  const fond = $("#fermeture");
  const liste = Array.isArray(noms) ? noms.map(String) : [];
  $("#fermeture-titre").textContent = t("Pilots are still running");
  // locked: no project name (nothing of the projects shows while locked)
  $("#fermeture-texte").textContent = etat.verrouille || !liste.length ? t("Pilots are running in native Windows mode.") : t("Pilots are running in native Windows mode: {0}.", [liste.join(", ")]);
  $("#fermeture-aide").textContent = t("In the background, they go on without the window: reopen Athanor to follow them or stop them. Stopping them interrupts the cycle under way.");
  const libelles = { annuler: t("Cancel"), arreter: t("Stop them"), continuer: t("Keep them running") };
  fond.querySelectorAll("[data-fermer]").forEach((b) => { b.textContent = libelles[b.dataset.fermer]; });
  const fin = (choix) => {
    fond.hidden = true;
    fond.onclick = null;
    document.removeEventListener("keydown", clavier, true);
    window.athanorFermer(choix);
  };
  const clavier = (e) => {
    if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); fin("annuler"); }
    else if (e.key === "Enter" && !document.activeElement.closest("[data-fermer]")) { e.preventDefault(); e.stopPropagation(); fin("continuer"); }
  };
  document.removeEventListener("keydown", fond._clavier || (() => {}), true);
  fond._clavier = clavier;
  document.addEventListener("keydown", clavier, true);
  fond.onclick = (e) => { const b = e.target.closest("[data-fermer]"); if (b) fin(b.dataset.fermer); };
  fond.hidden = false;
  $('#fermeture [data-fermer="continuer"]').focus();
  window.athanorFermer("affiche");
};

// ----------------------------------------------------------------------------- shell: header, sidebar, project picker
function rendreBandeau(v) {
  const p = phraseEtat(v);
  const boutons = [];
  if (v.kit_valide && v.projet) {
    if (v.actif) {
      boutons.push(`<button class="btn" data-act="arreter-apres-cycle" title="${h(t("Finishes the current milestone, saves it, then stops. Nothing is lost."))}">${t("Stop after cycle")}</button>`);
      boutons.push(`<button class="btn btn-danger" data-act="arreter" title="${h(t("Cuts the current cycle: unsaved work will be redone."))}">${t("Stop now")}</button>`);
    } else {
      // right after a click, the pilot takes a few seconds to show up: no second start
      const enDemarrage = (etat.demarrage[v.projet.nom] || 0) > Date.now();
      boutons.push(`<button class="btn btn-primaire" data-act="demarrer" ${enDemarrage ? "disabled" : ""}>${enDemarrage ? t("Starting…") : t("Start")}</button>`);
    }
    if (v.bureau && v.bureau.disponible && !cadre.actif) boutons.push(v.bureau.ecran
      ? `<button class="btn" data-ecran title="${h(t("Go and see what Claude is showing (windows on the “Claude” desktop)"))}">${t("View Claude's desktop")}</button>`
      : `<button class="btn" data-bureau="voir" title="${h(t("Go and see what Claude is showing (windows on the “Claude” desktop)"))}">${t("View Claude's desktop")}</button>`);
  }
  rendreBarre(v);
  const nomProjet = v.projet ? `<div class="muet petit">${h(v.projet.nom)} · ${h(v.projet.dossier)}</div>` : "";
  const nbActifs = (v.projets || []).filter((x) => x.actif).length;
  $("#bandeau").innerHTML = `<div class="etat-phrase">${nomProjet}<h1 title="${h(p.titre)}"><span class="point ${p.classe}"></span><span>${h(p.titre)}</span></h1><div class="sous" title="${h(p.sous)}">${h(p.sous)}</div></div><div class="actions">${boutons.join("")}</div>`;
  $("#nav-sous").textContent = nbActifs ? t("{0} project(s) running", [nbActifs]) : t("idle");
  $("#nav-kit").textContent = v.kit || "";
  const sel = $("#sel-projet");
  sel.setAttribute("aria-expanded", etat.selecteurOuvert ? "true" : "false");
  sel.innerHTML = v.projet ? `<span class="dot ${classePhase(v.projet)}"></span><span class="nom">${h(v.projet.nom)}</span><span class="chevron">▾</span>` : `<span class="nom muet">${t("Choose a project")}</span><span class="chevron">▾</span>`;
  const lien = $('#nav-liens a[data-page="demandes"]');
  lien.innerHTML = t("Requests") + (v.attente ? ` <span class="pastille">${v.attente}</span>` : "");
  const lienPv = $('#nav-liens a[data-page="pourvous"]');
  if (lienPv) lienPv.innerHTML = t("For you") + (v.pour_vous ? ` <span class="pastille pastille-alerte">${v.pour_vous}</span>` : "");
  $("#btn-verrou").hidden = !v.mot_de_passe;
  if (v.projet && etat.projet !== v.projet.nom) etat.projet = v.projet.nom;
  rendreSelecteur(v);
}

function rendreSelecteur(v) {
  const m = $("#menu-projets"); if (!m) return;
  m.hidden = !etat.selecteurOuvert;
  if (!etat.selecteurOuvert) return;
  const q = (etat.rechercheProjet || "").toLowerCase();
  const liste = (v.projets || []).filter((x) => !x.archive && (!q || x.nom.toLowerCase().includes(q) || x.dossier.toLowerCase().includes(q)));
  const archives = (v.projets || []).filter((x) => x.archive).length;
  const avaitFocus = document.activeElement && document.activeElement.id === "sel-recherche";
  const pos = avaitFocus ? document.activeElement.selectionStart : 0;
  m.innerHTML = `<input type="text" id="sel-recherche" placeholder="${h(t("Search a project…"))}" value="${h(etat.rechercheProjet || "")}">
    <div class="menu-liste">${liste.map((x) => `<a href="#suivi" data-projet="${h(x.nom)}" class="${x.courant ? "actif" : ""}"><span class="dot ${classePhase(x)}"></span><span class="nom">${h(x.nom)}</span><span class="muet petit">${x.actif ? t(x.phase === "quota" || x.phase === "attente_vous" ? "waiting" : "running") : t("stopped")}${x.attente ? " · " + t("{0} queued", [x.attente]) : ""}${x.pour_vous ? " · " + t("{0} for you", [x.pour_vous]) : ""}</span></a>`).join("") || `<div class="vide">${t("No project.")}</div>`}</div>
    ${v.projet ? `<a href="#projet" class="menu-nouveau menu-gerer">⚙ ${t("Manage this project")}</a>` : ""}
    <a href="#nouveau" class="menu-nouveau">+ ${t("New project")}</a>
    ${archives ? `<a href="#archives" class="menu-nouveau menu-archives">🗄 ${t("Archived projects ({0})", [archives])}</a>` : ""}`;
  const r = $("#sel-recherche"); if (r) { r.focus(); r.setSelectionRange(pos, pos); }
}

$("#bandeau").addEventListener("click", async (ev) => {
  const b = ev.target.closest("button"); if (!b) return;
  if (b.dataset.act === "demarrer") return demarrerPilote(b);
  if (b.dataset.act) { b.disabled = true; await action(api("pilote/" + b.dataset.act, "POST")); setTimeout(rafraichir, 800); }
  if (b.dataset.bureau) await action(api("bureau/" + b.dataset.bureau, "POST"));
  if (b.hasAttribute("data-ecran")) ouvrirEcran();
});

// Claude's screen on the Linux side (Linux, or Linux programs under WSL): a picture refreshed every 2 s
// while the window is open.
let minuterieEcran = null;
// one picture at a time: the next one is asked 2 s after the previous one arrived (a capture can take longer)
async function majEcran() {
  minuterieEcran = null;
  if ($("#ecran").hidden) return;
  try {
    const r = await api("ecran");
    if ($("#ecran").hidden) return;
    $("#ecran-img").src = "data:image/png;base64," + r.png;
    $("#ecran-img").hidden = false; $("#ecran-msg").hidden = true;
  } catch (e) {
    $("#ecran-img").hidden = true; $("#ecran-msg").hidden = false; $("#ecran-msg").textContent = e.message;
  }
  if (!$("#ecran").hidden && !minuterieEcran) minuterieEcran = setTimeout(majEcran, 2000);
}
function ouvrirEcran() {
  $("#ecran-titre").textContent = t("Claude's screen");
  $("#ecran-fermer").textContent = t("Close");
  $("#ecran-msg").textContent = t("Loading…"); $("#ecran-msg").hidden = false; $("#ecran-img").hidden = true;
  $("#ecran").hidden = false;
  clearTimeout(minuterieEcran); majEcran();
}
function fermerEcran() { $("#ecran").hidden = true; clearTimeout(minuterieEcran); minuterieEcran = null; }
$("#ecran-fermer").addEventListener("click", fermerEcran);
$("#ecran").addEventListener("click", (ev) => { if (ev.target.id === "ecran") fermerEcran(); });
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && !$("#ecran").hidden) fermerEcran(); });
function fermerSelecteur() { etat.selecteurOuvert = false; $("#menu-projets").hidden = true; $("#sel-projet").setAttribute("aria-expanded", "false"); }
$("#sel-projet").addEventListener("click", () => { etat.selecteurOuvert = !etat.selecteurOuvert; etat.rechercheProjet = ""; $("#sel-projet").setAttribute("aria-expanded", etat.selecteurOuvert ? "true" : "false"); if (etat.vue) rendreSelecteur(etat.vue); });
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && etat.selecteurOuvert) fermerSelecteur(); });
$("#menu-projets").addEventListener("input", (ev) => { if (ev.target.id === "sel-recherche") { etat.rechercheProjet = ev.target.value; rendreSelecteur(etat.vue); } });
$("#menu-projets").addEventListener("click", async (ev) => {
  const a = ev.target.closest("a"); if (!a) return;
  fermerSelecteur();
  if (!a.dataset.projet) return; // "+ New project": normal navigation
  ev.preventDefault();
  etat.projet = a.dataset.projet; etat.evenements = []; etat.dernierJournal = 0; etat.ouverts.clear();
  await action(api("projets/choisir", "POST", { nom: etat.projet }));
  if (["sante", "reglages", "options", "nouveau"].includes(etat.page)) location.hash = "#suivi"; else { await rafraichir(); rendrePage(); }
});
document.addEventListener("click", (ev) => { if (etat.selecteurOuvert && !ev.target.closest("#sel-projet, #menu-projets")) fermerSelecteur(); });

function traduireNav() {
  const libelles = { suivi: t("Overview"), equipe: t("Team"), demandes: t("Requests"), pourvous: t("For you"), journal: t("Log"), projet: t("Manage"), chat: t("Chat"), extensions: t("Skills & MCP"), sante: t("Setup & health"), reglages: t("Settings"), options: t("Options") };
  document.querySelectorAll("#nav-liens a, #nav-outil a").forEach((a) => { if (libelles[a.dataset.page]) a.textContent = libelles[a.dataset.page]; });
  $("#nav-titre-projet").textContent = t("Project");
  $("#nav-titre-outil").textContent = t("Tool");
  $("#btn-verrou").textContent = "🔒 " + t("Lock");
  $("#verrou-titre").textContent = t("Application locked");
  $("#verrou-mdp").placeholder = t("Password");
  $("#verrou-bouton").textContent = t("Unlock");
}

function naviguer() {
  let page = (location.hash || "#suivi").slice(1);
  // the application's options are a tab of the settings now (older links, and the texts that say "Options")
  if (page === "options") { etat.ongletReglages = "options"; history.replaceState(null, "", "#reglages"); page = "reglages"; }
  etat.page = page;
  document.querySelectorAll("#nav-liens a, #nav-outil a").forEach((a) => a.classList.toggle("actif", a.dataset.page === page));
  rendrePage();
}
window.addEventListener("hashchange", naviguer);

// ----------------------------------------------------------------------------- pages
const pages = {};

// --- first launch: a three-step wizard (language, appearance, what comes next) in an overlay
const ETAPES_ASSISTANT = 3;
function rendreAssistant() {
  const el = $("#assistant"); const n = etat.assistant;
  el.hidden = n < 0; if (n < 0) return;
  const barre = Array.from({ length: ETAPES_ASSISTANT }, (_, i) => `<i class="${i <= n ? "faite" : ""}"></i>`).join("");
  let corps = "", titre = "";
  if (n === 0) { titre = t("Welcome"); corps = `<p class="muet">${t("Athanor builds whole projects with Claude Code while you do something else. First, pick your language.")}</p>${rendreLangues()}`; }
  if (n === 1) { titre = t("Make it yours"); corps = `<p class="muet">${t("Pick a palette and an accent color. You can change them any time in Options.")}</p>${rendreApparence()}`; }
  if (n === 2) { titre = t("What comes next"); corps = `<p class="muet">${t("Five steps, all from this window. Then you can let it run for days, on several projects at once.")}</p>
    <div class="recap">${[t("Install the tooling"), t("Check and connect"), t("Add a project"), t("Say what you want"), t("Start")].map((x, i) => `<div><b>${i + 1}</b><span>${x}</span></div>`).join("")}</div>
    <p class="muet petit">${t("The overview page keeps these steps until the first project runs.")}</p>`; }
  el.innerHTML = `<div class="assistant-carte">
    <div class="assistant-tete"><svg width="30" height="30" viewBox="0 0 28 28" aria-hidden="true"><circle cx="14" cy="14" r="12" fill="none" stroke="currentColor" stroke-width="2"/><path d="M9 14l3.5 3.5L19 11" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"/></svg><h1>${titre}</h1></div>
    <div class="assistant-etapes">${barre}</div>
    <div>${corps}</div>
    <div class="assistant-pied">${n > 0 ? `<button class="btn" data-assistant="retour">${t("Back")}</button>` : ""}<span class="espace"></span><span class="muet petit">${t("Step {0} of {1}", [n + 1, ETAPES_ASSISTANT])}</span>
      <button class="btn btn-primaire" data-assistant="${n === ETAPES_ASSISTANT - 1 ? "fin" : "suite"}">${n === ETAPES_ASSISTANT - 1 ? t("Let's go") : t("Continue")}</button></div>
  </div>`;
}
function rendreLangues() { return `<div class="langues">${LANGUES.map((l) => `<button class="btn ${etat.langue === l.code ? "btn-primaire" : ""}" data-langue="${l.code}">${l.nom}</button>`).join("")}</div>`; }
function rendreApparence() {
  return `<div class="palettes">${THEMES.map((th) => `<button class="palette ${(etat.theme || "ardoise") === th.code ? "actif" : ""}" data-theme="${th.code}" style="--ap-bg:${th.ap[0]};--ap-panel:${th.ap[1]};--ap-line:${th.ap[2]};--ap-ink:${th.ap[3]};--ap-muted:${th.ap[4]}"><span class="apercu"><i class="ap-nav"></i><span class="ap-page"><i></i><i></i></span></span><span class="nom">${t(th.nom)}</span></button>`).join("")}</div>
  <div class="accents mt-3">${ACCENTS.map((a) => `<button class="accent ${(etat.accent || "sarcelle") === a.code ? "actif" : ""}" data-accent="${a.code}" style="--c:${a.c}" title="${t(a.nom)}" aria-label="${t(a.nom)}"></button>`).join("")}</div>`;
}
$("#assistant").addEventListener("click", async (ev) => {
  const b = ev.target.closest("button"); if (!b) return;
  if (b.dataset.langue) { await changerLangue(b.dataset.langue); return rendreAssistant(); }
  if (b.dataset.theme) { await changerApparence(b.dataset.theme, etat.accent); return rendreAssistant(); }
  if (b.dataset.accent) { await changerApparence(etat.theme, b.dataset.accent); return rendreAssistant(); }
  if (b.dataset.assistant === "retour") { etat.assistant--; return rendreAssistant(); }
  if (b.dataset.assistant === "suite") { etat.assistant++; return rendreAssistant(); }
  if (b.dataset.assistant === "fin") { await action(api("preferences", "POST", { bienvenue_vue: true })); etat.assistant = -1; rendreAssistant(); location.hash = "#suivi"; await rendrePage(); demarrerVisite(); }
});

// --- the guided tour, right after the first-launch wizard (and from Options): a bubble shows each place
// where things happen, on its own page: the mode, the installation and the check, a new project, Start.
const VISITE = [
  { page: "reglages", onglet: "options", cibles: ["#op-natif"], windows: true, titre: "How Claude works on this PC",
    texte: "Native Windows is selected: nothing else to install, Claude runs directly on Windows. Choose WSL here if you want Claude in a sandbox (it installs Linux once)." },
  { page: "sante", cibles: ["[data-onglet-sante]", "[data-installer-mode]", "[data-installer]"], titre: "Install, then check",
    texte: "One tab per way Claude can run on this computer, the current one marked: “Install everything” installs what it needs. Then “Check everything”: each problem has its own Repair or Ask Claude button." },
  { page: "nouveau", cibles: ["#np-nom"], titre: "Your project",
    texte: "A name, the development folder (new or existing, anywhere on the PC), and what you want. A folder made with Athanor on another PC carries on where it was." },
  { page: "suivi", cibles: ["#bandeau .actions .btn", "#sel-projet"], titre: "Start",
    texte: "Start launches the pilot: Claude frames, plans milestones, builds and checks them, cycle after cycle. You follow everything here." },
];
function etapesVisite() { return VISITE.filter((e) => !e.windows || (etat.vue || {}).windows); }
function demarrerVisite() { etat.visite = 0; montrerVisite(); }
function finirVisite() {
  etat.visite = -1; $("#visite").hidden = true;
  document.querySelectorAll(".visite-cible").forEach((x) => x.classList.remove("visite-cible"));
}
async function montrerVisite() {
  const etapes = etapesVisite(), n = etat.visite;
  if (n < 0 || n >= etapes.length) return finirVisite();
  const e = etapes[n];
  if (e.onglet) etat.ongletReglages = e.onglet;
  if (location.hash !== "#" + e.page) location.hash = "#" + e.page; else await rendrePage();
  let cible = null;
  for (let i = 0; i < 40 && !cible; i++) { // the page renders asynchronously
    await new Promise((r) => setTimeout(r, 100));
    for (const c of e.cibles) { const x = document.querySelector(c); if (x && x.offsetParent !== null) { cible = x; break; } }
  }
  if (n !== etat.visite) return;
  document.querySelectorAll(".visite-cible").forEach((x) => x.classList.remove("visite-cible"));
  const bulle = $("#visite");
  bulle.innerHTML = `<div class="visite-titre">${h(t(e.titre))}</div><p>${h(t(e.texte))}</p>
    <div class="visite-pied"><span class="muet petit">${t("Step {0} of {1}", [n + 1, etapes.length])}</span><span class="espace"></span>
      <button class="btn btn-petit" data-visite="passer">${t("Skip the tour")}</button>
      ${n > 0 ? `<button class="btn btn-petit" data-visite="retour">${t("Back")}</button>` : ""}
      <button class="btn btn-petit btn-primaire" data-visite="suite">${n === etapes.length - 1 ? t("Finish") : t("Next")}</button></div>`;
  bulle.hidden = false;
  if (cible) {
    cible.classList.add("visite-cible");
    cible.scrollIntoView({ block: "center", behavior: "smooth" });
    await new Promise((r) => setTimeout(r, 350));
    const z = cible.getBoundingClientRect(), l = bulle.offsetWidth, hb = bulle.offsetHeight;
    let haut = z.bottom + 12; if (haut + hb > window.innerHeight - 8) haut = Math.max(8, z.top - hb - 12);
    bulle.style.top = haut + "px";
    bulle.style.left = Math.max(8, Math.min(z.left, window.innerWidth - l - 8)) + "px";
  } else { bulle.style.top = "80px"; bulle.style.left = Math.max(8, (window.innerWidth - bulle.offsetWidth) / 2) + "px"; }
  bulle.querySelector('[data-visite="suite"]').focus();
}
$("#visite").addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-visite]"); if (!b) return;
  if (b.dataset.visite === "passer") return finirVisite();
  etat.visite += b.dataset.visite === "retour" ? -1 : 1;
  montrerVisite();
});
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && etat.visite >= 0 && !$("#visite").hidden) finirVisite(); });

// --- working mode of the project (serial lead, or parallel workers + merge)
async function rendreMode() {
  const m = await api("mode").catch(() => null); if (!m) return "";
  // one line: the long explanations are in the tooltips
  const long = t("the lead writes a fine-grained plan with dependencies; workers build independent milestones at the same time, each in its own git working copy; the pilot merges each one as soon as it finishes and starts the next, and the lead verifies and fixes. Much faster; uses more quota (one session per worker).");
  return `<div class="bloc mode-ligne"><div class="ligne m0">
    <h2 class="m0">${t("Working mode")}</h2>
    <label class="interrupteur m0" title="${h(long)}"><input type="checkbox" id="md-parallele" ${m.parallele ? "checked" : ""}><span><b>${t("Parallel mode")}</b></span></label>
    <span class="muet petit" title="${h(long)}">${m.parallele ? t("several workers at once: faster, uses more quota") : t("one milestone at a time; parallel: several workers at once, faster, more quota")}</span>
    ${m.parallele ? `<span class="espace"></span><span class="petit" title="${h(t("Starts with 2, adds one each time a worker delivers, and starts no more once the quota is met. Applies from the next cycle."))}">${t("Workers at most")}</span><div class="choix m0">${[2, 3, 4, 6, 8, 10].map((n) => `<button type="button" data-max="${n}" class="${m.parallele_max === n ? "actif" : ""}">${n}</button>`).join("")}</div>` : ""}
  </div></div>`;
}
async function enregistrerMode(parallele, max) {
  const r = await action(api("mode", "POST", { parallele, parallele_max: max }));
  if (r) rendrePage();
}

// --- chat with Claude about the project (api_chat.go): questions always; changes while the pilot is
// stopped (Athanor commits them); while it runs, Claude writes the change as a note for the pilot
const chat = { modele: (() => { try { return localStorage.getItem("chatModele") ?? "sonnet"; } catch (e) { return "sonnet"; } })(), questionSeule: false, signature: "", modeles: null, envoi: false };

function mdChat(s) {
  return String(s || "").split(/```/).map((bloc, i) => {
    if (i % 2) return `<pre class="chat-code">${h(bloc.replace(/^[a-zA-Z0-9_+-]*\n/, ""))}</pre>`;
    return h(bloc).replace(/`([^`\n]+)`/g, "<code>$1</code>").replace(/\*\*([^*\n]+)\*\*/g, "<b>$1</b>")
      .replace(/^(\s*)[-*] (.*)$/gm, "$1• $2").replace(/^#{1,4} (.*)$/gm, "<b>$1</b>").replace(/\n/g, "<br>");
  }).join("");
}

function messageChat(m, i) {
  const quand = m.quand ? new Date(m.quand).toLocaleTimeString(document.documentElement.lang || "en", { hour: "2-digit", minute: "2-digit" }) : "";
  if (m.role === "vous") return `<div class="chat-msg chat-vous"><div class="chat-bulle">${mdChat(m.texte)}</div><div class="chat-meta">${h(quand)}${m.mode === "modif" ? " · " + t("may change the files") : ""}</div></div>`;
  if (m.role === "erreur") return `<div class="chat-msg chat-claude"><div class="chat-bulle chat-erreur">${h(t(m.texte))}</div></div>`;
  const note = (m.texte.match(/<note>([\s\S]*?)<\/note>/) || [])[1];
  const texte = m.texte.replace(/<note>[\s\S]*?<\/note>/g, "").trim();
  const actions = (m.outils || []).length ? `<details class="chat-actions"><summary>${t("{0} action(s)", [m.outils.length])}</summary>${m.outils.map((o) => `<div>${h(t(o))}</div>`).join("")}</details>` : "";
  return `<div class="chat-msg chat-claude"><div class="chat-bulle">${mdChat(texte)}
    ${note ? `<div class="chat-note"><div class="muet petit">${t("Note for the pilot")}</div><div>${mdChat(note.trim())}</div>
      <div class="ligne m0 mt-1">${m.note ? `<span class="ok petit">✔ ${t("Sent to the pilot")}</span>` : `<button type="button" class="btn btn-petit btn-primaire" data-chat-note="${i}">${t("Send to the pilot")}</button>`}</div></div>` : ""}
    ${actions}</div>
    <div class="chat-meta">${h(quand)}${m.modele ? " · " + h(m.modele) : ""}${m.cout ? " · " + Number(m.cout).toFixed(3) + " $" : ""}${m.commit ? ` · <span class="ok">${t("committed {0}", [h(m.commit)])}</span>` : ""}</div></div>`;
}

function filChat(d) {
  const msgs = d.messages || [];
  let html = msgs.length ? msgs.map(messageChat).join("") : `<div class="vide">${t("Ask anything about the project: how a part works, where something is, what the pilot did, why a milestone failed. With the pilot stopped, Claude can also change files.")}</div>`;
  if (d.en_cours) {
    const p = d.partiel || {};
    const textes = p.textes || [], outils = p.outils || [];
    html += `<div class="chat-msg chat-claude"><div class="chat-bulle chat-attente">${textes.length ? mdChat(textes[textes.length - 1]) : ""}
      <div class="muet petit"><span class="chat-points"><i></i><i></i><i></i></span> ${outils.length ? h(t(outils[outils.length - 1])) : t("Claude is thinking…")}</div></div></div>`;
  }
  return html;
}

async function majChat(force) {
  if (etat.page !== "chat") return;
  const d = await api("chat").catch(() => null); if (!d) return;
  const sig = JSON.stringify([d.messages.length, d.en_cours, d.partiel, d.pilote_actif, (d.messages[d.messages.length - 1] || {}).note]);
  if (!force && sig === chat.signature) return;
  chat.signature = sig;
  const fil = $("#chat-fil"); if (!fil) return;
  const enBas = fil.scrollHeight - fil.scrollTop - fil.clientHeight < 80;
  fil.innerHTML = filChat(d);
  if (enBas || force) fil.scrollTop = fil.scrollHeight;
  const b = $("#chat-envoyer"), s = $("#chat-stop"), m = $("#chat-mode");
  if (b) b.hidden = d.en_cours;
  if (s) s.hidden = !d.en_cours;
  if (m) m.innerHTML = modeChat(d.pilote_actif);
}

function modeChat(piloteActif) {
  if (piloteActif) return `<span class="attention">●</span> ${t("The pilot is running: Claude only reads and answers. A change becomes a note for the pilot, sent with one click.")}`;
  return `<span class="ok">●</span> ${t("The pilot is stopped: Claude can change the project's files (no commands), and Athanor commits them.")}
    <label class="interrupteur m0 petit"><input type="checkbox" id="chat-question" ${chat.questionSeule ? "checked" : ""}><span>${t("question only")}</span></label>`;
}

pages.chat = async () => {
  const [d, mod] = await Promise.all([api("chat"), chat.modeles ? Promise.resolve({ modeles: chat.modeles }) : api("modeles").catch(() => ({ modeles: [] }))]);
  chat.modeles = mod.modeles || [];
  chat.signature = "";
  const modeles = ["", ...chat.modeles];
  if (!modeles.includes(chat.modele)) modeles.push(chat.modele);
  setTimeout(() => majChat(true), 0);
  return `<div class="bloc chat">
    <div class="ligne m0 chat-tete"><h2 class="m0">${t("Chat with Claude")}</h2><span class="muet petit croit" id="chat-mode">${modeChat(d.pilote_actif)}</span>
      <button type="button" class="btn btn-petit" id="chat-nouveau">${t("New conversation")}</button></div>
    <div class="chat-fil" id="chat-fil"></div>
    <div class="chat-saisie">
      <textarea id="chat-texte" rows="3" placeholder="${h(t("Your question or the change you want… (Enter sends, Shift+Enter: new line)"))}"></textarea>
      <div class="ligne m0 mt-1"><span class="petit muet">${t("Model")}</span><div class="choix m0" id="chat-modeles">${modeles.map((m) => `<button type="button" data-chat-modele="${h(m)}" class="${chat.modele === m ? "actif" : ""}">${h(m || t("account default"))}</button>`).join("")}</div>
        <span class="espace"></span>
        <button type="button" class="btn" id="chat-stop" hidden>${t("Stop")}</button>
        <button type="button" class="btn btn-primaire" id="chat-envoyer">${t("Send")}</button></div>
    </div>
  </div>`;
};

async function action_quota(fenetre, seuil, quoi) {
  document.activeElement.blur();
  const r = await action(api("quota/arret", "POST", { fenetre, seuil, action: quoi }));
  if (r) { await rafraichir(); rendrePage(); }
}

async function envoyerChat() {
  const zone = $("#chat-texte"); if (!zone || chat.envoi) return;
  const message = zone.value.trim(); if (!message) return;
  chat.envoi = true;
  const r = await action(api("chat", "POST", { message, modele: chat.modele, mode: chat.questionSeule ? "question" : "" }));
  chat.envoi = false;
  if (r) { zone.value = ""; await majChat(true); }
}

// --- the subscription's quota, as Claude Code last reported it (rate_limit_event of the newest cycle log)
function quandLisible(sec) {
  const d = new Date(sec * 1000), lang = document.documentElement.lang || "en";
  const heure = d.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" });
  const jours = Math.round((new Date(d.toDateString()) - new Date(new Date().toDateString())) / 86400000);
  if (jours === 0) return heure;
  if (jours === 1) return t("tomorrow {0}", [heure]);
  return d.toLocaleDateString(lang, { weekday: "long" }) + " " + heure;
}
function dans(sec) {
  const m = Math.max(0, Math.round((sec * 1000 - Date.now()) / 60000));
  return m < 60 ? t("in {0} min", [m]) : m < 48 * 60 ? t("in {0} h {1}", [Math.floor(m / 60), String(m % 60).padStart(2, "0")]) : t("in {0} days", [Math.round(m / 1440)]);
}
function nomFenetre(n) {
  if (n === "five_hour") return t("Current session (5 h)");
  if (n === "seven_day") return t("This week, all models");
  const m = n.match(/^seven_day_(.+)$/);
  if (m) return t("This week, {0}", [m[1].replace(/_/g, " ").replace(/^./, (c) => c.toUpperCase())]);
  return n.replace(/_/g, " ");
}
// automatic stop of a window: the rule key ("modele" = every model's own week) and its two lists
function regleFenetre(n) { return n === "five_hour" || n === "seven_day" ? n : "modele"; }
function arretAuto(nom, regles) {
  const cle = regleFenetre(nom), r = (regles || {})[cle] || { seuil: 0, action: "apres" };
  const seuils = [0, 50, 60, 70, 75, 80, 85, 90, 95];
  if (r.seuil && !seuils.includes(r.seuil)) seuils.push(r.seuil);
  const actions = [["apres", t("stop after the cycle")], ["maintenant", t("stop now")], ["pause", t("pause until the reset")]];
  return `<div class="quota-arret petit"><span class="muet">${t("Auto stop")}</span>
    <select data-quota-seuil="${cle}">${seuils.sort((a, b) => a - b).map((s) => `<option value="${s}" ${r.seuil === s ? "selected" : ""}>${s ? t("at {0} %", [s]) : t("off")}</option>`).join("")}</select>
    ${r.seuil ? `<select data-quota-action="${cle}">${actions.map(([v, l]) => `<option value="${v}" ${r.action === v ? "selected" : ""}>${h(l)}</option>`).join("")}</select>` : ""}</div>`;
}
function rendreQuota(q, regles) {
  if (!q || !(q.fenetres || []).length) return `<div class="bloc"><h2>${t("Subscription quota")}</h2><div class="vide petit">${t("Shown once a cycle has run: Claude Code reports it as it works.")}</div></div>`;
  const maintenant = Date.now() / 1000;
  const lignes = q.fenetres.map((f) => {
    const passe = f.reinit && f.reinit < maintenant;
    const u = passe ? 0 : f.utilisation;
    const pct = u == null ? null : Math.round(u * 100);
    const classe = pct == null ? "" : pct >= 90 ? "plein" : pct >= 70 ? "haut" : "";
    const r = (regles || {})[regleFenetre(f.nom)];
    return `<div class="quota-ligne ${classe}">
      <div class="ligne m0"><b>${h(nomFenetre(f.nom))}</b><span class="espace"></span><span>${pct == null ? "—" : pct + " %"}</span></div>
      <div class="barre m0 quota-barre"><i style="width:${Math.min(100, pct || 0)}%"></i>${r ? `<b class="quota-seuil" style="left:${Math.min(100, r.seuil)}%" title="${h(t("Auto stop"))} ${r.seuil} %"></b>` : ""}</div>
      <div class="ligne m0 quota-pied"><span class="muet petit">${passe ? t("started again") : f.reinit ? t("Resets: {0} ({1})", [quandLisible(f.reinit), dans(f.reinit)]) : ""}</span><span class="espace"></span>${arretAuto(f.nom, regles)}</div>
    </div>`;
  }).join("");
  const age = Math.round(maintenant - (q.lu || maintenant));
  const vieux = age > 15 * 60;
  return `<div class="bloc quota-bloc"><h2>${t("Subscription quota")}</h2>${lignes}
    <div class="muet petit mt-1">${q.statut === "rejected" ? `<span class="attention">${t("Limit reached: the pilot waits for it to start again.")}</span> ` : ""}${vieux ? t("As of the last cycle, {0} ago: it may have changed since.", [age < 7200 ? t("{0} min", [Math.round(age / 60)]) : t("{0} h", [Math.round(age / 3600)])]) : t("As Claude Code reported it a moment ago.")}</div></div>`;
}

// --- progress: the milestones done over time, the projection to the last one (time and cost), the cost of
// each cycle. A cycle that ends with a new commit is a milestone done (one milestone per cycle); the curve
// is anchored on the true count of the plan.
function rendreAvancement(stats, plan, faits, actif) {
  const total = plan.length;
  const lignes = stats.map((s) => ({ ...s, fin: new Date(s.horodatage).getTime() })).filter((s) => !isNaN(s.fin)).sort((a, b) => a.fin - b.fin);
  if (lignes.length < 2 || !total) return "";
  let precedent = "";
  lignes.forEach((s) => { s.jalon = s.etat === "ok" && s.commit && s.commit !== precedent; if (s.commit) precedent = s.commit; });
  let n = faits;
  for (let i = lignes.length - 1; i >= 0; i--) { lignes[i].faits = Math.max(0, n); if (lignes[i].jalon) n--; }
  const debut = lignes[0].fin - (Number(lignes[0].minutes) || 0) * 60000, dernier = lignes[lignes.length - 1];
  // pace: over the last 8 milestones (their cycles' time, waits included), and their cost
  const jalons = lignes.filter((s) => s.jalon).slice(-8);
  let eta = null, coutReste = null, heuresReste = null;
  const reste = total - faits;
  if (reste > 0 && jalons.length >= 2) {
    const i0 = lignes.indexOf(jalons[0]);
    const t0 = i0 > 0 ? lignes[i0 - 1].fin : jalons[0].fin - (Number(jalons[0].minutes) || 0) * 60000;
    const parJalon = (jalons[jalons.length - 1].fin - t0) / jalons.length;
    const cout = lignes.slice(i0).reduce((a, s) => a + (Number(s.cout_usd) || 0), 0) / jalons.length;
    eta = Math.max(Date.now(), dernier.fin) + parJalon * reste;
    heuresReste = (parJalon * reste) / 3600000;
    coutReste = cout * reste;
  }
  const largeur = ($("#page") || {}).clientWidth || 1000;
  const fin = eta || dernier.fin, L = Math.round(Math.max(520, Math.min(1800, largeur - 110))), H = 210, g = 34, d = 40, hautB = 168, basB = 186;
  const x = (tm) => g + ((tm - debut) / Math.max(1, fin - debut)) * (L - g - d);
  const y = (k) => hautB - (k / total) * (hautB - 12);
  const maxCout = Math.max(...lignes.map((s) => Number(s.cout_usd) || 0), 0.01);
  const lang = document.documentElement.lang || "en";
  const plusieursJours = new Date(debut).toDateString() !== new Date(fin).toDateString();
  const etiquette = (tm) => { const dt = new Date(tm); const hm = dt.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" }); return plusieursJours ? dt.toLocaleDateString(lang, { weekday: "short" }) + " " + hm : hm; };
  const points = [[debut, 0], ...lignes.map((s) => [s.fin, s.faits])];
  const chemin = points.map(([tm, k], i) => `${i ? "L" : "M"}${x(tm).toFixed(1)},${y(k).toFixed(1)}`).join(" ");
  const aire = `${chemin} L${x(dernier.fin).toFixed(1)},${hautB} L${x(debut).toFixed(1)},${hautB} Z`;
  const barres = lignes.map((s) => { const hb = ((Number(s.cout_usd) || 0) / maxCout) * 46; return `<rect class="av-cout" x="${(x(s.fin) - 3).toFixed(1)}" y="${(hautB - hb).toFixed(1)}" width="6" height="${hb.toFixed(1)}" rx="1.5"><title>${h(t("Cycle {0}", [s.cycle]))} · ${h(etiquette(s.fin))} · ${Number(s.cout_usd).toFixed(2)} $ · ${Math.round(s.minutes)} min${s.jalon ? " · " + h(t("milestone done")) : ""}</title></rect>`; }).join("");
  const grilles = [0.25, 0.5, 0.75, 1].map((f) => `<line class="av-grille" x1="${g}" x2="${L - d}" y1="${y(total * f).toFixed(1)}" y2="${y(total * f).toFixed(1)}"/><text class="av-txt" x="${g - 6}" y="${(y(total * f) + 4).toFixed(1)}" text-anchor="end">${Math.round(total * f)}</text>`).join("");
  const ticks = [0, 0.25, 0.5, 0.75, 1].map((f) => { const tm = debut + f * (fin - debut); return `<text class="av-txt" x="${x(tm).toFixed(1)}" y="${basB + 14}" text-anchor="${f === 0 ? "start" : f === 1 ? "end" : "middle"}">${h(etiquette(tm))}</text>`; }).join("");
  const projection = eta ? `<line class="av-projection" x1="${x(dernier.fin).toFixed(1)}" y1="${y(faits).toFixed(1)}" x2="${x(eta).toFixed(1)}" y2="${y(total).toFixed(1)}"/><circle class="av-but" cx="${x(eta).toFixed(1)}" cy="${y(total).toFixed(1)}" r="4"><title>${h(t("Estimated end: {0}", [quandLisible(eta / 1000)]))}</title></circle>` : "";
  // each milestone point: what it delivered, and the cycles it took since the previous one (hover: a
  // bubble; click: the details under the chart, kept across the live refreshes by its commit)
  const infos = [];
  let depuis = 0;
  lignes.forEach((s, i) => {
    if (!s.jalon) return;
    const tranche = lignes.slice(depuis, i + 1); depuis = i + 1;
    const id = (s.titre || "").split(":")[0].trim();
    const prevu = id ? plan.find((j) => j.id === id || (j.titre || "").split(":")[0].trim() === id) : null;
    infos.push({ s, cle: s.commit + "·" + s.cycle, titre: s.titre || t("{0} / {1} milestones done", [s.faits, total]), detail: prevu && [prevu.detail, ...(prevu.notes || [])].filter(Boolean).join(" · "),
      cycles: tranche.map((r) => r.cycle), minutes: tranche.reduce((a, r) => a + (Number(r.minutes) || 0), 0),
      cout: tranche.reduce((a, r) => a + (Number(r.cout_usd) || 0), 0), quand: etiquette(s.fin) });
  });
  infos.forEach((p) => { p.cx = x(p.s.fin); p.cy = y(p.s.faits); });
  etat.avInfos = infos;
  const choisi = infos.find((p) => p.cle === etat.avChoix);
  const rang = infos.indexOf(choisi);
  // the chosen point drawn last: never hidden under its neighbours
  const pts = infos.map((p, i) => [p, i]).sort((a, b) => (a[0] === choisi) - (b[0] === choisi)).map(([p, i]) => `<circle class="av-point${p === choisi ? " actif" : ""}" data-av="${i}" cx="${p.cx.toFixed(1)}" cy="${p.cy.toFixed(1)}" r="${p === choisi ? 5 : 3.5}"/>`).join("");
  const duree = (m) => m >= 90 ? t("{0} h {1} min", [Math.floor(m / 60), Math.round(m % 60)]) : t("{0} min", [Math.round(m)]);
  const detail = choisi ? `<div class="av-detail"><div class="ligne m0"><b class="croit">${md(choisi.titre)}</b>
      <span class="muet petit">${rang + 1} / ${infos.length}</span>
      <button class="btn btn-petit" data-av-pas="-1" ${rang <= 0 ? "disabled" : ""} title="${h(t("Previous milestone (←)"))}">‹</button><button class="btn btn-petit" data-av-pas="1" ${rang >= infos.length - 1 ? "disabled" : ""} title="${h(t("Next milestone (→)"))}">›</button>
      <button class="btn btn-petit" data-av-fermer title="${h(t("Close"))}">✕</button></div>
      ${choisi.detail ? `<div class="muet petit mt-1">${md(choisi.detail)}</div>` : ""}
      <dl class="fiche mt-2"><dt>${t("Finished")}</dt><dd>${h(choisi.quand)} · ${h(t("{0} / {1} milestones done", [choisi.s.faits, total]))}</dd>
      <dt>${t("Cycles")}</dt><dd>${h(choisi.cycles.join(", "))}</dd>
      <dt>${t("Working time")}</dt><dd>${h(duree(choisi.minutes))}</dd>
      <dt>${t("Cost")}</dt><dd>${choisi.cout.toFixed(2)} $</dd>
      ${choisi.s.commit ? `<dt>${t("Commit")}</dt><dd><code>${h(choisi.s.commit)}</code></dd>` : ""}
      ${choisi.s.template ? `<dt>${t("Model")}</dt><dd>${h(choisi.s.template)}</dd>` : ""}</dl></div>`
    : infos.length ? `<p class="muet petit mt-1">${t("Click a point to see its milestone; then ‹ › (or the arrow keys) go from one milestone to the next.")}</p>` : "";
  const resume = reste <= 0 ? t("Every milestone of the plan is done.")
    : eta ? t("At this pace, the last milestone around {0}: about {1} h of work and ≈ {2} $ more at list price.", [quandLisible(eta / 1000), heuresReste.toLocaleString(lang, { maximumFractionDigits: heuresReste < 10 ? 1 : 0 }), Math.round(coutReste)])
    : t("The estimate appears after two milestones.");
  return `<div class="bloc"><div class="ligne m0"><h2 class="m0">${t("Progress")}</h2><span class="espace"></span><span class="muet petit">${t("{0} milestone(s) left", [Math.max(0, reste)])}</span></div>
    <p class="av-resume">${h(resume)}</p>
    <svg class="avancement" viewBox="0 0 ${L} ${H}" role="img" aria-label="${h(resume)}">
      ${grilles}<path class="av-aire" d="${aire}"/>${barres}<path class="av-ligne" d="${chemin}"/>${projection}${pts}
      <line class="av-axe" x1="${g}" x2="${L - d}" y1="${hautB}" y2="${hautB}"/>${ticks}
    </svg>
    <div class="av-legende muet petit"><span><i class="av-l-ligne"></i>${t("milestones done")}</span>${eta ? `<span><i class="av-l-proj"></i>${t("projection")}</span>` : ""}<span><i class="av-l-cout"></i>${t("cost of each cycle (max {0} $)", [maxCout.toFixed(2)])}</span></div>
    ${detail}
  </div>`;
}

pages.suivi = async () => {
  const v = etat.vue; if (!v) return "";
  if (!v.kit_installe || !v.projet || v.demande_vide) return rendreGuide(v);
  const e = v.etat || {};
  const plan = v.plan || [];
  const faits = plan.filter((j) => j.fait).length;
  const stats = v.stats || [];
  const somme = (k) => stats.reduce((a, s) => a + (Number(s[k]) || 0), 0);
  const cache = somme("cache_lu"), plein = somme("entree_tokens") + somme("cache_ecrit");
  const ratio = cache + plein ? Math.round((cache / (cache + plein)) * 100) : 0;
  return `${v.pour_vous ? `<a class="bloc pv-bandeau" href="#pourvous">🙋 <strong>${t("Claude needs you")}</strong> · ${t("{0} task(s) on the For you page", [v.pour_vous])} →</a>` : ""}
  <div class="grille grille-2">
    <div class="colonne"><div class="bloc"><h2>${t("Right now")}</h2>
      <dl class="fiche">
        <dt>${t("Milestone")}</dt><dd>${e.jalon_en_cours ? `<b>${md(titreJalon(e.jalon_en_cours))}</b>${(() => { const j = plan.find((x) => x.titre === titreJalon(e.jalon_en_cours)); return j && j.detail ? `<div class="muet petit">${md(j.detail)}</div>` : ""; })()}` : "—"}</dd>
        <dt>${t("Last commit")}</dt><dd class="muet">${h(e.dernier_commit || "—")}</dd>
        ${e.reprise_prevue ? `<dt>${t("Resumes")}</dt><dd class="quota">${h(e.reprise_prevue)}</dd>` : ""}
        <dt>${t("Queued requests")}</dt><dd>${v.attente || 0}</dd>
        <dt>${t("Updated")}</dt><dd class="muet petit">${h(heureMaj(e.maj))}</dd>
      </dl>
      <div class="ligne mt-2">
        <button class="btn btn-petit" data-ouvrir="projet">${t("Open the project")}</button>
        <button class="btn btn-petit" data-ouvrir="atelier">${t("Workshop (.athanor)")}</button>
        <button class="btn btn-petit" data-ouvrir="deliverable">${t("Deliverable")}</button>
      </div>
    </div>
    ${rendreQuota(v.quota, v.quota_arret)}</div>
    <div class="bloc"><h2>${t("Project plan")}</h2>
      ${plan.length ? `<div class="barre"><i style="width:${(faits / plan.length) * 100}%"></i></div><div class="muet petit">${t("{0} / {1} milestones done", [faits, plan.length])}</div>
      <ul class="plan">${listePlan(plan, false)}</ul>`
        : `<div class="vide">${t("No plan yet: Claude writes it during the first cycle (framing).")}</div>`}
    </div>
  </div>
  ${rendreAvancement(stats, plan, faits, v.actif)}
  <div class="bloc"><h2>${t("Usage")}</h2>
    ${stats.length ? `<div class="chiffres">
      <div class="chiffre"><b>${stats.filter((s) => !String(s.cycle).includes("·")).length}</b><span>${t("cycles")}</span></div>
      <div class="chiffre"><b>${(() => { const m = Math.round(somme("minutes")); return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, "0")}`; })()}</b><span>${t("of work")}</span></div>
      <div class="chiffre"><b>${somme("tours")}</b><span>${t("turns")}</span></div>
      <div class="chiffre" title="${h(t("at list price (your subscription quota pays)"))}"><b>${stats.some((s) => s.estime) ? "≈ " : ""}${somme("cout_usd").toFixed(2)} $</b><span>${t("at list price")}</span></div>
      <div class="chiffre"><b>${ratio} %</b><span>${t("of tokens re-read from cache")}</span></div>
      <div class="chiffre"><b>${fmtTokens(somme("sortie_tokens"))}</b><span>${t("tokens produced")}</span></div>
      <div class="chiffre"><b>${fmtTokens(somme("contexte_max") / stats.length)}</b><span>${t("average peak context")}</span></div>
    </div>
    <div class="tableau mt-2"><table class="tab"><thead><tr><th>${t("Cycle")}</th><th>${t("Time")}</th><th>${t("Model")}</th><th>${t("State")}</th><th>${t("Minutes")}</th><th>${t("Turns")}</th><th>${t("Cost $")}</th><th>${t("Peak context")}</th></tr></thead><tbody>
    ${stats.slice(etat.ouverts.has("stats:tous") ? -200 : -5).reverse().map((s) => `<tr><td>${s.cycle}</td><td>${h((s.horodatage || "").slice(11, 16))}</td><td>${h(s.template)}</td><td class="${h(s.etat)}">${h(s.etat)}</td><td>${Math.round(s.minutes)}</td><td>${s.tours}</td><td${s.estime ? ` title="${h(t("≈: estimated from the turns of the lead and its sub-agents (cycle closed by the pilot); ?: price of this model not learned yet"))}"` : ""}>${s.estime && !Number(s.cout_usd) && s.tours ? "?" : (s.estime ? "≈ " : "") + Number(s.cout_usd).toFixed(2)}</td><td>${fmtTokens(s.contexte_max)}</td></tr>`).join("")}
    </tbody></table></div>
    ${stats.length > 5 ? `<button type="button" class="btn btn-petit mt-1" data-stats-tous>${etat.ouverts.has("stats:tous") ? t("Last 5 cycles only") : t("All {0} cycles", [stats.length])}</button>` : ""}` : `<div class="vide">${t("Numbers appear at the end of the first cycle.")}</div>`}
  </div>`;
};

// the installers of this system: on Windows, one per mode (choosing it also selects that mode)
function boutonsInstallation(v, primaire) {
  if (!v.windows) return `<button class="btn ${primaire ? "btn-primaire" : ""}" data-installer>${t("Install everything")}</button>`;
  return `<button class="btn ${primaire ? "btn-primaire" : ""}" data-installer-mode="${v.natif ? "natif" : "wsl"}">${t("Install everything")}</button>`;
}
// the systems of the Setup page: one tab per way Claude can run here, the current one marked
function modesSysteme(v) {
  if (v.windows) return [
    { id: "natif", nom: "Native Windows", actuel: !!v.natif,
      texte: "Claude runs directly on Windows: Git for Windows, jq, Python, Node.js and Claude Code for Windows. Nothing else to install, no WSL. No sandbox: Claude can read and change any file of your PC. For projects you trust." },
    { id: "wsl", nom: "WSL", actuel: !v.natif,
      texte: "Claude runs in WSL2 (the Linux built into Windows), in a sandbox: it only writes in the project. Installs Linux once (a window opens, Linux asks for a password once)." }];
  if (v.systeme === "darwin") return [{ id: "mac", nom: "macOS", actuel: true,
    texte: "Claude Code and the tools, installed in a terminal (it asks for your password once)." }];
  return [{ id: "linux", nom: "Linux", actuel: true,
    texte: "Claude Code, the system sandbox (bubblewrap) and the tools, in a terminal; it asks for your password once." }];
}

function rendreGuide(v) {
  const e1 = v.kit_installe, e2 = !!v.projet, e3 = e2 && !v.demande_vide;
  // the tooling of this system: on Windows, WSL (Claude in a sandbox) or native Windows, chosen here
  const outillage = !v.windows ? "Claude Code, the system sandbox (bubblewrap) and the tools, in a terminal; it asks for your password once."
    : v.natif ? "Native Windows: Git for Windows, jq, Python, Node.js and Claude Code for Windows, without WSL. Claude has no sandbox in this mode."
    : "WSL2 (the Linux built into Windows), Claude Code, the system sandbox. A window opens; Linux asks for a password once.";
  return `<div class="bloc"><h2>${t("First steps")}</h2><p class="intro">${t("Five steps, all from this window. Then you can let it run for days, on several projects at once.")}</p>
  <div class="etapes">
    <div class="etape ${e1 ? "faite" : ""}"><div class="n">1</div><div><b>${t("Install the tooling")}</b><div class="d">${t(outillage)}</div>${v.windows ? `<div class="muet petit">${t("Current mode: {0}. To choose the other one: Setup & health.", [t(v.natif ? "Native Windows" : "WSL")])}</div>` : ""}</div>
      ${boutonsInstallation(v, !e1)}</div>
    <div class="etape"><div class="n">2</div><div><b>${t("Check and connect")}</b><div class="d">${t("Checks all the tooling and the token of your Claude subscription. Every problem has its own “Repair” or “Ask Claude” button.")}</div></div>
      <a class="btn" href="#sante">${t("Open the check")}</a></div>
    <div class="etape ${e2 ? "faite" : ""}"><div class="n">3</div><div><b>${t("Add a project")}</b><div class="d">${t("Its name, its development folder (anywhere on the PC), and what you want: a game, a website, an app, a tool. The tool only writes in a .athanor subfolder, to delete at the end.")}</div></div>
      <a class="btn ${e1 && !e2 ? "btn-primaire" : ""}" href="#nouveau">${e2 ? t("Another project") : t("Add a project")}</a></div>
    <div class="etape ${e3 ? "faite" : ""}"><div class="n">4</div><div><b>${t("Say what you want")}</b><div class="d">${t("If not done yet: describe the expected result. Technologies are free unless you impose some.")}</div></div>
      <a class="btn ${e2 && !e3 ? "btn-primaire" : ""}" href="#demandes" ${e2 ? "" : 'aria-disabled="true"'}>${t("Write the request")}</a></div>
    <div class="etape"><div class="n">5</div><div><b>${t("Start")}</b><div class="d">${t("Claude frames, plans milestones, builds, checks and delivers, cycle after cycle. You follow everything here and can add requests along the way.")}</div></div>
      <button class="btn ${e3 ? "btn-primaire" : ""}" data-act="demarrer" ${e2 ? "" : "disabled"}>${t("Start")}</button></div>
  </div></div>`;
}

// --- new project
// --- managing a project: name, folder, milestones check, archive, delete
pages.projet = async () => {
  if (!etat.projet) await rafraichir(); // opened straight on this page: the project is known after the first refresh
  if (!etat.projet) return `<div class="vide">${t("No project.")}</div>`;
  const g = await api("projets/gestion?nom=" + encodeURIComponent(etat.projet));
  const p = g.projet, dis = p.actif ? "disabled" : "";
  const decoches = Object.entries(g.decoches || {});
  setTimeout(() => majGitHub(), 0); // the GitHub block asks WSL: the last state known first, then updated
  return `<div class="gestion">
  ${p.actif ? `<div class="bloc"><p class="attention">${t("The pilot is running: stop it to rename, move, archive or delete the project.")}</p></div>` : ""}
  ${await rendreMode()}
  <div class="grille grille-2">
    <div class="bloc"><h2>${t("Name")}</h2>
      <div class="ligne"><input type="text" id="gp-nom" value="${h(p.nom)}"><button class="btn" data-gestion="renommer" ${dis}>${t("Rename")}</button></div>
      <p class="muet petit">${t("Only the name shown in Athanor: the folder keeps its name.")}</p>
    </div>
    <div class="bloc"><h2>${t("Milestones")}</h2>
      <p class="muet petit">${t("Athanor compares the ticked milestones with what the folder contains: in an empty folder nothing is done, and a milestone whose files are missing is unticked. When the files come back, it is ticked again by itself. Claude checks more thoroughly at the next start.")}</p>
      ${decoches.length ? `<ul class="petit">${decoches.map(([id, r]) => `<li><b>${h(id)}</b> <span class="muet">${h(t(r.startsWith("missing: ") ? "missing: {0}" : r, [r.slice(9)]))}</span></li>`).join("")}</ul>` : ""}
      ${g.reverifier ? `<p class="petit ok">${t("Claude checks the milestones at the next start.")}</p>` : ""}
      <div class="ligne"><button class="btn btn-petit" data-gestion="verifier" ${dis}>${t("Check now")}</button><button class="btn btn-petit" data-gestion="verifier-claude" ${dis}>${t("Have Claude check them at the next start")}</button></div>
    </div>
  </div>
  <div class="bloc"><h2>${t("Folder")}</h2>
    <p><code>${h(p.dossier)}</code>${g.vide ? ` <span class="muet petit">(${t("empty")})</span>` : ""}</p>
    <label class="champ"><span>${t("Move or rename the folder, with everything in it (same drive)")}</span>
      <div class="ligne"><input type="text" id="gp-deplacer" value="${h(p.dossier)}"><button class="btn" data-gestion="deplacer" ${dis}>${t("Move")}</button></div></label>
    <label class="champ mt-2"><span>${t("Use another folder (created if needed): the plan and the requests come along, the milestones are checked against what it contains")}</span>
      <div class="ligne"><input type="text" id="gp-dossier" placeholder="${h(t("e.g. D:\\Dev\\my-site"))}"><button class="btn" data-gestion="dossier" ${dis}>${t("Use this folder")}</button></div></label>
  </div>
  <div class="bloc" id="gh-bloc">${blocGitHubInitial()}</div>
  <div class="grille grille-2">
    <div class="bloc"><h2>${t("Archive")}</h2>
      <p class="muet petit">${t("An archived project leaves the project list and is never restarted. Bring it back at any time from “Archived projects”, at the bottom of the project list.")}</p>
      <button class="btn" data-gestion="archiver" ${dis}>${t("Archive the project")}</button>
    </div>
    <div class="bloc"><h2>${t("Delete")}</h2>
      <label class="radio"><input type="radio" name="gp-suppr" value="liste" checked> ${t("Remove from the list only: the folder stays as it is")}</label>
      <label class="radio"><input type="radio" name="gp-suppr" value="atelier"> ${t("Also send its workshop (.athanor: plan, requests, log) to the recycle bin")}</label>
      <label class="radio"><input type="radio" name="gp-suppr" value="dossier"> ${t("Send the whole folder to the recycle bin")}</label>
      <button class="btn btn-danger mt-2" data-gestion="supprimer" data-dossier="${h(p.dossier)}" ${dis}>${t("Delete the project")}</button>
    </div>
  </div></div>`;
};

pages.archives = async () => {
  if (!etat.vue) await rafraichir();
  const l = ((etat.vue && etat.vue.projets) || []).filter((x) => x.archive);
  return `<div class="bloc gestion"><h2>${t("Archived projects")}</h2>
    <p class="muet petit">${t("Out of the project list and never restarted. Bring one back to work on it again.")}</p>
    ${l.length ? `<table class="tab"><tbody>${l.map((p) => `<tr><td><b>${h(p.nom)}</b></td><td class="muet">${h(p.dossier)}</td><td class="droite"><div class="ligne fin">
      <button class="btn btn-petit" data-gestion="desarchiver" data-nom="${h(p.nom)}">${t("Bring back")}</button>
      <select data-mode-suppr><option value="liste">${t("Remove from the list only")}</option><option value="atelier">${t("+ its workshop to the recycle bin")}</option><option value="dossier">${t("The whole folder to the recycle bin")}</option></select>
      <button class="btn btn-petit btn-danger" data-gestion="supprimer" data-nom="${h(p.nom)}" data-dossier="${h(p.dossier)}">${t("Delete")}</button></div></td></tr>`).join("")}</tbody></table>`
      : `<div class="vide">${t("No archived project.")}</div>`}
  </div>`;
};

async function gererProjet(act, nom, bouton) {
  const corps = { nom, action: act };
  if (act === "renommer") corps.valeur = $("#gp-nom").value;
  if (act === "deplacer") corps.valeur = $("#gp-deplacer").value;
  if (act === "dossier") {
    corps.valeur = $("#gp-dossier").value;
    if (!corps.valeur.trim() || !(await confirmer(t("Work on “{0}” in {1} from now on? Its plan and requests come along; its milestones are checked against what this folder contains. The current folder stays as it is.", [nom, corps.valeur]), { ok: t("Use this folder") }))) return;
  }
  if (act === "verifier-claude") { corps.action = "verifier"; corps.mode = "claude"; }
  if (act === "archiver" && !(await confirmer(t("Archive “{0}”? It leaves the project list; bring it back at any time.", [nom]), { ok: t("Archive the project") }))) return;
  if (act === "supprimer") {
    const sel = bouton && bouton.closest("tr") ? bouton.closest("tr").querySelector("[data-mode-suppr]") : null;
    corps.mode = sel ? sel.value : ((document.querySelector("input[name=gp-suppr]:checked") || {}).value || "liste");
    const textes = {
      liste: t("Remove “{0}” from the list? Its folder stays intact.", [nom]),
      atelier: t("Remove “{0}” and send its workshop (.athanor: plan, requests, log) to the recycle bin? The project's own files stay.", [nom]),
      dossier: t("Send the whole folder of “{0}” to the recycle bin? {1} and everything in it go, and can be restored from the recycle bin.", [nom, (bouton && bouton.dataset.dossier) || ""]),
    };
    if (!(await confirmer(textes[corps.mode], { ok: t("Delete the project"), danger: true }))) return;
  }
  if (bouton) bouton.disabled = true;
  const r = await action(api("projets/gestion", "POST", corps));
  if (bouton) bouton.disabled = false;
  if (!r) return;
  if (corps.action === "verifier") {
    const d = (r.decoches || []).length, c = (r.recoches || []).length;
    toast(d || c ? t("{0} milestone(s) unticked, {1} ticked again.", [d, c]) : t("The milestones match the folder."), "ok");
    if (corps.mode === "claude") toast(t("Claude checks the milestones at the next start."), "ok");
  }
  if (act === "renommer" && r.nom) etat.projet = r.nom;
  if ((act === "archiver" || act === "supprimer") && nom === etat.projet) { etat.projet = ""; etat.evenements = []; etat.dernierJournal = 0; }
  if (act === "desarchiver") { etat.projet = nom; etat.evenements = []; etat.dernierJournal = 0; }
  await rafraichir();
  if (act === "desarchiver" || (act === "archiver" || act === "supprimer") && etat.page === "projet") { location.hash = "#suivi"; return; }
  rendrePage();
}

// --- GitHub: the project's repository (system/github.sh). The sign-in is never kept: the repository gets
// its own key, valid for it alone and out of Claude's reach; the pilot pushes after each milestone.
function nomDepot(nom) {
  const s = String(nom || "").replace(/ /g, "-").replace(/[^A-Za-z0-9._-]/g, "").replace(/^[.-]+|[.-]+$/g, "").slice(0, 100);
  return s || "athanor-project";
}
function choixGitHub(pre, nomPropose) {
  return `${pre === "np" ? `<label class="radio"><input type="radio" name="np-gh" value="" checked> ${t("No GitHub for now")}</label>` : ""}
    <label class="radio"><input type="radio" name="${pre}-gh" value="creer" ${pre === "np" ? "" : "checked"}> ${t("Create a private repository")}</label>
    <input type="text" class="gh-champ" id="${pre}-gh-nom" placeholder="${h(nomPropose)}">
    <label class="radio"><input type="radio" name="${pre}-gh" value="existant"> ${t("Use one of my repositories")}</label>
    <input type="text" class="gh-champ" id="${pre}-gh-depot" placeholder="${h(t("OWNER/NAME, e.g. octocat/my-site"))}">
    <label class="interrupteur"><input type="checkbox" id="${pre}-gh-auto" checked><span>${t("Push automatically after each milestone")}</span></label>
    <p class="muet petit">${t("A window opens once to sign in to GitHub. Athanor gives the repository its own key, valid for that repository alone, then signs out: Claude can never touch your other repositories.")}</p>`;
}
// null: no GitHub; false: incomplete or invalid (said in a toast); else what to send to /api/github
function lireChoixGitHub(pre, nomProjet) {
  const mode = (document.querySelector(`input[name=${pre}-gh]:checked`) || {}).value || "";
  if (!mode) return null;
  const auto = !!($("#" + pre + "-gh-auto") || {}).checked;
  if (mode === "creer") {
    const tape = $("#" + pre + "-gh-nom").value.trim();
    const valeur = tape ? nomDepot(tape) : nomDepot(nomProjet);
    if (tape && valeur !== tape) $("#" + pre + "-gh-nom").value = valeur; // spaces and accents become a valid name
    return { action: "lier", mode, valeur, auto };
  }
  // OWNER/NAME, or the repository's address as GitHub shows it
  const v = $("#" + pre + "-gh-depot").value.trim().replace(/^(https?:\/\/)?(www\.)?github\.com\//i, "").replace(/^git@github\.com:/i, "").replace(/\/+$/, "").replace(/\.git$/i, "");
  if (!/^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9._-]{1,100}$/.test(v)) { toast(t("Write the repository as OWNER/NAME (e.g. octocat/my-site)."), "erreur"); return false; }
  return { action: "lier", mode, valeur: v, auto };
}
// Author of the commits (name, email proposed from the GitHub account) and Claude as co-author or not.
function rendreAuteur(id) {
  id = id || {};
  const emails = id.emails || [];
  const libelle = (e) => e.genre === "noreply" ? t("{0} (private GitHub address, recommended)", [e.email]) : e.genre === "github" ? t("{0} (GitHub account)", [e.email]) : t("{0} (git settings)", [e.email]);
  const choisi = id.email || (emails[0] && emails[0].email) || "";
  const connu = emails.some((e) => e.email === choisi);
  return `<label class="interrupteur"><input type="checkbox" data-gh-coauteur ${id.sans_coauteur ? "checked" : ""}><span>${t("Commit without Claude as co-author (no “Co-Authored-By: Claude” line in the commits)")}</span></label>
    <div class="gh-auteur"><div class="muet petit">${t("Author of the commits")}</div>
      <div class="ligne"><input type="text" id="gh-auteur-nom" list="gh-noms" value="${h(id.nom || (id.noms || [])[0] || "")}" placeholder="${h(t("Your name"))}">
        <select id="gh-auteur-email">${emails.map((e) => `<option value="${h(e.email)}" ${e.email === choisi ? "selected" : ""}>${h(libelle(e))}</option>`).join("")}<option value="" ${connu ? "" : "selected"}>${t("Other email…")}</option></select></div>
      <input type="text" inputmode="email" class="gh-champ" id="gh-auteur-autre" value="${h(connu ? "" : choisi)}" placeholder="${h(t("you@example.com"))}" ${connu ? "hidden" : ""}>
      <datalist id="gh-noms">${(id.noms || []).map((n) => `<option value="${h(n)}">`).join("")}</datalist>
      <div class="ligne"><button class="btn btn-petit" data-gh="identite">${t("Save the author")}</button>${id.defini ? "" : `<span class="muet petit">${t("Not chosen yet for this project: the commits use your last choice, else your git settings, else “Athanor”.")}</span>`}</div></div>`;
}
const LIBELLES_GH = { ok: "Last push", bloque: "Push blocked", rejete: "Push refused", erreur: "Push failed", attente: "Waiting for a first commit" };
function rendreGitHub(r) {
  if (r.erreur) return `<h2>GitHub</h2><p class="bloque petit">${h(t(r.erreur))}</p><button class="btn btn-petit" data-gh="actualiser">${t("Refresh")}</button>`;
  const g = r.github || {}, d = g.dernier;
  const manque = !g.gh || !g.ssh ? `<p class="attention petit">${t("The GitHub CLI or OpenSSH is missing in Linux: open the check and repair “Basic Linux packages”.")} <a href="#sante">${t("Open the check")}</a></p>` : "";
  if (!g.lie) return `<h2>GitHub</h2>
    <p class="muet petit">${t("Send the project's commits to one of your GitHub repositories, by itself after each milestone.")}</p>${manque}
    <div class="gh-etape"><b>1. ${t("Repository")}</b>${choixGitHub("gh", r.nom_propose || "")}</div>
    <div class="gh-etape"><b>2. ${t("Commits")}</b>${rendreAuteur(g.identite)}</div>
    <div class="gh-etape"><b>3. ${t("Link")}</b><div class="ligne"><button class="btn btn-primaire" data-gh="lier">${t("Link to GitHub")}</button><button class="btn btn-petit" data-gh="actualiser">${t("Refresh")}</button></div></div>`;
  const cls = !d ? "" : d.resultat === "ok" ? "ok" : d.resultat === "attente" ? "muet" : "bloque";
  const quand = d && d.quand ? String(d.quand).replace("T", " ").slice(0, 16) : "";
  const test = etat.ghTest && etat.ghTest.projet === etat.projet ? `<p class="petit ${etat.ghTest.ok ? "ok" : "bloque"}">${etat.ghTest.ok ? "✔" : "✖"} ${h(t(etat.ghTest.message_key || "", etat.ghTest.args || []))}</p>` : "";
  return `<h2>GitHub</h2>${manque}
    <p class="gh-statut"><span class="ok">✔</span> ${t("Linked to")} <b>${h(g.depot)}</b> · ${t("branch")} <code>${h(g.branche)}</code>${g.compte ? ` · ${t("account")} ${h(g.compte)}` : ""} <button class="btn btn-petit" data-gh="ouvrir">${t("Open on GitHub")}</button></p>
    <div class="ligne"><button class="btn" data-gh="tester">${t("Test the connection")}</button><button class="btn" data-gh="pousser">${t("Push now")}</button><button class="btn btn-primaire" data-gh="wip" title="${h(t("Commits what is in the folder now (“M3: … (in progress)”) and pushes it, without Claude: someone else can take over, even with no quota left."))}">${t("Save and push the work in progress")}</button>${d && d.resultat === "bloque" && d.sha ? `<button class="btn btn-danger" data-gh="malgre" data-sha="${h(d.sha)}">${t("Push anyway")}</button>` : ""}</div>
    <p class="muet petit">${t("Commits what is in the folder now (“M3: … (in progress)”) and pushes it, without Claude: someone else can take over, even with no quota left.")}</p>
    ${test}
    ${d ? `<div class="gh-dernier"><b class="${cls}">${h(t(LIBELLES_GH[d.resultat] || d.resultat))}</b> <span class="muet petit">${h(quand)}${d.commit ? " · " + h(d.commit) : ""}</span>
      <div class="petit">${h(t(d.modele || d.message || "", d.args || []))}</div>
      ${(d.fichiers || []).length ? `<ul class="petit">${d.fichiers.map((x) => `<li><code>${h(x.f)}</code> — ${h(t(x.genre))}</li>`).join("")}</ul>` : ""}</div>`
      : `<p class="muet petit">${t("Nothing pushed yet.")}</p>`}
    <label class="interrupteur"><input type="checkbox" data-gh-auto ${g.auto ? "checked" : ""}><span>${t("Push automatically after each milestone")}</span></label>
    ${rendreAuteur(g.identite)}
    <label class="champ mt-2"><span>${t("GitHub branch")}</span><div class="ligne"><input type="text" id="gh-branche" value="${h(g.branche)}"><button class="btn btn-petit" data-gh="branche">${t("Change")}</button></div></label>
    <p class="muet petit">${t("The key Athanor pushes with works for this repository only and stays out of Claude's reach. Athanor never overwrites what is on GitHub and checks every push for secrets (keys, tokens, .env files).")}</p>
    <div class="ligne"><button class="btn btn-petit" data-gh="actualiser">${t("Refresh")}</button><button class="btn btn-petit btn-danger" data-gh="delier">${t("Unlink")}</button></div>`;
}
// the GitHub block: the last state known is shown at once (WSL can take a while to answer), then updated
function blocGitHubInitial() {
  const c = (etat.ghCache || {})[etat.projet];
  if (c) return rendreGitHub(c) + `<p class="muet petit gh-maj">${t("Updating…")}</p>`;
  return `<h2>GitHub</h2><p class="muet petit gh-maj">${t("Loading…")}</p><button class="btn btn-petit" data-gh="actualiser">${t("Refresh")}</button>`;
}
async function majGitHub(jusqua) {
  clearTimeout(etat.ghMinuteur);
  // a later call (another page render, another click) takes over; a running follow-up is kept
  const n = etat.ghNumero = (etat.ghNumero || 0) + 1;
  if (!jusqua && etat.ghSuivi && etat.ghSuivi.projet === etat.projet && Date.now() < etat.ghSuivi.jusqua) jusqua = etat.ghSuivi.jusqua;
  // the page is still being built (its other blocks are awaited): the block comes a moment later
  for (let i = 0; i < 100 && !$("#gh-bloc") && etat.page === "projet" && n === etat.ghNumero; i++) await new Promise((r) => setTimeout(r, 100));
  if (!$("#gh-bloc") || etat.page !== "projet" || n !== etat.ghNumero) return;
  const nom = etat.projet;
  // WSL slow to answer: say so, with the Refresh button already there
  const lent = setTimeout(() => { const m = $("#gh-bloc .gh-maj"); if (m && n === etat.ghNumero) m.textContent = t("Still waiting for WSL (it may be starting)…"); }, 12000);
  const r = await api("github").catch((e) => ({ erreur: e.message }));
  clearTimeout(lent);
  if (!r.erreur) { etat.ghCache = etat.ghCache || {}; etat.ghCache[nom] = r; }
  const bloc = $("#gh-bloc");
  if (n !== etat.ghNumero || !bloc || etat.projet !== nom || etat.page !== "projet") return;
  const saisis = {}; bloc.querySelectorAll("input[id]").forEach((i) => { saisis[i.id] = i.type === "checkbox" ? i.checked : i.value; });
  const choix = (bloc.querySelector("input[name=gh-gh]:checked") || {}).value;
  bloc.innerHTML = rendreGitHub(r);
  bloc.querySelectorAll("input[id]").forEach((i) => { if (i.id in saisis && i.id !== "gh-branche") { if (i.type === "checkbox") i.checked = saisis[i.id]; else i.value = saisis[i.id]; } });
  if (choix) { const c = bloc.querySelector(`input[name=gh-gh][value="${choix}"]`); if (c) c.checked = true; }
  const g = r.github || {};
  // after "Link": the window is the user's; the block follows until the link and its first push are there
  if (jusqua && Date.now() < jusqua && !(g.lie && g.dernier)) etat.ghMinuteur = setTimeout(() => majGitHub(jusqua), 4000);
  else etat.ghSuivi = null;
}
function lireAuteur() {
  const sel = $("#gh-auteur-email");
  return { nom: ($("#gh-auteur-nom") || {}).value || "", email: (sel && sel.value) || ($("#gh-auteur-autre") || {}).value || "", sans_coauteur: !!(document.querySelector("[data-gh-coauteur]") || {}).checked };
}
async function actionGitHub(act, b) {
  if (act === "actualiser") return majGitHub();
  if (act === "tester") {
    b.disabled = true; b.textContent = t("Testing…");
    const r = await api("github", "POST", { action: "tester" }).catch((e) => { toast(e.message, "erreur"); return null; });
    if (r) { etat.ghTest = Object.assign({ projet: etat.projet }, r); toast(t(r.message_key || "", r.args || []), r.ok ? "ok" : "erreur"); }
    return majGitHub();
  }
  const corps = { action: act };
  if (act === "lier") { const c = lireChoixGitHub("gh", etat.projet); if (!c) return; Object.assign(corps, c); }
  if (act === "branche") corps.valeur = $("#gh-branche").value;
  if (act === "malgre") corps.valeur = b.dataset.sha || "";
  if (act === "identite") Object.assign(corps, lireAuteur());
  if (act === "malgre" && !(await confirmer(t("Push anyway? What Athanor flagged becomes readable on GitHub, in the history, even if it is deleted later. Only do it if these are not real secrets."), { ok: t("Push anyway"), danger: true }))) return;
  if (act === "delier" && !(await confirmer(t("Unlink “{0}” from GitHub? Nothing is deleted on GitHub: Athanor deletes its key and stops pushing.", [etat.projet]), { ok: t("Unlink"), danger: true }))) return;
  b.disabled = true;
  if (act === "pousser" || act === "malgre" || act === "wip") b.textContent = t("Pushing…");
  const r = await action(api("github", "POST", corps));
  b.disabled = false;
  if (r && r.enregistre) toast(t("Saved: {0}", [r.enregistre]), "ok");
  if (r && r.github && r.github.dernier) { const d = r.github.dernier; toast(t(d.modele || d.message || "", d.args || []), d.resultat === "ok" ? "ok" : "erreur"); }
  if (act === "lier" && r) etat.ghSuivi = { projet: etat.projet, jusqua: Date.now() + 5 * 60000 };
  majGitHub();
}

pages.nouveau = async () => `<div class="grille grille-2">
    <div class="bloc"><h2>${t("New project")}</h2>
      <p class="intro petit">${t("A project = a development folder, anywhere on the PC (existing or new). The tool only adds a .athanor subfolder (request, plan, log, checks): delete it at the end and the project is clean.")}</p>
      <label class="champ"><span>${t("Name")}</span><input type="text" id="np-nom" placeholder="${h(t("e.g. watch-store"))}"></label>
      <label class="champ"><span>${t("Development folder")}</span><input type="text" id="np-dossier" placeholder="${h(t("e.g. D:\\Dev\\watch-store"))}"></label>
      <label class="champ"><span>${t("What you want (you can complete it later)")}</span><textarea id="np-demande" rows="7" placeholder="${h(t("Describe the expected result, for whom, on which platform. Technologies are free unless you impose one."))}"></textarea></label>
      ${barreAmeliorer("np-demande")}
      ${zonePieces("np")}
      <div class="champ gh-nouveau"><span>GitHub</span>${choixGitHub("np", nomDepot(""))}</div>
      <button class="btn btn-primaire" id="np-creer">${t("Create the project")}</button>
    </div>
    <div class="bloc"><h2>${t("Pick a folder")}</h2>
      <div class="racines" id="np-racines"></div>
      <div class="chemin" id="np-chemin"></div>
      <div class="explorateur" id="np-explorateur"></div>
      <p class="muet petit mt-2">${t("Click a folder to enter it; “Use this folder” copies the path shown. For a new folder, complete the path by hand: it will be created.")}</p>
      <button class="btn" id="np-utiliser">${t("Use this folder")}</button>
    </div>
  </div>`;

async function rendreExplorateur(chemin) {
  const r = await api("dossiers?chemin=" + encodeURIComponent(chemin || "")).catch(() => null); if (!r || !$("#np-chemin")) return;
  etat.explorateur = r.chemin;
  $("#np-chemin").textContent = r.chemin;
  $("#np-racines").innerHTML = r.racines.map((x) => `<button class="btn btn-petit" data-chemin="${h(x.chemin)}">${x.nom === "~" ? t("Home folder") : h(x.nom)}</button>`).join("");
  $("#np-explorateur").innerHTML = (r.parent ? `<div class="parent" data-chemin="${h(r.parent)}">↑ ${t("parent folder")}</div>` : "") + (r.dossiers.map((d) => `<div data-chemin="${h(d.chemin)}">📁 ${h(d.nom)}</div>`).join("") || `<div class="muet">${t("No subfolder.")}</div>`);
}

// --- team
// A tree: the project lead and the parallel workers (the Claude sessions of the cycle), each with the
// sub-agents it called. A click on a sub-agent shows the brief it received and the report it handed back.
const ROLES = {
  architect: "Requirements, architecture and the milestone plan",
  creator: "Content: story, world, dialogue, texts, generated assets",
  developer: "One precise development task, with its tests",
  tester: "Automated tests, screenshots, bug reports",
  critic: "Uses the result like a demanding end user",
  executor: "Mechanical work, on the cheap model",
  scout: "Reads and summarises one question for the framing",
};
function modeleLisible(m) {
  if (!m) return "";
  if (String(m).toLowerCase() === "auto") return t("chosen by the lead");
  const n = String(m).replace(/^claude-/, "").replace(/\[.*\]$/, "").replace(/-\d{8}$/, "");
  const [nom, ...v] = n.split("-");
  return nom.charAt(0).toUpperCase() + nom.slice(1) + (v.length ? " " + v.join(".") : "");
}
function dureeLisible(ms) { return ms >= 60000 ? Math.round(ms / 60000) + " min" : Math.max(1, Math.round(ms / 1000)) + " s"; }
const LIBELLES_STATUT = { travaille: "working", attend: "waiting", termine: "finished", disponible: "available", arrete: "stopped" };
const pastille = (st) => `<span class="pastille ${h(st)}">${t(LIBELLES_STATUT[st] || st)}</span>`;

// what an agent did lately, live: its last actions and how long ago the last one was (a long build or
// test run says nothing for minutes: the agent is still at work)
function activiteVivante(a) {
  if (a.statut !== "travaille" && a.statut !== "attend") return "";
  const depuis = a.derniere ? Math.max(0, Math.round(Date.now() / 1000 - a.derniere)) : null;
  const age = depuis === null ? "" : depuis < 60 ? t("last action {0} s ago", [depuis]) : t("last action {0} min ago", [Math.floor(depuis / 60)]);
  const icone = { outil: "▶", texte: "💬", sous_agent: "👥" };
  const liste = (a.actions || []).slice(-5).map((x) => `<li><span class="muet">${h(x.heure)}</span> ${icone[x.genre] || "•"} ${h(t(x.texte))}</li>`).join("");
  const long = depuis !== null && depuis >= 120 && a.statut === "travaille" ? ` · ${t("a long command may be running (a build, tests): it is still working")}` : "";
  return `<div class="vivant">${age ? `<div class="muet petit">⏱ ${age}${long}</div>` : ""}${liste ? `<ul class="actions-agent">${liste}</ul>` : ""}</div>`;
}

function rendreAppel(a) {
  const cle = "agent:" + a.id, ouvert = etat.ouverts.has(cle);
  const meta = [a.duree_ms && dureeLisible(a.duree_ms), modeleLisible(a.modele)].filter(Boolean).join(" · ");
  const chiffres = [a.outils && (a.outils === 1 ? t("1 tool") : t("{0} tools", [a.outils])), a.jetons && fmtTokens(a.jetons) + " tokens"].filter(Boolean).join(" · ");
  return `<div class="appel ${h(a.statut)} ${ouvert ? "ouvert" : ""}" data-agent-cle="${h(cle)}">
    <div class="appel-ligne"><span class="avatar petit">${h((a.nom || "?").charAt(0).toUpperCase())}</span>
      <div class="appel-corps"><div class="appel-tete"><span class="appel-nom">${h(a.nom)}</span> ${pastille(a.statut)}${meta ? ` <span class="appel-meta">· ${h(meta)}</span>` : ""}</div>
        ${a.mission ? `<div class="appel-mission">${h(a.mission)}</div>` : ""}
        ${a.statut === "travaille" && a.activite ? `<div class="activite">${h(t(a.activite))}</div>` : ""}
        ${activiteVivante(a)}</div>
      <span class="jalon-chevron">${ouvert ? "▾" : "▸"}</span></div>
    ${ouvert ? `<div class="appel-details">
      ${a.bulle && a.statut !== "termine" ? `<div class="bulle">${h(a.bulle.length > 700 ? a.bulle.slice(0, 700) + "…" : a.bulle)}</div>` : ""}
      ${a.consigne ? `<div><div class="muet petit">${t("Brief it was given")}</div><pre class="texte-agent">${h(a.consigne)}</pre></div>` : ""}
      ${a.rapport ? `<div><div class="muet petit">${t("Report it handed back")}</div><pre class="texte-agent">${h(a.rapport)}</pre></div>` : a.statut === "termine" ? "" : `<div class="muet petit">${a.statut === "arrete" ? t("Stopped before handing back a report.") : t("No report yet: it is still working.")}</div>`}
      ${chiffres ? `<div class="muet petit">${h(chiffres)}</div>` : ""}
    </div>` : ""}
  </div>`;
}

function rendreSession(a, appels, nom, mission, avatar) {
  const cleR = "rapport:" + a.id, rapportOuvert = etat.ouverts.has(cleR);
  const meta = [a.statut !== "disponible" && (a.tours === 1 ? t("1 turn") : t("{0} turns", [a.tours || 0])), a.contexte && fmtTokens(a.contexte) + " tokens", modeleLisible(a.modele)].filter(Boolean).join(" · ");
  const fini = a.statut === "termine" && a.rapport, long = fini && a.rapport.length > 600;
  return `<div class="agent ${h(a.genre || "")} ${h(a.statut)}">
    <div class="agent-tete"><div class="avatar">${h(avatar)}</div>
      <div class="agent-identite"><div class="agent-nom">${h(nom)} ${pastille(a.statut)}</div>
        ${mission ? `<div class="agent-mission" title="${h(mission)}">${h(mission)}</div>` : ""}
        ${meta ? `<div class="agent-meta">${h(meta)}</div>` : ""}</div></div>
    ${!fini && a.activite && a.statut !== "disponible" ? `<div class="activite">${h(t(a.activite))}</div>` : ""}
    ${!fini ? activiteVivante(a) : ""}
    ${!fini && a.bulle ? `<div class="bulle">${h(a.bulle.length > 700 ? a.bulle.slice(0, 700) + "…" : a.bulle)}</div>` : ""}
    ${fini ? `<div class="rapport-session" data-agent-cle="${h(cleR)}"><div class="muet petit">${a.genre === "chef" ? t("End of the cycle") : t("Report to the project lead")}</div>
      ${rapportOuvert ? `<pre class="texte-agent">${h(a.rapport)}</pre>` : `<div class="bulle">${h(long ? a.rapport.slice(0, 600) + "…" : a.rapport)}</div>`}
      ${long ? `<span class="rapport-tete petit">${rapportOuvert ? t("Show less") : t("Read it all")}</span>` : ""}</div>` : ""}
    ${appels.length ? `<div class="appels"><div class="appels-titre">${t("Sub-agents called ({0})", [appels.length])}</div>${appels.map(rendreAppel).join("")}</div>` : ""}
  </div>`;
}

// ----------------------------------------------------------------------------- For you: tasks only the user can do
// Claude hands over what it cannot do itself (an account, a payment, a code, a key…); the user answers
// "done" (with a text, files, secret values) or "skip" (Claude finds another way), and sees Claude's verdict.
// Links in Claude's text open in the default browser (POST /api/lien), never inside the application.
function avecLiens(texte) {
  // links are cut from the raw text, then every piece is escaped
  const brut = String(texte || ""), re = /https?:\/\/[^\s<>"'`]+/g;
  let html = "", i = 0, m;
  while ((m = re.exec(brut))) {
    const u = m[0].replace(/[.,;:!?)\]]+$/, "");
    html += h(brut.slice(i, m.index)) + `<a href="#" data-lien="${h(u)}">${h(u)}</a>`;
    i = m.index + u.length; re.lastIndex = i;
  }
  return html + h(brut.slice(i));
}
const ETATS_TACHE = () => ({
  pour_vous: [t("Waits for you"), "pv-vous"],
  a_verifier: [t("Claude checks it"), "pv-claude"],
  a_contourner: [t("Claude looks for another way"), "pv-claude"],
});
function etiquetteTache(x) {
  if (x.etat === "close") return x.statut === "verifiee" ? [t("Verified ✓"), "pv-ok"] : [t("Resolved"), "pv-ok"];
  if (x.etat === "pour_vous" && x.statut === "manque") return [t("Something is missing"), "pv-vous"];
  return ETATS_TACHE()[x.etat] || [x.etat, ""];
}
// drafts belong to the project they were typed for: forgotten when another project is shown
function pvProjet() {
  if (etat.pvProjet === etat.projet) return;
  etat.pvProjet = etat.projet; etat.pvForm = {};
  for (const k of Object.keys(etat.pieces)) if (k.startsWith("pv-")) delete etat.pieces[k];
}
function brouillonTache(id) { pvProjet(); return (etat.pvForm[id] = etat.pvForm[id] || { texte: "", secrets: {} }); }

function carteTache(x) {
  const [libelle, classe] = etiquetteTache(x);
  const rep = x.reponses || [];
  const derniere = rep[rep.length - 1];
  const b = brouillonTache(x.id);
  const meta = [x.jalons && x.jalons.length ? t("blocks {0}", [x.jalons.join(", ")]) : "", heureMaj(x.cree)].filter(Boolean).join(" · ");
  let corps = "";
  if (x.etat === "pour_vous" && x.statut === "manque" && x.note) corps += `<div class="pv-note"><strong>🔁 ${t("Claude: still missing")}</strong><div>${avecLiens(x.note)}</div></div>`;
  if (x.etat !== "close") {
    if (x.details) corps += `<div class="pv-details">${avecLiens(x.details)}</div>`;
    if (x.attendu) corps += `<p class="petit"><strong>${t("What Claude needs back:")}</strong> ${avecLiens(x.attendu)}</p>`;
  }
  if (x.etat === "pour_vous") {
    corps += `<div class="pv-form">
      ${(x.secrets || []).map((n) => `<label class="champ"><span><code>${h(n)}</code> ${x.donnes && x.donnes[n] ? `<span class="pv-donne">✓ ${t("already given: leave empty to keep it")}</span>` : ""}</span>
        <input type="password" autocomplete="off" spellcheck="false" data-pv-secret="${h(x.id)}|${h(n)}" value="${h(b.secrets[n] || "")}" placeholder="${h(t("paste it here: it stays on this PC, in the workshop, never in git"))}"></label>`).join("")}
      <label class="champ"><span>${t("Your answer (optional)")}</span><textarea rows="3" data-pv-texte="${h(x.id)}" placeholder="${h(t("e.g. Done, here is the address you asked for: … Or, to skip: I do not want a paid plan, use a free alternative."))}">${h(b.texte)}</textarea></label>
      ${zonePieces("pv-" + x.id)}
      <div class="ligne mt-1">
        <button class="btn btn-primaire" data-pv="fait" data-id="${h(x.id)}">✓ ${t("It's done")}</button>
        <button class="btn" data-pv="passer" data-id="${h(x.id)}" title="${h(t("Claude will not ask again: it finds another way, guided by your text if you wrote one."))}">⏭ ${t("Skip: find another way")}</button>
      </div></div>`;
  } else if (derniere && x.etat !== "close") {
    corps += `<p class="muet petit">${x.etat === "a_contourner" ? t("You skipped it: Claude finds another way at the start of its next cycle.") : t("You said it is done: Claude checks it at the start of its next cycle.")}${derniere.texte ? " " + h(t("Your answer: {0}", [derniere.texte])) : ""}</p>`;
  } else if (x.etat === "close" && x.note) {
    corps += `<div class="pv-note pv-note-ok">${avecLiens(x.note)}</div>`;
  }
  // the rounds so far: the user's answers and Claude's verdicts
  const tours = [];
  rep.forEach((r) => {
    tours.push(`<li><strong>${t("You")}</strong> · ${h(heureMaj(r.le))} · ${r.action === "passer" ? t("skipped") : t("done")}${r.texte ? ": " + avecLiens(r.texte) : ""}${(r.fichiers || []).length ? ` <span class="muet">(${r.fichiers.map((f) => h(f.split("/").pop())).join(", ")})</span>` : ""}${(r.secrets || []).length ? ` <span class="muet">(${h(t("secret given: {0}", [r.secrets.join(", ")]))})</span>` : ""}</li>`);
    (x.historique || []).filter((j) => j.tour === r.tour).forEach((j) => tours.push(`<li><strong>Claude</strong> · ${h(heureMaj(j.le))} · ${avecLiens(j.note)}</li>`));
  });
  const historique = tours.length > 1 || (tours.length && x.etat === "pour_vous") ? `<details class="mt-1"><summary class="petit">${t("History ({0})", [tours.length])}</summary><ul class="pv-histo petit">${tours.join("")}</ul></details>` : "";
  return `<div class="bloc pv-carte ${x.etat === "close" ? "pv-close" : ""}" data-tache="${h(x.id)}">
    <div class="pv-tete"><span class="badge">${h(x.id)}</span> <strong>${h(x.titre)}</strong> <span class="pv-etat ${classe}">${libelle}</span></div>
    <div class="muet petit">${h(meta)}</div>
    ${corps}${historique}</div>`;
}

pages.pourvous = async () => {
  pvProjet();
  const r = await api("pourvous");
  const l = r.taches || [];
  const actives = l.filter((x) => x.etat !== "close"), closes = l.filter((x) => x.etat === "close");
  etat.pvSignature = JSON.stringify(l);
  return `<div class="bloc"><h2>${t("For you")}</h2>
      <p class="intro petit">${t("What Claude cannot do itself: create an account, pay, receive a code by email or SMS, generate a key… Do it, then answer here: Claude checks it at the start of its next cycle and tells you whether it works. Or skip it, and Claude finds another way.")}</p></div>
    ${actives.length ? actives.map(carteTache).join("") : `<div class="vide">${t("Nothing for you right now: Claude manages on its own. When it needs you, the task appears here and the menu shows it.")}</div>`}
    ${closes.length ? `<details class="bloc"><summary>${t("Done ({0})", [closes.length])}</summary>${closes.map(carteTache).join("")}</details>` : ""}`;
};

async function repondreTache(b) {
  const p = etat.projet, id = b.dataset.id, act = b.dataset.pv, br = brouillonTache(id);
  if (etat.pvProjet !== p) return;
  if (act === "passer" && !(await confirmer(t("Skip this task? Claude will not ask for it again and finds another way, guided by your answer if you wrote one."), { ok: t("Skip it") }))) return;
  b.disabled = true;
  try {
    // files first (they go to .athanor/attachments/for-you-ID/), then the answer that lists them
    const fichiers = [];
    for (const f of etat.pieces["pv-" + id] || []) {
      const r = await fetch("/api/pieces?p=" + encodeURIComponent(p) + "&cible=todo-" + encodeURIComponent(id), {
        method: "POST", headers: { "X-Athanor": CSRF, "X-Nom-Fichier": encodeURIComponent(f.name) }, body: f });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(t("{0} could not be attached: {1}", [f.name, d.erreur_key ? t(d.erreur_key, d.args || []) : t("Error {0}", [r.status])]));
      fichiers.push(d.chemin);
    }
    const r = await action(api("pourvous", "POST", { id, action: act, texte: br.texte, fichiers, secrets: act === "fait" ? br.secrets : {} }));
    if (r) { delete etat.pvForm[id]; etat.pieces["pv-" + id] = []; await rafraichir(); rendrePage(); }
  } catch (e) { toast(e.message, "erreur"); }
  finally { b.disabled = false; }
}

pages.equipe = async () => {
  const r = await api("agents").catch(() => ({}));
  const v = etat.vue || {};
  const agents = r.agents || [], equipe = r.equipe || [];
  const appelsDe = (id) => agents.filter((a) => a.genre === "sous_agent" && a.parent === id);
  const ouvriers = agents.filter((a) => a.genre === "ouvrier");
  const chef = Object.assign({ id: "chef", genre: "chef", statut: v.actif ? "travaille" : "disponible", tours: 0 }, agents.find((a) => a.id === "chef") || {});
  if (!v.actif && chef.statut === "attend" && !chef.tours && !ouvriers.length) chef.statut = "disponible";
  if (ouvriers.length && !chef.tours && chef.statut !== "termine") { chef.statut = "attend"; chef.activite = "Waits for the workers, then verifies and merges their work."; }
  if (!chef.modele) chef.modele = r.modele_chef || "";
  // pilot stopped in the middle of a cycle: nobody is still working
  if (!v.actif) for (const a of [chef, ...agents]) if (a.statut === "travaille" || a.statut === "attend") { a.statut = "arrete"; a.activite = ""; }
  // the pilot runs but no cycle is under way (quota, waiting for you, a pause after an error): say why
  const phase = (v.etat || {}).phase;
  if (v.actif && phase && phase !== "travail" && chef.statut !== "termine") { chef.statut = "attend"; chef.activite = phraseEtat(v).titre; chef.actions = []; chef.derniere = 0; }
  const appelsCycle = {};
  for (const a of agents) if (a.genre === "sous_agent") appelsCycle[a.nom] = (appelsCycle[a.nom] || 0) + 1;
  etat.agPerso = {};
  const membres = equipe.map((m) => { if (m.perso) etat.agPerso[m.nom] = m.perso; return `<div class="membre ${appelsCycle[m.nom] ? "utilise" : ""} ${m.perso ? "perso" : ""}">
      <div class="membre-nom">${h(m.nom)} <span class="muet">· ${m.modele ? h(modeleLisible(m.modele)) : t("lead's model")}</span>${m.perso ? ` <span class="badge">${m.perso.origine === "chef" ? t("proposed by the lead") : t("created by you")}${m.perso.projet ? "" : " · " + t("all projects")}${m.perso.lecture_seule ? " · " + t("read only") : ""}</span>` : ""}</div>
      <div class="membre-role">${h(ROLES[m.nom] ? t(ROLES[m.nom]) : m.mission)}</div>
      <div class="muet petit">${m.appele_par === "chef_ouvriers" ? t("called by the lead and the workers") : t("called by the lead")}${appelsCycle[m.nom] ? " · " + t("called {0}× this cycle", [appelsCycle[m.nom]]) : ""}</div>
      ${m.perso ? `<div class="ligne mt-1"><button class="btn btn-petit" data-ag="modifier" data-nom="${h(m.nom)}">${t("Edit")}</button><button class="btn btn-petit btn-danger" data-ag="supprimer" data-nom="${h(m.nom)}">${t("Remove")}</button></div>` : ""}</div>`; }).join("");
  return `<p class="intro">${t("Who works on this cycle and who called whom. Click a sub-agent to read the brief it was given and the report it handed back.")}</p>
  ${rendreSession(chef, appelsDe("chef"), t("Project lead"), t("Leads the current milestone: reads the plan, codes, checks, commits."), ((w) => (w[0][0] + (w.length > 1 ? w[w.length - 1][0] : w[0][1] || "")).toUpperCase())(t("Project lead").split(/\s+/)))}
  ${ouvriers.length ? `<h3 class="equipe-section">${t("Workers in parallel ({0})", [ouvriers.length])}</h3>
    <div class="equipe">${ouvriers.map((o) => rendreSession(o, appelsDe(o.id), t("Worker " + o.jalon), o.mission || t("Builds milestone " + o.jalon), o.jalon.replace(/_/g, "."))).join("")}</div>` : ""}
  ${membres ? `<h3 class="equipe-section">${t("Who can be called")}</h3><div class="roster">${membres}</div>` : ""}
  ${(r.refuses || []).length ? `<p class="muet petit">${t("Removed proposals of the lead (never proposed again): {0}", [r.refuses.map(h).join(", ")])}</p>` : ""}
  ${formulaireAgent()}`;
};

// A sub-agent of the user's: the lead sees it in its team (.athanor/TEAM.md) and calls it when its
// description fits or when a request names it. Saved in extensions/agents.json (system/equipe.py).
function formulaireAgent() {
  const f = etat.agForm;
  if (!f) return `<div class="ligne mt-2"><button class="btn" data-ag="nouveau">＋ ${t("New sub-agent")}</button>
    <span class="muet petit">${t("A specialist of your own (a translator, a researcher, a level designer…): the lead calls it when its description fits, or when your request names it. The lead can also propose some.")}</span></div>`;
  return `<div class="bloc agent-form mt-2"><h2>${f.ancien ? t("Edit {0}", [f.ancien]) : t("New sub-agent")}</h2>
    <div class="grille grille-2">
      <label class="champ"><span>${t("Name")}</span><input type="text" id="ag-nom" value="${h(f.nom || "")}" placeholder="${h(t("e.g. translator"))}"></label>
      <label class="champ"><span>${t("Model")}</span><select id="ag-modele">
        <option value="" ${!f.modele ? "selected" : ""}>${t("Like the other sub-agents")}</option>
        <option value="eco" ${f.modele === "eco" ? "selected" : ""}>${t("Cheap model (mechanical work)")}</option>
        <option value="fort" ${f.modele === "fort" ? "selected" : ""}>${t("Most capable model (hard reasoning)")}</option></select></label>
    </div>
    <label class="champ"><span>${t("When the lead should call it")}</span><input type="text" id="ag-description" value="${h(f.description || "")}" placeholder="${h(t("e.g. Translates the interface texts into Spanish and German, keeping the tone of the brand."))}"></label>
    <label class="champ"><span>${t("Its instructions")}</span><textarea id="ag-consignes" rows="9" placeholder="${h(t("Its role, how it works, what it must always and never do, what it hands back. Write it as you would brief a colleague: ✨ Improve with Claude can structure it."))}">${h(f.consignes || "")}</textarea></label>
    ${barreAmeliorer("ag-consignes", "agent")}
    <label class="interrupteur"><input type="checkbox" id="ag-lecture" ${f.lecture_seule ? "checked" : ""}><span>${t("Read only: it reads and reports, never writes a file")}</span></label>
    <label class="interrupteur"><input type="checkbox" id="ag-tous" ${f.tous ? "checked" : ""}><span>${t("For all my projects (otherwise this project only)")}</span></label>
    <p class="muet petit">${t("Like the other sub-agents, it works inside the project's sandbox and has its skills, MCP tools and attached files.")}</p>
    <div class="ligne"><button class="btn btn-primaire" data-ag="enregistrer">${t("Save the sub-agent")}</button><button class="btn" data-ag="annuler">${t("Cancel")}</button></div></div>`;
}
async function actionAgent(act, b) {
  if (act === "nouveau") { etat.agForm = { nom: "", description: "", consignes: "", modele: "", lecture_seule: false, tous: false }; return rendrePage(); }
  if (act === "annuler") { etat.agForm = null; return rendrePage(); }
  if (act === "modifier") {
    const a = (etat.agPerso || {})[b.dataset.nom]; if (!a) return;
    etat.agForm = { ancien: a.nom, nom: a.nom, description: a.description, consignes: a.consignes, modele: a.modele, lecture_seule: a.lecture_seule, tous: !a.projet };
    await rendrePage(); const z = $("#ag-nom"); if (z) z.scrollIntoView({ block: "center" }); return;
  }
  if (act === "supprimer") {
    const a = (etat.agPerso || {})[b.dataset.nom]; if (!a) return;
    const texte = a.origine === "chef" ? t("Remove “{0}” from the team? The lead proposed it: it will not propose it again in this project.", [a.nom]) : t("Remove “{0}” from the team?", [a.nom]);
    if (!(await confirmer(texte, { ok: t("Remove"), danger: true }))) return;
    if (await action(api("equipe/agents", "POST", { action: "supprimer", ancien: a.nom }))) rendrePage();
    return;
  }
  if (act === "enregistrer") {
    const f = etat.agForm || {};
    const agent = { nom: $("#ag-nom").value.trim().toLowerCase(), description: $("#ag-description").value, consignes: $("#ag-consignes").value, modele: $("#ag-modele").value, lecture_seule: $("#ag-lecture").checked };
    b.disabled = true;
    const r = await action(api("equipe/agents", "POST", { action: "enregistrer", ancien: f.ancien || "", agent, tous: $("#ag-tous").checked }));
    b.disabled = false;
    if (r) { etat.agForm = null; rendrePage(); }
  }
}

// --- skills & MCP
// A library kept in Athanor (verified catalog, GitHub link, written by hand, pasted configuration) and,
// for the current project, a switch per skill or server. The pilot applies the choice at the next cycle.
const ext = { onglet: "skills", lien: "", trouves: null, voir: null, tests: {}, occupe: "", recherche: { skills: { q: "", res: null }, mcp: { q: "", res: null } }, apercu: null };
pages.extensions = async () => {
  const d = await api("extensions");
  const o = ext.onglet, type = o === "skills" ? "skill" : "mcp";
  const intro = `<p class="intro">${t("Skills give Claude expertise it reads only when it needs it; MCP servers give it new tools (up-to-date documentation, code navigation, a browser…). Install them once in Athanor, then switch on what each project uses: it applies from the next cycle, for the lead and for the parallel workers.")}</p>
  <div class="onglets"><button class="${o === "skills" ? "actif" : ""}" data-ext-onglet="skills">Skills <span class="muet">${d.skills.length}</span></button><button class="${o === "mcp" ? "actif" : ""}" data-ext-onglet="mcp">MCP <span class="muet">${d.mcp.length}</span></button><button class="${o === "plugins" ? "actif" : ""}" data-ext-onglet="plugins">Plugins <span class="muet">${d.plugins || 0}</span></button></div>`;
  if (o === "plugins") return intro + await pagePlugins(d);
  const liste = o === "skills" ? d.skills : d.mcp;
  const cat = (o === "skills" ? d.catalogue.skills : d.catalogue.mcp) || [];
  const occupe = (cle) => ext.occupe === cle ? "disabled" : "";
  const secrets = (x) => champsServeur(x);
  const test = (x) => { const r = ext.tests[x.id]; if (!r) return ""; if (r === "…") return `<div class="muet petit">${x.type === "windows" ? t("Starting the Windows program…") : t("Starting the server in its cage… (the first time, it downloads it: up to two minutes)")}</div>`;
    return r.ok ? `<div class="ext-test ok">✓ ${r.outils && r.outils.length ? t("Works: {0} tools ({1})", [r.outils.length, h(r.outils.slice(0, 8).join(", ")) + (r.outils.length > 8 ? "…" : "")]) : t(r.note || "Works.")}</div>`
      : `<div class="ext-test echec">✕ ${h(t(r.erreur || "Failed"))}${r.details && r.details.length ? `<pre class="texte-agent">${h(r.details.join("\n"))}</pre>` : ""}</div>`; };
  const voir = (x) => ext.voir && ext.voir.id === x.id && ext.voir.type === type ? `<div class="ext-voir">
      ${type === "skill" ? `<textarea class="code" rows="14" data-ext-texte="${h(x.id)}">${h(ext.voir.texte)}</textarea>
        ${ext.voir.fichiers && ext.voir.fichiers.length > 1 ? `<div class="muet petit">${t("Files")}: ${ext.voir.fichiers.map((f) => `<code>${h(f)}</code>`).join(" ")}</div>` : ""}
        <div class="ligne"><button class="btn btn-petit" data-ext-enregistrer="${h(x.id)}">${t("Save the skill")}</button></div>`
        : ext.voir.modifiable ? `<textarea class="code" rows="16" spellcheck="false" data-ext-texte="${h(x.id)}">${h(ext.voir.texte)}</textarea>
        <div class="muet petit">${t("“reglages”: name → default value (the value used is in its field below the server). “ouvrir”: the program the pilot opens for this server before each cycle: “programme” (blender, roblox-studio, unreal-editor, or C:\\…\\name.exe), “args” ({FICHIER}: the project's file), “fichiers” (e.g. *.blend), “sans_fichier”, “processus”, the “port” to wait for, “delai” in seconds. ${NAME}: a setting or a key.")}</div>
        <div class="ligne"><button class="btn btn-petit" data-ext-mcp-enregistrer="${h(x.id)}">${t("Save the server")}</button></div>`
        : `<pre class="texte-agent">${h(ext.voir.texte)}</pre>`}</div>` : "";
  const lignes = liste.map((x) => `<div class="ext-ligne ${x.actif ? "actif" : ""}">
      <label class="interrupteur" title="${h(d.projet ? t("Used by “{0}”", [d.projet]) : t("Choose a project first"))}"><input type="checkbox" data-ext-actif="${type}|${h(x.id)}" ${x.actif ? "checked" : ""} ${d.projet && !x.plugin ? "" : "disabled"}></label>
      <div class="ext-corps">
        <div class="ext-nom">${h(x.nom)}${x.scripts ? ` <span class="etiquette" title="${h(t("Contains scripts: Claude runs them inside its sandbox."))}">${t("scripts")}</span>` : ""}${!x.source && !x.catalogue ? ` <span class="etiquette">${t("yours")}</span>` : ""}${x.type ? ` <span class="etiquette" ${x.type === "windows" ? `title="${h(t("Runs on Windows, outside the cage, with your rights: Claude only sees its tools."))}"` : ""}>${genreServeur(x.type)}</span>` : ""}${x.plugin ? ` <span class="etiquette" title="${h(t("On and off with its plugin (Plugins tab)"))}">${t("plugin {0}", [h(x.plugin)])}</span>` : ""}</div>
        ${x.description ? `<div class="ext-desc">${h(x.description)}</div>` : ""}
        <div class="muet petit ext-source">${[x.source ? h(x.source.startsWith("registry:") ? t("MCP Registry") + " · " + x.source.slice(9) : x.source.replace(/^github\.com\/https?:\/\//, "")) : x.catalogue ? "" : h(t("written in Athanor")), x.resume ? `<code>${h(x.resume)}</code>` : ""].filter(Boolean).join(" · ")}</div>
        ${secrets(x)}${test(x)}${voir(x)}
      </div>
      <div class="ext-actions"><button class="btn btn-petit" data-ext-voir="${type}|${h(x.id)}">${ext.voir && ext.voir.id === x.id && ext.voir.type === type ? t("Close") : type === "skill" || !x.plugin ? t("View / edit") : t("View")}</button>
        ${type === "mcp" && (x.type === "http" || x.type === "sse") && !/^https?:\/\/(localhost|127\.|0\.0\.0\.0|\[::1\]|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(x.resume || "") ? `<button class="btn btn-petit" data-ext-connexion="${h(x.id)}" title="${h(t("For a server that asks you to sign in (OAuth): once, valid for every project."))}">${t("Sign in")}</button>` : ""}
        ${type === "mcp" ? `<button class="btn btn-petit" data-ext-tester="${h(x.id)}" ${d.projet ? "" : "disabled"} ${ext.tests[x.id] === "…" ? "disabled" : ""}>${t("Test")}</button>` : ""}
        <button class="btn btn-petit btn-danger" data-ext-retirer="${type}|${h(x.id)}" title="${h(t("Remove"))}">✕</button></div>
    </div>`).join("");
  const cartes = cat.map((c) => `<div class="ext-carte">
      <div class="ext-nom">${h(c.nom)} <span class="muet petit">${h(c.licence)}</span></div>
      <div class="ext-desc">${h(c.description)}</div>
      <div class="muet petit">${[c.depot ? h(c.depot.includes("://") ? c.depot.split("://")[1] : "github.com/" + c.depot) : "", c.prerequis ? t("needs {0}", [c.prerequis]) : ""].filter(Boolean).join(" · ")}${(c.secrets || []).length ? " · " + c.secrets.map((sc) => sc.optionnel ? t("optional key") : t("key required")).join(", ") : ""}${c.scripts ? " · " + t("scripts") : ""}</div>
      <div class="ligne">${c.installe ? `<span class="ok petit">✓ ${t("Installed")}</span>` : `<button class="btn btn-petit btn-primaire" data-ext-installer="${type}|${h(c.id)}" ${occupe(type + "|" + c.id)}>${ext.occupe === type + "|" + c.id ? t("Installing…") : t("Install")}</button>`}</div>
    </div>`).join("");
  // search: SkillsMP for skills, the official MCP Registry for servers
  const rech = ext.recherche[o];
  const resultats = !rech.res ? "" : !rech.res.length ? `<div class="vide">${t("Nothing found: try another word, in English (e.g. testing, react, docs).")}</div>` : rech.res.map((x, i) => o === "skills" ? `<div class="ext-ligne">
      <div class="ext-corps"><div class="ext-nom">${h(x.nom)} <span class="muet petit">${x.auteur ? t("by {0}", [h(x.auteur)]) : ""}${x.etoiles ? " · ★ " + x.etoiles : ""}</span></div>
        ${x.description ? `<div class="ext-desc">${h(x.description)}</div>` : ""}<div class="muet petit ext-source">${h(x.url.replace(/^https:\/\//, ""))}</div>
        ${ext.apercu && ext.apercu.i === i ? (ext.apercu.texte === "…" ? `<div class="muet petit">${t("Downloading…")}</div>` : `<pre class="texte-agent ext-apercu">${h(ext.apercu.texte)}</pre>`) : ""}</div>
      <div class="ext-actions"><button class="btn btn-petit" data-ext-apercu="${i}">${ext.apercu && ext.apercu.i === i ? t("Close") : t("View")}</button>
        ${x.installe ? `<span class="ok petit">✓ ${t("Installed")}</span>` : `<button class="btn btn-petit btn-primaire" data-ext-installer-res="${i}" ${occupe("res|" + i)}>${ext.occupe === "res|" + i ? t("Installing…") : t("Install")}</button>`}</div></div>`
    : `<div class="ext-ligne">
      <div class="ext-corps"><div class="ext-nom">${h(x.titre)} <span class="muet petit">${h(x.version || "")}</span>${x.genre ? ` <span class="etiquette">${x.genre === "remote" ? t("remote") : t("local, in a cage") + " · " + h(x.genre)}</span>` : ""}</div>
        ${x.description ? `<div class="ext-desc">${h(x.description)}</div>` : ""}
        <div class="muet petit ext-source">${h(x.nom)}${x.depot ? " · " + h(x.depot.replace(/^https:\/\//, "")) : ""}${x.cles && x.cles.length ? " · " + t("keys: {0}", [x.cles.join(", ")]) : ""}</div>
        ${x.installable ? "" : `<div class="muet petit">${h(t(x.raison || ""))}</div>`}</div>
      <div class="ext-actions">${x.installe ? `<span class="ok petit">✓ ${t("Installed")}</span>` : x.installable ? `<button class="btn btn-petit btn-primaire" data-ext-installer-res="${i}" ${occupe("res|" + i)}>${ext.occupe === "res|" + i ? t("Installing…") : t("Install")}</button>` : ""}</div></div>`).join("");
  const blocRecherche = `<div class="bloc"><h2>${o === "skills" ? t("Search the community") : t("Search the MCP Registry")}</h2>
    <p class="muet petit">${o === "skills" ? t("Thousands of skills published on GitHub, found through SkillsMP. Not checked by hand: read a skill (View) before installing it.") : t("The official directory of MCP servers. Not checked by hand: only install servers you trust; local ones run in a cage.")}</p>
    <div class="ligne"><input type="text" id="ext-q" class="large" value="${h(rech.q)}" placeholder="${h(o === "skills" ? t("e.g. front-end, testing, react, documentation") : t("e.g. github, database, figma, search"))}"><button class="btn" data-ext-rechercher ${occupe("recherche")}>${ext.occupe === "recherche" ? t("Looking…") : t("Search")}</button></div>
    ${resultats ? `<div class="ext-resultats">${resultats}</div>` : ""}</div>`;
  const ajouter = o === "skills" ? `
      <h3>${t("From a GitHub link")}</h3>
      <p class="muet petit">${t("A repository or a folder holding one or several skills (a SKILL.md each), e.g. https://github.com/anthropics/skills/tree/main/skills/webapp-testing")}</p>
      <div class="ligne"><input type="text" id="ext-lien" class="large" value="${h(ext.lien)}" placeholder="https://github.com/…"><button class="btn" data-ext-chercher ${occupe("github")}>${ext.occupe === "github" ? t("Looking…") : t("Look")}</button></div>
      ${ext.trouves ? `<div class="ext-trouves">${ext.trouves.skills.map((x, i) => `<label class="interrupteur"><input type="checkbox" data-ext-trouve="${i}" ${x.installe ? "" : "checked"}><span><b>${h(x.nom)}</b>${x.installe ? ` <span class="muet petit">(${t("already installed: it will be updated")})</span>` : ""}${x.scripts ? ` <span class="etiquette">${t("scripts")}</span>` : ""}<br><span class="muet petit">${h(x.description || "")}</span></span></label>`).join("")}
        <div class="ligne"><button class="btn btn-primaire" data-ext-installer-trouves ${occupe("github")}>${t("Install the selected skills")}</button></div></div>` : ""}
      <h3>${t("Write a skill")}</h3>
      <div class="grille grille-2"><input type="text" id="ext-nom" placeholder="${h(t("Name, e.g. house-style"))}"><input type="text" id="ext-description" placeholder="${h(t("When Claude should use it, e.g. Use when writing CSS for this site."))}"></div>
      <textarea id="ext-contenu" rows="6" class="mt-1" placeholder="${h(t("The instructions, in Markdown: rules, steps, examples…"))}"></textarea>
      <div class="ligne mt-1"><button class="btn" data-ext-creer>${t("Create the skill")}</button></div>`
    : `
      <h3>${t("Paste a configuration")}</h3>
      <p class="muet petit">${t("The JSON block or the “claude mcp add …” command given in the server's README. API keys found in it are stored apart, never shown again.")}</p>
      <div class="grille grille-2"><input type="text" id="ext-mcp-nom" placeholder="${h(t("Name (optional)"))}"><input type="text" id="ext-mcp-source" placeholder="${h(t("Link to its GitHub page (optional)"))}"></div>
      <textarea id="ext-mcp-texte" rows="7" class="code mt-1" placeholder='{"mcpServers": {"name": {"command": "npx", "args": ["-y", "…"]}}}'></textarea>
      <label class="interrupteur mt-1"><input type="checkbox" id="ext-mcp-windows"><span>${t("Windows program (a .exe, a .bat, a C:\\ path): it runs on Windows, outside the cage, with your rights")}</span></label>
      <div class="ligne mt-1"><button class="btn" data-ext-mcp-ajouter>${t("Add the server")}</button></div>`;
  return `${intro}
  <div class="bloc"><h2>${t("In Athanor")}</h2>
    <p class="muet petit">${d.projet ? t("The switch turns it on or off for “{0}”.", [d.projet]) : t("Choose a project to switch them on or off.")}</p>
    ${lignes || `<div class="vide">${t("Nothing installed yet: pick from the catalog below or add your own.")}</div>`}</div>
  ${blocRecherche}
  <div class="bloc"><h2>${t("Verified catalog")}</h2>
    <p class="muet petit">${t("Well-known open-source projects, checked by hand. Installing copies them into Athanor; nothing is used until you switch it on for a project.")}</p>
    <div class="ext-catalogue">${cartes}</div></div>
  <div class="bloc"><h2>${t("Add your own")}</h2>
    <div class="ext-avert">⚠ ${o === "skills" ? t("A skill guides Claude: only add skills from sources you trust. Read it first (View / edit).") : t("An MCP server runs code on your PC: only add servers you trust. Local servers run in a cage: no access to your personal files, your Windows drives or the Claude token.")}</div>
    ${ajouter}</div>`;
};

// --- plugins (Skills & MCP page, Plugins tab)
// Claude Code plugins from marketplaces (the official one, plus those the user adds): installed once in
// Athanor, switched on per project. Their MCP servers join the library: keys and sign-in are given once
// here, for every project. Hooks and LSP servers are left out (they would run outside the sandbox).
const plg = { q: "", categorie: "", n: 60, occupe: "", chargeAuto: false, marche: "" };
function resumePlugin(x) {
  const n = (l) => (l || []).length;
  return [n(x.skills) && t("{0} skill(s)", [n(x.skills)]), n(x.commandes) && t("{0} command(s)", [n(x.commandes)]), n(x.agents) && t("{0} sub-agent(s)", [n(x.agents)]), n(x.mcp) && t("{0} MCP server(s)", [n(x.mcp)])].filter(Boolean).join(" · ");
}
// Where a server runs: in a cage (Linux), on Windows (a Windows program), or online.
function genreServeur(type) {
  return type === "stdio" ? t("local, in a cage") : type === "windows" ? t("Windows program") : t("remote");
}
// The keys (hidden, asked once for the whole application) and the settings (an address, a port, a path:
// shown in clear, with their default) of an MCP server. Both are saved by the same button.
function champsServeur(x) {
  return (x.secrets || []).map((sc) => sc.reglage ? `<div class="ext-secret ext-reglage">
      <code>${h(sc.nom)}</code> <span class="petit muet">${t("setting")}${sc.defaut && sc.valeur !== sc.defaut ? " · " + t("default: {0}", [h(sc.defaut)]) : ""}</span>
      <input type="text" autocomplete="off" spellcheck="false" data-ext-cle="${h(x.id)}|${h(sc.nom)}" value="${h(sc.valeur || "")}" placeholder="${h(sc.defaut || "")}">
      <button class="btn btn-petit" data-ext-cle-ok="${h(x.id)}|${h(sc.nom)}">${t("Save")}</button></div>`
    : `<div class="ext-secret ${sc.defini || sc.optionnel ? "" : "manquant"}">
      <code>${h(sc.nom)}</code> <span class="petit">${sc.defini ? "✓ " + t("key saved") : sc.optionnel ? t("optional key: better limits") : t("key required: the server is skipped without it")}</span>
      <input type="password" autocomplete="off" data-ext-cle="${h(x.id)}|${h(sc.nom)}" placeholder="${h(sc.defini ? t("Replace the key") : t("Paste the key"))}">
      <button class="btn btn-petit" data-ext-cle-ok="${h(x.id)}|${h(sc.nom)}">${t("Save")}</button>${sc.lien ? ` <span class="muet petit">${h(sc.lien.replace(/^https:\/\//, ""))}</span>` : ""}</div>`).join("");
}
function serveurPlugin(x, d) {
  const sc = champsServeur(x);
  const distant = x.type === "http" || x.type === "sse";
  return `<div class="plg-serveur"><div class="ligne"><span>🔌 <b>${h(x.nom)}</b> <span class="etiquette">${genreServeur(x.type)}</span></span>
      <span class="espace"></span>${distant ? `<button class="btn btn-petit" data-ext-connexion="${h(x.id)}" title="${h(t("For a server that asks you to sign in (OAuth): once, valid for every project."))}">${t("Sign in")}</button>` : ""}
      <button class="btn btn-petit" data-ext-tester="${h(x.id)}" ${d.projet && ext.tests[x.id] !== "…" ? "" : "disabled"}>${t("Test")}</button></div>${sc}${testExt(x)}</div>`;
}
function testExt(x) {
  const r = ext.tests[x.id]; if (!r) return "";
  if (r === "…") return `<div class="muet petit">${x.type === "windows" ? t("Starting the Windows program…") : t("Starting the server in its cage… (the first time, it downloads it: up to two minutes)")}</div>`;
  return r.ok ? `<div class="ext-test ok">✓ ${r.outils && r.outils.length ? t("Works: {0} tools ({1})", [r.outils.length, h(r.outils.slice(0, 8).join(", ")) + (r.outils.length > 8 ? "…" : "")]) : t(r.note || "Works.")}</div>`
    : `<div class="ext-test echec">✕ ${h(t(r.erreur || "Failed"))}${r.details && r.details.length ? `<pre class="texte-agent">${h(r.details.join("\n"))}</pre>` : ""}</div>`;
}
async function pagePlugins(d) {
  let p = await api("plugins");
  const officiel = (p.marches || [])[0];
  if (officiel && !officiel.charge && !officiel.erreur && !plg.chargeAuto) {
    // the first visit: the official catalog is downloaded once (then on "Refresh")
    plg.chargeAuto = true;
    await api("plugins/marche", "POST", { action: "actualiser", id: officiel.id }).catch(() => {});
    p = await api("plugins");
  }
  const occ = (c) => plg.occupe === c ? "disabled" : "";
  const serveurs = {}; for (const x of d.mcp || []) if (x.plugin) (serveurs[x.plugin] = serveurs[x.plugin] || []).push(x);
  const installes = (p.plugins || []).map((x) => `<div class="ext-ligne ${x.actif ? "actif" : ""}">
      <label class="interrupteur" title="${h(d.projet ? t("Used by “{0}”", [d.projet]) : t("Choose a project first"))}"><input type="checkbox" data-plg-actif="${h(x.id)}" ${x.actif ? "checked" : ""} ${d.projet ? "" : "disabled"}></label>
      <div class="ext-corps">
        <div class="ext-nom">${h(x.nom)} ${x.version ? `<span class="muet petit">${h(x.version)}</span>` : ""} ${x.auteur ? `<span class="muet petit">· ${t("by {0}", [h(x.auteur)])}</span>` : ""}</div>
        ${x.description ? `<div class="ext-desc">${h(x.description)}</div>` : ""}
        <div class="muet petit ext-source">${h(resumePlugin(x))}${x.source ? " · " + h(x.source) : ""}</div>
        ${(x.ecartes || []).length ? `<div class="muet petit">⚠ ${t("Left out, they would run outside Claude's sandbox: {0}", [x.ecartes.join(", ")])}</div>` : ""}
        ${(serveurs[x.id] || []).map((s) => serveurPlugin(s, d)).join("")}
      </div>
      <div class="ext-actions"><button class="btn btn-petit" data-plg-maj data-plg-installer="${h(x.marche)}|${h(x.entree || x.id)}" ${occ("i|" + x.marche + "|" + (x.entree || x.id))} title="${h(t("Download its latest version"))}">${plg.occupe === "i|" + x.marche + "|" + (x.entree || x.id) ? t("Updating…") : t("Update")}</button>
        <button class="btn btn-petit btn-danger" data-plg-retirer="${h(x.id)}" title="${h(t("Remove"))}">✕</button></div></div>`).join("");
  // the catalog: every plugin of every marketplace, filtered here
  const q = plg.q.toLowerCase(), cat = p.catalogue || [];
  const categories = [...new Set(cat.map((x) => x.categorie).filter(Boolean))].sort();
  const filtres = cat.filter((x) => (!plg.categorie || x.categorie === plg.categorie) && (!plg.marche || x.marche === plg.marche)
    && (!q || (x.nom + " " + x.description + " " + (x.auteur || "")).toLowerCase().includes(q)));
  const cartes = filtres.slice(0, plg.n).map((x) => `<div class="ext-carte">
      <div class="ext-nom">${h(x.nom)} ${x.categorie ? `<span class="etiquette">${h(x.categorie)}</span>` : ""}</div>
      <div class="ext-desc">${h(x.description)}</div>
      <div class="muet petit">${[x.auteur && t("by {0}", [h(x.auteur)]), x.source && h(x.source)].filter(Boolean).join(" · ")}</div>
      ${x.raison ? `<div class="muet petit">${h(t(x.raison))}</div>` : ""}
      <div class="ligne">${x.installe ? `<span class="ok petit">✓ ${t("Installed")}</span>` : x.raison ? "" : `<button class="btn btn-petit btn-primaire" data-plg-installer="${h(x.marche)}|${h(x.nom)}" ${occ("i|" + x.marche + "|" + x.nom)}>${plg.occupe === "i|" + x.marche + "|" + x.nom ? t("Installing…") : t("Install")}</button>`}
        ${x.homepage && /^https:\/\//.test(x.homepage) ? `<a href="#" class="petit" data-lien="${h(x.homepage)}">${t("Web page")}</a>` : ""}</div></div>`).join("");
  const marches = (p.marches || []).map((m) => `<div class="ligne petit"><span><b>${h(m.nom)}</b> <span class="muet">${m.depot ? "github.com/" + h(m.depot) : h(m.url || "")} · ${m.charge ? t("{0} plugin(s)", [m.n]) : m.erreur ? h(t(m.erreur)) : t("not loaded yet")}${m.maj ? " · " + t("updated {0}", [h(m.maj)]) : ""}</span></span><span class="espace"></span>
      <button class="btn btn-petit" data-plg-marche="actualiser|${h(m.id)}" ${occ("m|" + m.id)}>${plg.occupe === "m|" + m.id ? t("Loading…") : t("Refresh")}</button>
      ${m.id === "claude-plugins-official" ? "" : `<button class="btn btn-petit btn-danger" data-plg-marche="retirer|${h(m.id)}" title="${h(t("Remove"))}">✕</button>`}</div>`).join("");
  return `<div class="bloc"><h2>${t("In Athanor")}</h2>
      <p class="muet petit">${d.projet ? t("The switch turns it on or off for “{0}”.", [d.projet]) : t("Choose a project to switch them on or off.")} ${t("A plugin brings skills, commands, sub-agents and MCP servers at once. Keys and sign-ins are given once here, for every project.")}</p>
      ${installes || `<div class="vide">${t("No plugin installed yet: pick one from the catalog below.")}</div>`}</div>
    <div class="bloc"><h2>${t("Catalog")} <span class="muet petit">${cat.length}</span></h2>
      <p class="muet petit">${t("The plugins of Claude Code's official marketplace and of the marketplaces you add. Not checked by Athanor: install what you trust. Their hooks and LSP servers are always left out; their MCP servers run like the others (local ones in a cage).")}</p>
      <div class="ligne"><input type="text" id="plg-q" class="large" value="${h(plg.q)}" placeholder="${h(t("Search: shopify, stripe, notion, figma, database…"))}">
        <select id="plg-categorie"><option value="">${t("All categories")}</option>${categories.map((c) => `<option value="${h(c)}" ${plg.categorie === c ? "selected" : ""}>${h(c)}</option>`).join("")}</select>
        ${(p.marches || []).length > 1 ? `<select id="plg-marche"><option value="">${t("All marketplaces")}</option>${p.marches.map((m) => `<option value="${h(m.id)}" ${plg.marche === m.id ? "selected" : ""}>${h(m.nom)}</option>`).join("")}</select>` : ""}</div>
      <div class="muet petit mt-1">${t("{0} plugin(s) found", [filtres.length])}</div>
      <div class="ext-catalogue mt-1">${cartes || `<div class="vide">${cat.length ? t("Nothing found: try another word.") : t("The catalog is not loaded: Refresh below.")}</div>`}</div>
      ${filtres.length > plg.n ? `<div class="ligne mt-1"><button class="btn" data-plg-plus>${t("Show more")}</button></div>` : ""}</div>
    <div class="bloc"><h2>${t("Marketplaces")}</h2>${marches}
      <h3>${t("Add a marketplace")}</h3>
      <p class="muet petit">${t("A GitHub repository holding .claude-plugin/marketplace.json (owner/repo or its link), or the https address of a marketplace.json.")}</p>
      <div class="ligne"><input type="text" id="plg-marche-texte" class="large" placeholder="owner/repo"><button class="btn" data-plg-marche="ajouter|" ${occ("m|")}>${plg.occupe === "m|" ? t("Loading…") : t("Add")}</button></div></div>`;
}
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "extensions") return;
  const b = ev.target.closest("[data-plg-installer], [data-plg-retirer], [data-plg-marche], [data-plg-plus], [data-ext-connexion]"); if (!b) return;
  const d = b.dataset;
  if (d.plgPlus !== undefined) { plg.n += 60; return rendrePage(); }
  if (d.extConnexion) return action(api("extensions/connexion", "POST", { id: d.extConnexion }));
  if (d.plgInstaller) {
    const [marche, ...nom] = d.plgInstaller.split("|");
    plg.occupe = "i|" + d.plgInstaller; await rendrePage();
    // an update first reads the marketplace again: it gives the plugin's latest commit
    if (d.plgMaj !== undefined) await api("plugins/marche", "POST", { action: "actualiser", id: marche }).catch(() => {});
    await action(api("plugins/installer", "POST", { marche, nom: nom.join("|") })); plg.occupe = ""; return rendrePage();
  }
  if (d.plgRetirer) {
    if (!(await confirmer(t("Remove the plugin “{0}” from Athanor? It is switched off in every project, with its MCP servers and their keys.", [d.plgRetirer]), { ok: t("Remove"), danger: true }))) return;
    if (await action(api("plugins/retirer", "POST", { id: d.plgRetirer }))) rendrePage(); return;
  }
  if (d.plgMarche) {
    const [act, id] = d.plgMarche.split("|");
    if (act === "retirer" && !(await confirmer(t("Remove this marketplace? The plugins installed from it stay."), { ok: t("Remove"), danger: true }))) return;
    const texte = act === "ajouter" ? $("#plg-marche-texte").value.trim() : "";
    if (act === "ajouter" && !texte) return;
    plg.occupe = "m|" + (id || ""); await rendrePage();
    await action(api("plugins/marche", "POST", { action: act, id, texte })); plg.occupe = ""; return rendrePage();
  }
});
$("#page").addEventListener("input", (ev) => {
  if (ev.target.id !== "plg-q") return;
  plg.q = ev.target.value; plg.n = 60;
  clearTimeout(plg.minuterie); plg.minuterie = setTimeout(async () => { const pos = ev.target.selectionStart; await rendrePage(); const z = $("#plg-q"); if (z) { z.focus(); z.setSelectionRange(pos, pos); } }, 250);
});
$("#page").addEventListener("change", async (ev) => {
  const i = ev.target;
  if (i.id === "plg-categorie") { plg.categorie = i.value; plg.n = 60; return rendrePage(); }
  if (i.id === "plg-marche") { plg.marche = i.value; plg.n = 60; return rendrePage(); }
  if (!i.dataset.plgActif) return;
  const r = await action(api("plugins/projet", "POST", { id: i.dataset.plgActif, actif: i.checked }));
  if (!r) i.checked = !i.checked; else rendrePage();
});

$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "extensions") return;
  const b = ev.target.closest("[data-ext-rechercher], [data-ext-apercu], [data-ext-installer-res], [data-ext-onglet], [data-ext-installer], [data-ext-chercher], [data-ext-installer-trouves], [data-ext-creer], [data-ext-mcp-ajouter], [data-ext-voir], [data-ext-enregistrer], [data-ext-mcp-enregistrer], [data-ext-tester], [data-ext-retirer], [data-ext-cle-ok]");
  if (!b) return;
  const d = b.dataset;
  if (d.extOnglet) { ext.onglet = d.extOnglet; ext.voir = null; ext.apercu = null; return rendrePage(); }
  if (d.extRechercher !== undefined) {
    const o = ext.onglet, q = $("#ext-q").value.trim(); if (!q) return;
    ext.recherche[o].q = q; ext.occupe = "recherche"; ext.apercu = null; await rendrePage();
    const r = await action(api("extensions/recherche", "POST", { type: o === "skills" ? "skill" : "mcp", q }));
    ext.occupe = ""; ext.recherche[o].res = r ? r.resultats : ext.recherche[o].res; return rendrePage();
  }
  if (d.extApercu !== undefined) {
    const i = Number(d.extApercu), x = ext.recherche.skills.res[i];
    if (ext.apercu && ext.apercu.i === i) { ext.apercu = null; return rendrePage(); }
    ext.apercu = { i, texte: "…" }; await rendrePage();
    const r = await action(api("extensions/github", "POST", { url: x.url, nom: x.nom, apercu: true }));
    ext.apercu = r ? { i, texte: r.texte || "" } : null; return rendrePage();
  }
  if (d.extInstallerRes !== undefined) {
    const o = ext.onglet, i = Number(d.extInstallerRes), x = ext.recherche[o].res[i];
    ext.occupe = "res|" + i; await rendrePage();
    const r = o === "skills" ? await action(api("extensions/github", "POST", { url: x.url, nom: x.nom })) : await action(api("extensions/registre", "POST", { nom: x.nom }));
    ext.occupe = ""; if (r) x.installe = true; return rendrePage();
  }
  if (d.extInstaller) {
    const [type, id] = d.extInstaller.split("|"); ext.occupe = d.extInstaller; await rendrePage();
    await action(api("extensions/catalogue", "POST", { type, id })); ext.occupe = ""; return rendrePage();
  }
  if (d.extChercher !== undefined) {
    ext.lien = $("#ext-lien").value.trim(); if (!ext.lien) return;
    ext.occupe = "github"; ext.trouves = null; await rendrePage();
    const r = await action(api("extensions/github", "POST", { url: ext.lien }));
    ext.occupe = ""; ext.trouves = r && r.skills ? r : null; return rendrePage();
  }
  if (d.extInstallerTrouves !== undefined && ext.trouves) {
    const chemins = [...document.querySelectorAll("[data-ext-trouve]")].filter((c) => c.checked).map((c) => ext.trouves.skills[Number(c.dataset.extTrouve)].chemin);
    if (!chemins.length) return;
    ext.occupe = "github"; await rendrePage();
    const r = await action(api("extensions/github", "POST", { url: ext.lien, chemins }));
    ext.occupe = ""; if (r) { ext.trouves = null; ext.lien = ""; } return rendrePage();
  }
  if (d.extCreer !== undefined) {
    const r = await action(api("extensions/skill", "POST", { nom: $("#ext-nom").value, description: $("#ext-description").value, contenu: $("#ext-contenu").value }));
    if (r) rendrePage(); return;
  }
  if (d.extMcpAjouter !== undefined) {
    const r = await action(api("extensions/mcp", "POST", { nom: $("#ext-mcp-nom").value, source: $("#ext-mcp-source").value, texte: $("#ext-mcp-texte").value, windows: $("#ext-mcp-windows").checked }));
    if (r) rendrePage(); return;
  }
  if (d.extVoir) {
    const [type, id] = d.extVoir.split("|");
    if (ext.voir && ext.voir.id === id && ext.voir.type === type) { ext.voir = null; return rendrePage(); }
    const r = await action(api(`extensions/voir?type=${type}&id=${encodeURIComponent(id)}`));
    if (r) { ext.voir = { type, id, texte: r.texte, fichiers: r.fichiers, modifiable: !!r.modifiable }; rendrePage(); } return;
  }
  if (d.extMcpEnregistrer) {
    const texte = document.querySelector(`[data-ext-texte="${CSS.escape(d.extMcpEnregistrer)}"]`).value;
    const r = await action(api("extensions/mcp/modifier", "POST", { id: d.extMcpEnregistrer, texte }));
    if (r) { ext.voir = null; rendrePage(); } return;
  }
  if (d.extEnregistrer) {
    const texte = document.querySelector(`[data-ext-texte="${CSS.escape(d.extEnregistrer)}"]`).value;
    const r = await action(api("extensions/skill", "POST", { id: d.extEnregistrer, contenu: texte }));
    if (r) { ext.voir = null; rendrePage(); } return;
  }
  if (d.extTester) {
    const id = d.extTester; ext.tests[id] = "…"; await rendrePage();
    ext.tests[id] = await api("extensions/tester", "POST", { id }).catch((e) => ({ ok: false, erreur: e.message }));
    return rendrePage();
  }
  if (d.extCleOk) {
    const [id, nom] = d.extCleOk.split("|"); const champ = document.querySelector(`[data-ext-cle="${CSS.escape(d.extCleOk)}"]`);
    const r = await action(api("extensions/secret", "POST", { id, nom, valeur: champ.value })); if (r) rendrePage(); return;
  }
  if (d.extRetirer) {
    const [type, id] = d.extRetirer.split("|");
    if (!(await confirmer(t("Remove “{0}” from Athanor? It is switched off in every project.", [id]), { ok: t("Remove"), danger: true }))) return;
    const r = await action(api("extensions/retirer", "POST", { type, id })); if (r) { delete ext.tests[id]; rendrePage(); } return;
  }
});
$("#page").addEventListener("change", async (ev) => {
  const c = ev.target.closest("[data-ext-actif]"); if (!c) return;
  const [type, id] = c.dataset.extActif.split("|");
  const r = await action(api("extensions/projet", "POST", { type, id, actif: c.checked }));
  if (!r) c.checked = !c.checked; else c.closest(".ext-ligne").classList.toggle("actif", c.checked);
});
$("#page").addEventListener("keydown", (ev) => {
  if (ev.key === "Enter" && ev.target.id === "ext-lien") { ev.preventDefault(); document.querySelector("[data-ext-chercher]").click(); }
  if (ev.key === "Enter" && ev.target.id === "ext-q") { ev.preventDefault(); document.querySelector("[data-ext-rechercher]").click(); }
});

// --- requests
// --- attached files: kept in the browser until the request is saved, then sent to the workshop
// (.athanor/attachments/), listed in the request for Claude
const TAILLE_PIECE_MAX = 50 * 1024 * 1024;
function tailleCourte(n) { return n >= 1048576 ? (n / 1048576).toFixed(1) + " MB" : n >= 1024 ? Math.ceil(n / 1024) + " KB" : n + " B"; }
function zonePieces(id) {
  return `<div class="pieces" data-pieces="${id}">
    <input type="file" multiple hidden id="${id}-fichiers">
    <div class="ligne"><button type="button" class="btn btn-petit" data-choisir-pieces="${id}">📎 ${t("Attach files")}</button>
      <span class="muet petit">${t("or drop them here: images, PDF, text, documents, data (50 MB at most each)")}</span></div>
    <ul class="pieces-liste" id="${id}-liste">${listePieces(id)}</ul></div>`;
}
function listePieces(id) {
  return (etat.pieces[id] || []).map((f, i) => `<li><span>${h(f.name)}</span> <span class="muet petit">${h(tailleCourte(f.size))}</span> <button type="button" class="btn-lien" data-retirer-piece="${id}|${i}" title="${h(t("Remove"))}">✕</button></li>`).join("");
}
function ajouterPieces(id, fichiers) {
  const l = etat.pieces[id] = etat.pieces[id] || [];
  for (const f of fichiers) {
    if (f.size > TAILLE_PIECE_MAX) { toast(t("{0} is too large (50 MB at most).", [f.name]), "erreur"); continue; }
    if (!l.some((x) => x.name === f.name && x.size === f.size)) l.push(f);
  }
  const ul = $("#" + id + "-liste"); if (ul) ul.innerHTML = listePieces(id);
}
// cible: "demande" (REQUEST.md) or a queue file; projet: the project's name
async function envoyerPieces(id, cible, projet) {
  const fichiers = etat.pieces[id] || [];
  let ok = 0;
  for (const f of fichiers) {
    try {
      const r = await fetch("/api/pieces?p=" + encodeURIComponent(projet) + "&cible=" + encodeURIComponent(cible), {
        method: "POST", headers: { "X-Athanor": CSRF, "X-Nom-Fichier": encodeURIComponent(f.name) }, body: f });
      const d = await r.json().catch(() => ({}));
      if (!r.ok) throw new Error(d.erreur_key ? t(d.erreur_key, d.args || []) : t("Error {0}", [r.status]));
      ok++;
    } catch (e) { toast(t("{0} could not be attached: {1}", [f.name, e.message]), "erreur"); }
  }
  etat.pieces[id] = [];
  const ul = $("#" + id + "-liste"); if (ul) ul.innerHTML = "";
  if (ok) toast(t("{0} file(s) attached to the request.", [ok]), "ok");
}

pages.demandes = async () => {
  const d = await api("demandes");
  const planTexte = (await api("plan").catch(() => ({ texte: "" }))).texte || "";
  const plan = d.plan || [];
  const enCours = d.demande && !/^\s*$/.test(d.demande.replace(/^#.*$/gm, "").replace(/^\(.*\)$/gm, ""));
  const titreDemande = (d.demande.match(/^#\s*(.+)$/m) || [])[1] || t("Current request");
  return `
  <div class="grille grille-2">
    <div class="colonne">
      <div class="bloc"><h2>${t("New request")}</h2>
        <p class="intro petit">${t("It is time-stamped and queued. Claude takes it when the current request is finished; you can change the order.")}</p>
        <label class="champ"><span>${t("Short title")}</span><input type="text" id="nd-titre" placeholder="${h(t("e.g. ten levels, each with its own music"))}"></label>
        <label class="champ"><span>${t("What you want")}</span><textarea id="nd-texte" rows="6" placeholder="${h(t("Describe the expected result, for whom, on which platform. Technologies are free unless you impose one."))}"></textarea></label>
        ${barreAmeliorer("nd-texte")}
        ${zonePieces("nd")}
        <button class="btn btn-primaire" id="nd-ajouter">${t("Add to the queue")}</button>
      </div>
      <div class="bloc"><h2>${t("Note along the way")}</h2>
        <p class="intro petit">${t("To correct without waiting: Claude reads it at the start of the next cycle, during the current request.")}</p>
        <textarea id="rq-texte" rows="2" placeholder="${h(t("e.g. the footstep sounds are too high-pitched, lower them."))}"></textarea>
        ${zonePieces("rq")}
        <div class="ligne mt-1"><button class="btn" id="rq-ajouter">${t("Add the note")}</button></div>
      </div>
      <div class="bloc"><details><summary>${t("REQUEST.md, the full file")}</summary>
        <textarea id="dm-texte" class="code" rows="18">${h(d.demande)}</textarea>
        <div class="ligne mt-1"><button class="btn" id="dm-enregistrer">${t("Save the file")}</button><button class="btn" data-ouvrir="demande">${t("Open in the editor")}</button></div></details></div>
      <div class="bloc"><details><summary>${t("PLAN.md, the full plan")}</summary>
        <p class="muet petit">${t("Claude's working document: milestones, files, dependencies, acceptance criteria. Edit it here when the buttons on the milestones are not enough; Claude follows it from the next cycle.")}</p>
        <textarea id="pl-texte" class="code" rows="18">${h(planTexte)}</textarea>
        <div class="ligne mt-1"><button class="btn" id="pl-enregistrer">${t("Save the plan")}</button><button class="btn" data-ouvrir="plan">${t("Open in the editor")}</button></div></details></div>
    </div>
    <div class="colonne">
      <div class="bloc"><h2>${t("Where things stand")}</h2>
      <div class="flux">
        ${[...d.faites.filter((f) => f.livree), ...d.faites.filter((f) => !f.livree)].map((f) => `<div class="carte-demande ${f.livree ? "faite" : "integree"}"><div class="num">${f.livree ? "✔" : "+"}</div><div class="corps"><div class="titre">${h(f.titre)}</div><div class="meta">${f.livree ? t("done") : t("added to the plan")}${f.date ? " · " + h(f.date) : ""}</div></div></div>`).join("")}
        ${enCours ? `<div class="carte-demande en-cours"><div class="num">▶</div><div class="corps"><div class="titre">${h(titreDemande)}</div><div class="meta">${etat.vue && etat.vue.actif ? t("in progress") : t("current request, pilot stopped")}</div>
          ${plan.length ? `<ul class="plan jalons-mini">${listePlan(plan, true)}</ul>` : ""}
          </div></div>` : `<div class="vide">${t("No current request. Write the first one on the left: it becomes the project's request.")}</div>`}
        ${d.attente.map((a, i) => `<div class="carte-demande" data-fichier="${h(a.fichier)}"><div class="num">${i + 1}</div><div class="corps"><div class="titre">${h(a.apercu || a.nom)}</div><div class="meta">${t("queued")} · ${h(a.nom)}</div></div>
          <div class="tools"><button class="btn btn-petit" data-monter title="${h(t("Move up"))}" ${i === 0 ? "disabled" : ""}>↑</button><button class="btn btn-petit" data-descendre title="${h(t("Move down"))}" ${i === d.attente.length - 1 ? "disabled" : ""}>↓</button><button class="btn btn-petit btn-danger" data-retirer title="${h(t("Remove"))}">✕</button></div></div>`).join("")}
      </div></div>
    </div>
  </div>`;
};

async function reordonner(fichier, sens) {
  const d = await api("demandes");
  const ordre = d.attente.map((a) => a.fichier);
  const i = ordre.indexOf(fichier), j = i + sens;
  if (i < 0 || j < 0 || j >= ordre.length) return;
  [ordre[i], ordre[j]] = [ordre[j], ordre[i]];
  await action(api("demandes/ordre", "POST", { ordre }));
  rendrePage();
}

// --- log
pages.journal = async () => {
  const f = etat.filtres;
  const casee = (k, lib) => `<label><input type="checkbox" data-filtre="${k}" ${f[k] ? "checked" : ""}> ${lib}</label>`;
  return `<div class="journal-tools">
    ${casee("pilote", t("Pilot"))} ${casee("texte", t("Claude speaks"))} ${casee("outil", t("Tools"))} ${casee("resultat", t("Results"))} ${casee("sous_agent", t("Sub-agents"))}
    <span class="espace"></span>
    <input type="text" id="jr-recherche" placeholder="${h(t("filter…"))}" value="${h(etat.recherche)}">
    <label><input type="checkbox" data-filtre="brut" ${f.brut ? "checked" : ""}> ${t("Raw (JSON)")}</label>
    <span class="muet petit" id="jr-fichier"></span>
  </div>
  <div class="journal" id="journal"></div>`;
};

function rendreJournal() {
  const el = $("#journal"); if (!el) return;
  const f = etat.filtres, q = etat.recherche.toLowerCase();
  const items = etat.evenements.filter((e) => {
    if (q && !(e.titre + " " + e.detail).toLowerCase().includes(q)) return false;
    if (f.brut) return true;
    const ok = { pilote: f.pilote, texte: f.texte, outil: f.outil, resultat: f.resultat, erreur: true, sous_agent: f.sous_agent, fin: true, quota: true, system: true }[e.genre];
    return ok && (!e.sous_agent || f.sous_agent);
  });
  const bas = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
  el.innerHTML = items.slice(-1500).map((e) => f.brut
    ? `<div class="ev g-${h(e.genre)}"><span class="h">${h(e.heure)}</span><span class="m"></span><span class="t">${h(e.brut)}</span></div>`
    : `<div class="ev g-${h(e.genre)} ${e.sous_agent ? "sous" : ""} ${e.detail ? "cliquable" : ""}" data-n="${e.n}"><span class="h">${h(e.heure)}</span><span class="m"></span><span class="t">${e.sous_agent ? "↳ " : ""}${h(t(e.titre))}</span></div>${etat.ouverts.has(e.n) && e.detail ? `<div class="ev-detail">${h(e.detail)}</div>` : ""}`
  ).join("");
  if (bas || etat.suiviAutoScroll) el.scrollTop = el.scrollHeight;
}

async function lireJournal() {
  const r = await api("journal?apres=" + etat.dernierJournal).catch(() => null);
  if (!r) return;
  if (r.evenements.length) {
    etat.evenements.push(...r.evenements);
    etat.dernierJournal = r.evenements[r.evenements.length - 1].n;
    if (etat.evenements.length > 6000) etat.evenements.splice(0, etat.evenements.length - 5000);
    if (etat.page === "journal") rendreJournal();
  }
  const fj = $("#jr-fichier"); if (fj) fj.textContent = r.fichier || "";
}

// --- setup & health
// the checks' list alone: refreshed in place while a check or a repair runs (majSante), so the fields
// below keep what is typed in them and the output keeps its scroll
function listeSante(verifs) {
  const ico = (v) => v.statut === "ok" ? ["✔", "ok"] : v.statut === "en_cours" ? ["◌", "en_cours"] : v.statut === "echec" ? (v.facultatif ? ["!", "facultatif"] : ["✖", "echec"]) : ["○", ""];
  return verifs.map((v) => { const [i, c] = ico(v); return `
    <div class="verif v-${h(v.statut)} ${v.facultatif ? "v-facultatif" : ""} ${etat.verifChoisie === v.id ? "choisi" : ""}" data-id="${h(v.id)}" data-tache="${h(v.tache_id)}">
      <div class="ico ${c}">${i}</div>
      <div><div class="titre" data-voir="${h(v.tache_id)}" title="${h(t(v.explication))}">${h(t(v.titre))}</div><div class="resume">${h(v.en_reparation ? t("repair in progress…") : v.resume || t(v.explication))}</div></div>
      <div class="ligne">
        ${v.statut === "echec" && !v.en_reparation ? `${v.reparation_auto ? `<button class="btn btn-petit" data-reparer title="${h(t(v.reparation_info))}">${t("Repair")}</button>` : ""}<button class="btn btn-petit" data-claude>${t("Ask Claude")}</button>` : ""}
        <button class="btn btn-petit" data-verifier ${v.statut === "en_cours" ? "disabled" : ""}>${t("Check")}</button>
      </div></div>`; }).join("");
}
async function majSante() {
  const verifs = await api("sante").catch(() => null);
  const el = $(".bloc-liste");
  if (verifs && el && etat.page === "sante") { const html = listeSante(verifs); if (html !== etat.santeListe) { etat.santeListe = html; el.innerHTML = html; } }
  await rendreSortie();
}

pages.sante = async () => {
  const verifs = await api("sante");
  etat.santeListe = listeSante(verifs); etat.sortieSig = "";
  const v = etat.vue || {}, modes = modesSysteme(v);
  const actuel = modes.find((m) => m.actuel) || modes[0];
  const choisi = modes.find((m) => m.id === etat.ongletSante) || actuel;
  const tete = `<div class="onglets">${modes.map((m) => `<button class="${m === choisi ? "actif" : ""}" data-onglet-sante="${m.id}">${t(m.nom)}${m.actuel ? ` <span class="badge">${t("current")}</span>` : ""}</button>`).join("")}</div>`;
  const installer = v.windows ? `<button class="btn" data-installer-mode="${choisi.id}">${t("Install everything")}</button>` : `<button class="btn" data-installer>${t("Install everything")}</button>`;
  if (!choisi.actuel) return tete + `<div class="bloc"><h2>${t(choisi.nom)}</h2><p class="intro">${t(choisi.texte)}</p>
    <div class="ligne"><button class="btn btn-primaire" data-mode-systeme="${choisi.id}">${t("Use this mode")}</button>${installer}</div>
    <p class="muet petit mt-2">${t("“Install everything” also switches to this mode. The checks below the tabs are those of the mode in use ({0}).", [t(actuel.nom)])}</p></div>`;
  return tete + `<p class="intro">${t(choisi.texte)}</p><div class="ligne">
    <button class="btn btn-primaire" id="st-tout">${t("Check everything")}</button>
    ${installer}
    ${v.windows && v.natif ? "" : `<button class="btn" id="st-barriere" title="${h(t("A short Claude session tries to leave the project, read your files, launch cmd.exe: everything must be blocked."))}">${t("Test the sandbox (2 min)")}</button>`}
    <button class="btn" id="st-recompiler" title="${h(t("After a repair of its Go code (app/): shows the change, checks it, rebuilds, runs the autotest, and only then replaces the executable, keeping the previous one."))}">${t("Rebuild Athanor")}</button>
  </div>
  <div class="bloc bloc-liste">${listeSante(verifs)}</div>
  <div class="bloc"><h2>${t("Ask Claude")}</h2>
    <p class="intro petit">${t("A Claude session in the tool folder, outside the sandbox and without sudo: it installs, fixes, then summarizes. Whatever needs sudo or Windows is written to logs\\TODO_FOR_YOU.md.")}</p>
    <div class="ligne"><input type="text" id="st-libre" class="croit" placeholder="${h(t("e.g. install Godot 4 · why does the pilot stop?"))}"><button class="btn" id="st-libre-btn">${t("Send")}</button></div>
    <div class="ligne mt-1"><input type="text" id="st-complement" class="croit" placeholder="${h(t("Detail attached to repairs (optional): what you already tried, since when…"))}"></div>
  </div>
  <div id="st-sortie"></div>`;
};

async function rendreSortie() {
  const el = $("#st-sortie"); if (!el || !etat.tacheChoisie) return;
  const choisie = etat.tacheChoisie;
  const r = await api("tache/" + choisie).catch(() => null); if (!r || choisie !== etat.tacheChoisie) return;
  const tk = r.tache;
  const st = { en_cours: [t("running"), "quota"], ok: [t("finished"), "ok"], echec: [t("failed, code {0}", [tk.code]), "echec"], jamais: ["", ""] }[tk.statut] || ["", ""];
  const duree = $("#st-duree", el);
  // the output is rebuilt only when it changed, and never under a selection (copying a line): a long answer
  // rebuilt every 1.5 s froze the page; it follows the end only when it was already at the end
  const sig = choisie + "|" + tk.statut + "|" + r.evenements.length + "|" + ((r.evenements.at(-1) || {}).titre || "");
  if (sig === etat.sortieSig && duree) { duree.textContent = `${st[0]} · ${tk.duree_s} s`; return; }
  const ancienne = $(".sortie", el);
  if (ancienne && String(window.getSelection() || "") && ancienne.contains(window.getSelection().anchorNode)) return;
  const enBas = !ancienne || ancienne.scrollHeight - ancienne.scrollTop - ancienne.clientHeight < 40, haut = ancienne ? ancienne.scrollTop : 0;
  etat.sortieSig = sig;
  el.innerHTML = `<div class="bloc"><div class="ligne"><h2 class="m0">${h(t(tk.nom))}</h2><span class="${st[1]}" id="st-duree">${st[0]} · ${tk.duree_s} s</span><span class="espace"></span><button class="btn btn-petit" id="st-copier">${t("Copy output")}</button></div>
    <div class="sortie">${r.evenements.map((e) => `<div class="ev g-${h(e.genre)} ${e.sous_agent ? "sous" : ""}"><span class="h"></span><span class="m"></span><span class="t">${h(t(e.titre))}</span></div>`).join("") || '<div class="ev"><span class="h"></span><span class="m"></span><span class="t muet">…</span></div>'}</div></div>`;
  const s = $(".sortie", el); s.scrollTop = enBas ? s.scrollHeight : haut;
  $("#st-copier").onclick = () => navigator.clipboard.writeText(tk.sortie.join("\n")).then(() => toast(t("Copied."), "ok"));
}

// --- settings (.env)
// --- settings: one page, two tabs. "Claude": the pilot's settings (the kit's .env), grouped, with plain
// names (the variable stays shown small, as in the documentation); "Options": the application's.
// The labels of the settings Athanor knows: [group, name]. Unknown ones go to "Advanced".
const REGLAGES = {
  CLAUDE_CODE_OAUTH_TOKEN: ["connexion", "Connection token"],
  MODEL: ["modeles", "Model of the lead"], FALLBACK_MODEL: ["modeles", "Fallback model"],
  FRAMING_MODEL: ["modeles", "Model of the framing"], WORKER_MODEL: ["modeles", "Model of the parallel workers"],
  SUBAGENT_MODEL: ["modeles", "Model of the sub-agents"], CHEAP_MODEL: ["modeles", "Cheap model"], EFFORT: ["modeles", "Thinking effort"],
  CLOSE_AFTER_MILESTONE: ["marche", "One milestone per cycle"], RELAY_CONTEXT_K: ["marche", "Fresh session beyond"],
  MAX_HOURS: ["marche", "Safety stop after"], CYCLE_MAX_MIN: ["marche", "Longest cycle"], IDLE_MAX_MIN: ["marche", "Silent cycle stopped after"],
  FRAMING_MAX_MIN: ["marche", "Longest framing"], RTK: ["marche", "Compressed command outputs"],
  KEEP_AWAKE: ["marche", "Keep the PC awake"], PILOT_ENGINE: ["avance", "Engine of the pilot"],
  LANGUAGE: ["avance", "Language of Claude's documents"], TZ: ["avance", "Time zone of the logs"],
  QUOTA_WAIT_MIN: ["avance", "Wait when the quota's end is unknown"], ERROR_WAIT_MIN: ["avance", "Wait after an error"],
};
const GROUPES_REGLAGES = [["connexion", "Connection"], ["modeles", "Models"], ["marche", "How the pilot works"]];
const EFFORTS = ["", "low", "medium", "high", "xhigh", "max"];

// modeles: the proposals, from the server (the aliases, then the models this PC has used: a new model
// appears by itself once a cycle has run on it); any other name can be typed in the field
function champReglage(l, modeles) {
  const id = "env-" + l.cle;
  const choix = (valeurs, nom) => `<input type="text" id="${id}" data-cle="${h(l.cle)}" value="${h(l.valeur)}"><div class="choix">${valeurs.map((m) => `<button type="button" data-pour="${id}" data-val="${h(m)}" class="${l.valeur === m ? "actif" : ""}">${h(nom(m))}</button>`).join("")}</div>`;
  const nomModele = (m) => m === "auto" ? t("chosen by the lead") : m || t("account default");
  if (["MODEL", "FALLBACK_MODEL", "FRAMING_MODEL", "WORKER_MODEL", "CHEAP_MODEL"].includes(l.cle)) return choix(["", ...modeles], nomModele);
  if (l.cle === "SUBAGENT_MODEL") return choix(["", ...modeles, "auto"], nomModele);
  if (l.cle === "EFFORT") return choix(EFFORTS, (m) => m || t("normal"));
  if (l.cle === "PILOT_ENGINE") return choix(["bash", "go"], (m) => t({ bash: "bash (default)", go: "Go (serial mode)" }[m]));
  if (l.cle === "KEEP_AWAKE") return choix(["1", "screen", "0"], (m) => t({ "1": "PC awake", screen: "PC and screen awake", "0": "no" }[m]));
  if (["RTK", "UNREAL_MCP", "CLOSE_AFTER_MILESTONE"].includes(l.cle) && (l.valeur === "0" || l.valeur === "1")) return `<label class="interrupteur m0"><input type="checkbox" id="${id}" data-cle="${h(l.cle)}" ${l.valeur === "1" ? "checked" : ""}> <span>${l.valeur === "1" ? t("enabled") : t("disabled")}</span></label>`;
  return `<input type="${l.secret && !etat.envSecrets ? "password" : "text"}" id="${id}" data-cle="${h(l.cle)}" value="${h(l.valeur)}" ${l.secret ? 'autocomplete="off"' : ""}>`;
}

pages.reglages = async () => {
  const onglet = etat.ongletReglages === "options" ? "options" : "claude";
  const tete = `<div class="onglets"><button class="${onglet === "claude" ? "actif" : ""}" data-onglet-reglages="claude">${t("Claude")}</button><button class="${onglet === "options" ? "actif" : ""}" data-onglet-reglages="options">${t("Options")}</button></div>`;
  if (onglet === "options") return tete + await rendreOptions();
  const [lignes, mod] = await Promise.all([api("env" + (etat.envSecrets ? "?secrets=1" : "")), api("modeles").catch(() => ({ modeles: [] }))]);
  const modeles = mod.modeles || [];
  const ligne = (l) => {
    const nom = (REGLAGES[l.cle] || [])[1];
    return `<div class="reglage"><div>${nom ? `<div class="nom-reglage">${h(t(nom))} <code class="cle-env">${h(l.cle)}</code></div>` : `<div class="cle">${h(l.cle)}</div>`}<div class="aide">${h(t(l.aide))}</div></div><div>${champReglage(l, modeles)}</div></div>`;
  };
  const groupe = (g) => lignes.filter((l) => (REGLAGES[l.cle] || ["avance"])[0] === g);
  const actif = etat.vue && etat.vue.actif;
  const avance = groupe("avance");
  return `${tete}
  <div class="ligne reglages-tete">
    <p class="muet m0 croit">${t("The pilot reads these settings when it starts. The token gives access to your subscription: never share it.")}</p>
    <label class="interrupteur m0"><input type="checkbox" id="rg-secrets" ${etat.envSecrets ? "checked" : ""}> <span>${t("Show secrets")}</span></label>
    <button class="btn btn-primaire" id="rg-enregistrer">${t("Save")}</button>
    ${actif ? `<button class="btn" id="rg-redemarrer" title="${h(t("Saves, lets the current milestone finish, then restarts the pilot with the new settings."))}">${t("Save and restart after cycle")}</button>` : ""}
  </div>
  ${GROUPES_REGLAGES.map(([g, titre]) => { const l = groupe(g); return l.length ? `<div class="bloc"><h2>${t(titre)}</h2>${l.map(ligne).join("")}</div>` : ""; }).join("")}
  ${avance.length ? `<div class="bloc"><details><summary>${t("Advanced")} <span class="muet petit">${t("{0} setting(s)", [avance.length])}</span></summary>${avance.map(ligne).join("")}</details></div>` : ""}`;
};

function lireEnvFormulaire() {
  const v = {};
  document.querySelectorAll("[data-cle]").forEach((i) => { v[i.dataset.cle] = i.type === "checkbox" ? (i.checked ? "1" : "0") : i.value; });
  return v;
}

// --- options (saved as soon as a switch or a field changes): the "Options" tab of the settings
async function rendreOptions() {
  const o = await api("options");
  const b = o.bureau || {};
  return `<div class="grille grille-2">
    <div class="colonne">
      <div class="bloc"><h2>${t("Appearance")}</h2>
        <label class="champ"><span>${t("Language")}</span></label>${rendreLangues()}
        <p class="muet petit mt-1">${t("Also sets the language Claude uses for its plan, log and documents in new projects.")}</p>
        <label class="champ mt-3"><span>${t("Palette and accent")}</span></label>${rendreApparence()}
      </div>
      <div class="bloc"><h2>${t("Startup")}</h2>
        <label class="interrupteur"><input type="checkbox" id="op-lancer" data-option ${o.lancer_pilote_au_demarrage ? "checked" : ""}><span>${t("Restart the pilots that were running, when the app opens")}</span></label>
        <label class="interrupteur"><input type="checkbox" id="op-windows" data-option ${o.demarrage_windows ? "checked" : ""}><span>${t("Open Athanor when the computer starts (Windows, or your Linux desktop session)")}</span></label>
        <p class="muet petit">${t("Pilots run in WSL in the background, without a window. Remember to disable the PC's sleep mode.")}</p>
        ${o.windows ? `<label class="interrupteur"><input type="checkbox" id="op-barre" data-option ${o.barre_windows ? "checked" : ""}><span>${t("Windows' own title bar instead of Athanor's (when Athanor opens again)")}</span></label>` : ""}
      </div>
      <div class="bloc"><h2>${t("Lock")}</h2>
        <p class="intro petit">${t("With a password, the padlock locks the app: Claude keeps working, but nothing can be seen, changed or read (keys included) without the password. The app starts locked.")}</p>
        ${o.mot_de_passe ? `<label class="champ"><span>${t("Current password")}</span><input type="password" id="mdp-ancien" autocomplete="current-password"></label>` : ""}
        <label class="champ"><span>${o.mot_de_passe ? t("New password (empty to remove it)") : t("Password")}</span><input type="password" id="mdp-nouveau" autocomplete="new-password"></label>
        <button class="btn" id="mdp-enregistrer">${o.mot_de_passe ? t("Change") : t("Enable the lock")}</button>
      </div>
    </div>
    <div class="colonne">
      <div class="bloc"><h2>${t("Projects")}</h2>
        ${(o.projets || []).length ? `<table class="tab"><tbody>${o.projets.map((p) => `<tr><td><b>${h(p.nom)}</b>${p.archive ? ` <span class="badge">${t("archived")}</span>` : ""}</td><td class="muet">${h(p.dossier)}</td><td class="droite"><button class="btn btn-petit" data-gerer="${h(p.nom)}" data-archive="${p.archive ? 1 : ""}">${t("Manage")}</button></td></tr>`).join("")}</tbody></table>` : `<div class="vide">${t("No project.")}</div>`}
        <p class="muet petit mt-2">${t("Rename a project, move its folder, archive or delete it: “Manage”.")}</p>
      </div>
      <div class="bloc"${b.message === "Virtual desktop: Windows only." ? " hidden" : ""}><h2>${t("“Claude” desktop")}</h2>
        <label class="interrupteur"><input type="checkbox" id="op-bureau" data-option ${o.bureau_virtuel ? "checked" : ""}><span>${t("Open the programs Claude starts (Unreal, Blender, a game under test) on a desktop of their own")}</span></label>
        <p class="petit ${b.disponible ? "ok" : "attention"}">${h(t(b.message || ""))}</p>
        ${b.ecran ? `<p class="muet petit">${t("The Linux windows Claude opens (a game under test, Godot, a browser) go to a private screen: never on yours, never taking the focus.")}</p>
        <div class="ligne"><button class="btn btn-petit" data-ecran>${t("View Claude's screen")}</button></div>`
        : b.isole ? `<p class="muet petit">${t("They never show on your screen and never take the focus: a full-screen game is not interrupted. On Claude's desktop, the bar at the top (or Ctrl + Alt + B) brings you back.")}</p>
        ${!o.bureau_virtuel && b.restantes ? `<p class="petit attention">${t("{0} window(s) still open on Claude's desktop: close them there, they will then open on your screen.", [b.restantes])}</p>` : ""}
        <div class="ligne"><button class="btn btn-petit" data-bureau="voir" ${b.disponible ? "" : "disabled"}>${t("View Claude's desktop")}</button><button class="btn btn-petit" data-ecran title="${h(t("The Linux windows Claude opens (a game under test, Godot, a browser) go to a private screen: never on yours, never taking the focus."))}">${t("Claude's Linux screen")}</button></div>`
        : `<div class="ligne"><button class="btn btn-petit" data-bureau="voir" ${b.disponible ? "" : "disabled"}>${t("View Claude's desktop")}</button><button class="btn btn-petit" data-bureau="revenir" ${b.disponible ? "" : "disabled"}>${t("Come back")}</button><button class="btn btn-petit" data-bureau="ramener" ${b.disponible ? "" : "disabled"}>${t("Bring the windows back here")}</button><span class="muet petit">${t("{0} window(s) moved", [b.deplacees || 0])}</span></div>
        ${(b.vues || []).length ? `<ul class="muet petit mt-1">${b.vues.slice(0, 10).map((f) => `<li>${h(f)}</li>`).join("")}</ul>` : ""}`}
        <label class="champ mt-2"${b.isole || b.ecran ? " hidden" : ""}><span>${t("Recognized: programs inside the projects and in tools\\, “Athanor” windows, and these executables (one per line)")}</span><textarea id="op-programmes" data-option class="code" rows="5">${h(o.programmes_supplementaires)}</textarea></label>
        <p class="muet petit"${b.isole || b.ecran ? " hidden" : ""}>${t("Windows shortcut to switch desktops: Ctrl + Win + ← / →.")}</p>
      </div>
      <div class="bloc"${o.windows ? "" : " hidden"}><h2>${t("Execution mode")}</h2>
        <label class="radio"><input type="radio" name="op-mode" id="op-natif" data-option ${o.mode_natif ? "checked" : ""}> <b>${t("Native Windows")}</b> <span class="muet">${t("(recommended to begin: nothing else to install)")}</span></label>
        <p class="petit attention">${t("No sandbox in this mode: Claude can read and change any file of your PC and run any program, without asking. The file guard only covers its file tools. Use it for projects you trust.")}</p>
        <label class="radio"><input type="radio" name="op-mode" id="op-wsl" data-option ${o.mode_natif ? "" : "checked"}> <b>WSL</b> <span class="muet">${t("(Claude works in a sandbox: it only writes in the project; installs Linux once)")}</span></label>
        <p class="petit ${o.git_bash ? "ok" : "attention"}">${o.git_bash ? h(t("Git Bash: {0}", [o.git_bash])) : t("Git for Windows is missing: Setup & health → Install everything.")}</p>
        <p class="muet petit">${t("Applies to the pilots started afterwards. When you close Athanor while native pilots run, it asks whether they go on in the background.")}</p>
      </div>
      <div class="bloc"><h2>${t("Tool folder")}</h2>
        <input type="text" id="op-kit" data-option value="${h(o.kit)}"><div class="muet petit mt-1">${o.kit_valide ? "✔ " + t("tool recognized (system\\pilot.sh present)") : "✖ " + t("this folder is not the tool")}</div>
        <div class="ligne mt-1"><button class="btn btn-petit" data-ouvrir="kit">${t("Open the folder")}</button><button class="btn btn-petit" data-ouvrir="logs">${t("Logs folder")}</button><span class="espace"></span><button class="btn btn-petit" id="op-guide">${t("Show the first-launch guide again")}</button></div>
      </div>
    </div>
  </div>`;
};
// the native Windows switch is sent only where it is shown (Windows)
function o_windows() { const b = $("#op-natif"); return !!(b && !b.closest(".bloc").hidden); }
async function enregistrerOptions() {
  const r = await action(api("options", "POST", { kit: $("#op-kit").value, lancer_pilote_au_demarrage: $("#op-lancer").checked, bureau_virtuel: $("#op-bureau").checked, programmes_supplementaires: $("#op-programmes").value, demarrage_windows: $("#op-windows").checked, mode_natif: $("#op-natif") && o_windows() ? $("#op-natif").checked : undefined, barre_windows: $("#op-barre") ? $("#op-barre").checked : undefined }));
  if (r && document.activeElement && document.activeElement.matches("input[type=checkbox]")) rendrePage();
}

// ----------------------------------------------------------------------------- rendering and events
let numeroRendu = 0;
async function rendrePage() {
  // renders overlap (a live refresh of one page, a click to another): only the last one asked is shown
  const n = ++numeroRendu;
  const p = pages[etat.page] || pages.suivi;
  let html;
  try { html = await p(); } catch (e) { if (e.message === t("Locked")) return; html = `<div class="vide">${h(e.message)}</div>`; }
  if (n !== numeroRendu) return;
  $("#page").innerHTML = html;
  if (etat.page === "journal") rendreJournal();
  if (etat.page === "sante") rendreSortie();
  if (etat.page === "nouveau") rendreExplorateur(etat.explorateur);
}

async function changerLangue(code) {
  etat.langue = code; appliquerLangue(code);
  await action(api("preferences", "POST", { langue: code }));
  traduireNav(); if (etat.vue) rendreBandeau(etat.vue); if (etat.assistant < 0) rendrePage();
}
async function changerApparence(theme, accent) {
  etat.theme = theme || ""; etat.accent = accent || ""; appliquerTheme(etat.theme, etat.accent);
  await action(api("preferences", "POST", { theme: etat.theme, accent: etat.accent }));
  if (etat.assistant < 0) rendrePage();
}

// Milestones of the plan: a click on the line opens or closes its details; the buttons inside edit the plan.
$("#page").addEventListener("click", async (ev) => {
  if (ev.target.closest("[data-stats-tous]")) { etat.ouverts.has("stats:tous") ? etat.ouverts.delete("stats:tous") : etat.ouverts.add("stats:tous"); rendrePage(); return; }
  if (ev.target.closest("[data-plan-archives]")) { etat.ouverts.has("plan:archives") ? etat.ouverts.delete("plan:archives") : etat.ouverts.add("plan:archives"); rendrePage(); return; }
  const li = ev.target.closest("li[data-jalon-cle]"), bJ = ev.target.closest("[data-jalon-action]");
  if (bJ) {
    const id = bJ.dataset.jalon, act = bJ.dataset.jalonAction;
    if (act === "retirer" && !(await confirmer(t("Remove milestone {0} from the plan? Its work, if any, stays in the project; the other milestones no longer depend on it.", [id]), { ok: t("Remove"), danger: true }))) return;
    const r = await action(api("plan/jalon", "POST", { id, action: act }));
    if (r) { await rafraichir(); rendrePage(); }
    return;
  }
  if (li && !ev.target.closest("a, button, code")) { const c = li.dataset.jalonCle; etat.ouverts.has(c) ? etat.ouverts.delete(c) : etat.ouverts.add(c); rendrePage(); return; }
  // Team: a sub-agent or a session report opens or closes (text inside stays selectable)
  const ag = ev.target.closest(".appel-ligne, .rapport-tete");
  if (ag && !String(window.getSelection() || "")) { const c = ag.closest("[data-agent-cle]").dataset.agentCle; etat.ouverts.has(c) ? etat.ouverts.delete(c) : etat.ouverts.add(c); rendrePage(); }
});

$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("button, .titre[data-voir], .ev.cliquable, .explorateur div[data-chemin]"); if (!b) return;
  const page = etat.page;
  if (b.dataset.langue) return changerLangue(b.dataset.langue);
  if (b.dataset.theme) return changerApparence(b.dataset.theme, etat.accent);
  if (b.dataset.accent) return changerApparence(etat.theme, b.dataset.accent);
  if (b.dataset.ouvrir) return action(api("ouvrir", "POST", { quoi: b.dataset.ouvrir }));
  if (b.dataset.bureau) return action(api("bureau/" + b.dataset.bureau, "POST")).then(rendrePage);
  if (b.hasAttribute("data-ecran")) return ouvrirEcran();
  if (b.dataset.act === "demarrer") return demarrerPilote(b);
  if (b.dataset.act) { b.disabled = true; await action(api("pilote/" + b.dataset.act, "POST")); return setTimeout(rafraichir, 800); }
  if (b.dataset.installerMode !== undefined) { const r = await action(api("sante/installer", "POST", { mode: b.dataset.installerMode })); if (r) { await rafraichir(); rendrePage(); } return; }
  if (b.hasAttribute("data-installer")) return action(api("sante/installer", "POST"));
  if (b.dataset.modeSysteme) { const r = await action(api("mode-systeme", "POST", { natif: b.dataset.modeSysteme === "natif" })); if (r) { await rafraichir(); rendrePage(); } return; }
  if (b.dataset.gestion) return gererProjet(b.dataset.gestion, b.dataset.nom || etat.projet, b);
  if (b.dataset.gh) return actionGitHub(b.dataset.gh, b);
  if (b.dataset.ag) return actionAgent(b.dataset.ag, b);
  if (b.dataset.pv) return repondreTache(b);
  if (b.dataset.choisirPieces) return $("#" + b.dataset.choisirPieces + "-fichiers").click();
  if (b.dataset.retirerPiece) { const [id, i] = b.dataset.retirerPiece.split("|"); (etat.pieces[id] || []).splice(Number(i), 1); $("#" + id + "-liste").innerHTML = listePieces(id); return; }
  if (b.dataset.max && page === "projet") return enregistrerMode($("#md-parallele").checked, Number(b.dataset.max));
  if (page === "nouveau") {
    if (b.dataset.chemin) return rendreExplorateur(b.dataset.chemin);
    if (b.id === "np-utiliser") { $("#np-dossier").value = etat.explorateur; return; }
    if (b.id === "np-creer") {
      if (b.disabled) return;
      const gh = lireChoixGitHub("np", $("#np-nom").value); if (gh === false) return;
      b.disabled = true; // one project per click, even on a double-click
      const r = await action(api("projets", "POST", { nom: $("#np-nom").value, dossier: $("#np-dossier").value, demande: $("#np-demande").value }));
      b.disabled = false;
      if (r) {
        etat.projet = r.nom; etat.evenements = []; etat.dernierJournal = 0;
        if ((etat.pieces.np || []).length) await envoyerPieces("np", "demande", r.nom);
        if (gh) await action(api("github", "POST", gh)); // the sign-in window opens; the link shows under Manage
        await rafraichir(); location.hash = "#suivi";
      }
      return;
    }
  }
  if (page === "demandes") {
    const carte = b.closest("[data-fichier]");
    // one request / note per click, even on a double-click
    if (b.id === "nd-ajouter") {
      if (b.disabled) return; b.disabled = true;
      const r = await action(api("demandes", "POST", { titre: $("#nd-titre").value, texte: $("#nd-texte").value }));
      if (r && (etat.pieces.nd || []).length) await envoyerPieces("nd", r.ou === "attente" && r.fichier ? r.fichier : "demande", etat.projet);
      b.disabled = false; if (r) rendrePage(); return;
    }
    if (b.id === "rq-ajouter") {
      if (b.disabled) return; b.disabled = true;
      const texte = $("#rq-texte").value, avecPieces = (etat.pieces.rq || []).length > 0;
      const r = texte.trim() || !avecPieces ? await action(api("remarque", "POST", { texte })) : true;
      if (r && avecPieces) await envoyerPieces("rq", "demande", etat.projet); // added to the current request: Claude takes them at the next cycle
      b.disabled = false; if (r) rendrePage(); return;
    }
    if (b.id === "dm-enregistrer") return action(api("demande", "PUT", { texte: $("#dm-texte").value }));
    if (b.id === "pl-enregistrer") { const r = await action(api("plan", "PUT", { texte: $("#pl-texte").value })); if (r) { await rafraichir(); } return; }
    if (carte && b.hasAttribute("data-monter")) return reordonner(carte.dataset.fichier, -1);
    if (carte && b.hasAttribute("data-descendre")) return reordonner(carte.dataset.fichier, 1);
    if (carte && b.hasAttribute("data-retirer")) { if (await confirmer(t("Remove this request from the queue?"), { ok: t("Remove"), danger: true })) { await action(api("demandes/" + encodeURIComponent(carte.dataset.fichier), "DELETE")); rendrePage(); } return; }
  }
  if (page === "journal" && b.classList.contains("ev")) { const n = Number(b.dataset.n); etat.ouverts.has(n) ? etat.ouverts.delete(n) : etat.ouverts.add(n); etat.suiviAutoScroll = false; return rendreJournal(); }
  if (page === "sante") {
    if (b.dataset.ongletSante) { etat.ongletSante = b.dataset.ongletSante; return rendrePage(); }
    const ligne = b.closest(".verif"); const id = ligne && ligne.dataset.id;
    if (b.id === "st-tout") { await action(api("sante/verifier", "POST", { id: "tout" })); return rendrePage(); }
    if (b.id === "st-installer") return action(api("sante/installer", "POST"));
    if (b.id === "st-recompiler") { const r = await action(api("sante/recompiler", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-barriere") { const r = await action(api("sante/barriere", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-libre-btn") { const r = await action(api("sante/libre", "POST", { texte: $("#st-libre").value })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; rendrePage(); } return; }
    if (b.dataset.voir !== undefined) { etat.tacheChoisie = b.dataset.voir; etat.verifChoisie = id; return rendrePage(); }
    if (b.hasAttribute("data-verifier")) { const r = await action(api("sante/verifier", "POST", { id })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
    if (b.hasAttribute("data-reparer")) { const r = await action(api("sante/reparer", "POST", { id })); if (r && r.tache) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
    if (b.hasAttribute("data-claude")) { const r = await action(api("sante/claude", "POST", { id, complement: $("#st-complement").value })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
  }
  if (page === "chat") {
    if (b.id === "chat-envoyer") return envoyerChat();
    if (b.id === "chat-stop") { await action(api("chat/arreter", "POST")); return majChat(true); }
    if (b.id === "chat-nouveau") { if (await confirmer(t("Start a new conversation? Claude forgets this one."), { ok: t("New conversation") })) { await action(api("chat/effacer", "POST")); majChat(true); } return; }
    if (b.dataset.chatModele !== undefined) { chat.modele = b.dataset.chatModele; try { localStorage.setItem("chatModele", chat.modele); } catch (e) { /* ignore */ } b.parentElement.querySelectorAll("button").forEach((x) => x.classList.toggle("actif", x === b)); return; }
    if (b.dataset.chatNote !== undefined) { b.disabled = true; await action(api("chat/note", "POST", { index: Number(b.dataset.chatNote) })); return majChat(true); }
  }
  if (page === "reglages") {
    if (b.dataset.ongletReglages) { etat.ongletReglages = b.dataset.ongletReglages; return rendrePage(); }
    if (b.dataset.pour) { const i = $("#" + b.dataset.pour); i.value = b.dataset.val; b.parentElement.querySelectorAll("button").forEach((x) => x.classList.toggle("actif", x === b)); return; }
    if (b.id === "rg-enregistrer") return action(api("env", "POST", lireEnvFormulaire()));
    if (b.id === "rg-redemarrer") { const r = await action(api("env", "POST", lireEnvFormulaire())); if (r) await action(api("pilote/redemarrer-apres-cycle", "POST")); return; }
  }
  if (page === "reglages") {
    if (b.dataset.gerer) { if (b.dataset.archive) { location.hash = "#archives"; return; } etat.projet = b.dataset.gerer; etat.evenements = []; etat.dernierJournal = 0; await rafraichir(); location.hash = "#projet"; return; }
    if (b.dataset.retirerProjet) { if (await confirmer(t("Remove “{0}” from the list? Its folder stays intact.", [b.dataset.retirerProjet]), { ok: t("Remove from list"), danger: true })) { await action(api("projets/" + encodeURIComponent(b.dataset.retirerProjet), "DELETE")); if (etat.projet === b.dataset.retirerProjet) etat.projet = ""; await rafraichir(); rendrePage(); } return; }
    if (b.id === "op-guide") { await action(api("preferences", "POST", { bienvenue_vue: false })); etat.assistant = 0; return rendreAssistant(); }
    if (b.id === "mdp-enregistrer") { const r = await action(api("mot-de-passe", "POST", { ancien: ($("#mdp-ancien") || {}).value || "", nouveau: $("#mdp-nouveau").value })); if (r) { toast(r.defini ? t("Lock enabled: the padlock is at the bottom left.") : t("Lock removed."), "ok"); rendrePage(); rafraichir(); } return; }
  }
});

$("#page").addEventListener("keydown", (ev) => {
  if (ev.target.id === "chat-texte" && ev.key === "Enter" && !ev.shiftKey && !ev.isComposing) { ev.preventDefault(); envoyerChat(); }
});

$("#page").addEventListener("change", (ev) => {
  const i = ev.target;
  if (i.id === "chat-question") chat.questionSeule = i.checked;
  if (i.dataset.quotaSeuil || i.dataset.quotaAction) {
    const cle = i.dataset.quotaSeuil || i.dataset.quotaAction, bloc = i.closest(".quota-arret");
    const seuil = Number((bloc.querySelector("[data-quota-seuil]") || {}).value || 0);
    const choix = bloc.querySelector("[data-quota-action]");
    const quoi = choix ? choix.value : ((etat.vue && etat.vue.quota_arret || {})[cle] || {}).action || "apres";
    action_quota(cle, seuil, quoi);
  }
  if (i.dataset.filtre) { etat.filtres[i.dataset.filtre] = i.checked; rendreJournal(); }
  if (i.id === "rg-secrets") { etat.envSecrets = i.checked; rendrePage(); }
  if (i.type === "checkbox" && i.dataset.cle) i.nextElementSibling.textContent = i.checked ? t("enabled") : t("disabled");
  if (etat.page === "reglages" && i.hasAttribute("data-option")) enregistrerOptions();
  if (i.hasAttribute("data-gh-coauteur")) action(api("github", "POST", Object.assign({ action: "identite" }, lireAuteur()))).then((r) => { if (!r) i.checked = !i.checked; });
  if (i.id === "gh-auteur-email") { const a = $("#gh-auteur-autre"); a.hidden = i.value !== ""; if (!a.hidden) a.focus(); }
  if (i.hasAttribute("data-gh-auto")) action(api("github", "POST", { action: "auto", auto: i.checked })).then((r) => { if (!r) i.checked = !i.checked; });
  if (i.id === "md-parallele") { const actif = $("#page .choix button.actif"); enregistrerMode(i.checked, actif ? Number(actif.dataset.max) : 3); }
});
// attached files: chosen with the button or dropped on a form's zone; a file dropped elsewhere is ignored
// (otherwise the window would open it in place of the application)
window.addEventListener("dragover", (ev) => {
  if (!fichiersGlisses(ev)) return;
  const z = ev.target.closest && ev.target.closest("[data-pieces]");
  document.querySelectorAll(".pieces.survol").forEach((x) => x !== z && x.classList.remove("survol"));
  if (z) z.classList.add("survol");
  ev.dataTransfer.dropEffect = z ? "copy" : "none";
});
window.addEventListener("dragleave", (ev) => { if (!ev.relatedTarget) document.querySelectorAll(".pieces.survol").forEach((x) => x.classList.remove("survol")); });
window.addEventListener("drop", (ev) => {
  if (!fichiersGlisses(ev)) return;
  document.querySelectorAll(".pieces.survol").forEach((x) => x.classList.remove("survol"));
  const z = ev.target.closest && ev.target.closest("[data-pieces]");
  if (z && ev.dataTransfer.files.length) ajouterPieces(z.dataset.pieces, ev.dataTransfer.files);
});
$("#page").addEventListener("change", (ev) => {
  const i = ev.target;
  if (i.type === "file" && i.id.endsWith("-fichiers")) { ajouterPieces(i.id.slice(0, -"-fichiers".length), i.files); i.value = ""; }
});
// the sub-agent being written keeps its fields if the page is drawn again
// links in Claude's texts (For you page) open in the default browser
$("#page").addEventListener("click", (ev) => {
  const a = ev.target.closest("a[data-lien]"); if (!a) return;
  ev.preventDefault();
  action(api("lien", "POST", { url: a.dataset.lien }));
});
// answers being written on the For you page survive the page being drawn again
$("#page").addEventListener("input", (ev) => {
  const d = ev.target.dataset;
  if (d.pvTexte) brouillonTache(d.pvTexte).texte = ev.target.value;
  if (d.pvSecret) { const [id, nom] = d.pvSecret.split("|"); brouillonTache(id).secrets[nom] = ev.target.value; }
});
const CHAMPS_AGENT = { "ag-nom": "nom", "ag-description": "description", "ag-consignes": "consignes", "ag-modele": "modele", "ag-lecture": "lecture_seule", "ag-tous": "tous" };
for (const genre of ["input", "change"]) $("#page").addEventListener(genre, (ev) => {
  const c = CHAMPS_AGENT[ev.target.id];
  if (c && etat.agForm) etat.agForm[c] = ev.target.type === "checkbox" ? ev.target.checked : ev.target.value;
});
$("#page").addEventListener("input", (ev) => {
  if (ev.target.id === "jr-recherche") { etat.recherche = ev.target.value; rendreJournal(); }
  if (ev.target.id === "np-nom" && $("#np-gh-nom")) $("#np-gh-nom").placeholder = nomDepot(ev.target.value);
  // typing a repository picks its option
  const m = /^(np|gh)-gh-(nom|depot)$/.exec(ev.target.id);
  if (m) { const r = document.querySelector(`input[name=${m[1]}-gh][value=${m[2] === "nom" ? "creer" : "existant"}]`); if (r) r.checked = true; }
});

// --- lock
function afficherVerrou() {
  const v = $("#verrou"); v.hidden = !etat.verrouille;
  if (etat.verrouille) { const e = (etat.vue && etat.vue.etat) || {}; $("#verrou-etat").textContent = etat.vue && etat.vue.actif ? t("Claude keeps working{0}.", [e.cycle ? " (" + t("cycle {0}", [e.cycle]) + ")" : ""]) : t("The pilot is stopped."); setTimeout(() => $("#verrou-mdp").focus(), 50); }
}
$("#verrou-form").addEventListener("submit", async (ev) => {
  ev.preventDefault();
  try { await api("deverrouiller", "POST", { mot_de_passe: $("#verrou-mdp").value }); etat.verrouille = false; $("#verrou-mdp").value = ""; $("#verrou-erreur").textContent = ""; afficherVerrou(); rafraichir(); rendrePage(); }
  catch (e) { $("#verrou-erreur").textContent = e.message; }
});
$("#btn-verrou").addEventListener("click", async () => { await action(api("verrouiller", "POST")); etat.verrouille = true; afficherVerrou(); });

// --- refresh loop
async function rafraichir() {
  try {
    const v = await fetch("/api/etat" + (etat.projet ? "?p=" + encodeURIComponent(etat.projet) : ""), { headers: { "X-Athanor": CSRF } }).then((r) => r.json());
    etat.vue = v;
    if (v.langue && v.langue !== etat.langue) { etat.langue = v.langue; appliquerLangue(v.langue); traduireNav(); }
    if ((v.theme || "") !== etat.theme || (v.accent || "") !== etat.accent) { etat.theme = v.theme || ""; etat.accent = v.accent || ""; appliquerTheme(etat.theme, etat.accent); }
    if (!v.verrouille && v.bienvenue && etat.assistant < 0) { etat.assistant = 0; rendreAssistant(); }
    if (v.verrouille !== etat.verrouille) { etat.verrouille = v.verrouille; afficherVerrou(); if (!v.verrouille) rendrePage(); }
    rendreBandeau(v);
    annoncerFins(v);
    if (!v.verrouille) {
      await lireJournal();
      // the Team page follows live, except while text is selected in it (a brief, a report)
      if ((etat.page === "equipe" && !String(window.getSelection() || "") && !etat.agForm) || (etat.page === "suivi" && !document.activeElement.matches("input, textarea, select"))) rendrePage();
      if (etat.page === "chat") majChat(false);
      if (etat.page === "pourvous" && !document.activeElement.matches("input, textarea") && ++etat.pvTic % 4 === 0) {
        const r = await api("pourvous").catch(() => null);
        if (r && JSON.stringify(r.taches || []) !== etat.pvSignature) rendrePage();
      }
      annoncerTaches(v);
      // the checks and the output update in place: the page itself is never rebuilt under the user's typing
      if (etat.page === "sante" && (document.querySelector(".verif.v-en_cours") || etat.tacheChoisie)) await majSante();
    }
  } catch (e) { /* server briefly unreachable */ }
}

(async () => {
  await rafraichir();
  const v = etat.vue || {};
  etat.langue = v.langue || "en"; appliquerLangue(etat.langue); traduireNav();
  if (location.hash === "#bienvenue") location.hash = "#suivi";
  naviguer();
  setInterval(rafraichir, 1500);
})();
