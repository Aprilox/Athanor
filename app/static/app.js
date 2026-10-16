/* Athanor — interface. Vanilla JS, no dependency. All strings go through t(). */
"use strict";

const CSRF = document.querySelector('meta[name="csrf"]').content;
const $ = (s, r = document) => r.querySelector(s);
const h = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
// pilot phase (STATE.json) -> status dot class
const classePhase = (x) => !x || !x.actif ? "" : ({ travail: "travaille", quota: "quota", erreur: "erreur", connexion: "erreur", entre_cycles: "entre_cycles" }[x.phase] || "travaille");
// escaped text with `code` spans rendered (the plan and requests are Markdown-ish)
const md = (s) => h(s).replace(/`([^`]+)`/g, "<code>$1</code>");
// a milestone as the pilot reports it is the full plan line: find its short title in the plan
// Milestone state for the plan lists: done, in progress (the first unchecked one, or in parallel mode
// every milestone a worker is building), or to do.
function etatDuJalon(j, premierLibre) {
  if (j.fait) return "coche";
  const ouvriers = ((etat.vue && etat.vue.etat && etat.vue.etat.ouvriers) || "").split(/\s+/).filter(Boolean);
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
function rendreJalon(j, etatJalon, compact) {
  const ico = j.fait ? "✔" : etatJalon === "en-cours" ? "▶" : "○";
  return `${j.vague ? `<li class="vague">${h(j.vague)}</li>` : ""}<li class="${etatJalon}" title="${h(j.complet)}"><span class="jalon-ico">${ico}</span><div class="jalon-corps"><div class="jalon-titre">${md(j.titre)}${j.detail ? `<span class="jalon-detail"> — ${md(j.detail)}</span>` : ""}</div>${!compact && j.acceptation ? `<div class="jalon-accept">✓ ${md(j.acceptation)}</div>` : ""}</div></li>`;
}

// ----------------------------------------------------------------------------- state
const etat = {
  page: "suivi", projet: "", vue: null, verrouille: false, langue: "en", theme: "", accent: "", assistant: -1, demarrage: {},
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

// ----------------------------------------------------------------------------- confirmation dialog
// The app's own dialog: the browser's confirm()/alert() would show the page address and look like a web page.
// confirmer(message, { ok, annuler, danger }) → Promise<boolean>
function confirmer(message, options = {}) {
  const fond = $("#dialogue"), ok = $("#dialogue-ok"), annuler = $("#dialogue-annuler");
  $("#dialogue-texte").textContent = message;
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

// Desktop app, not a web page: no browser context menu outside the text fields (copy/paste stay available there).
document.addEventListener("contextmenu", (e) => { if (!e.target.closest("input, textarea, [contenteditable], .journal, pre, code")) e.preventDefault(); });
// …and none of the browser's own shortcuts (reload, print, find, view source, developer tools, downloads).
document.addEventListener("keydown", (e) => {
  const k = e.key.toLowerCase(), ctrl = e.ctrlKey || e.metaKey;
  if (["f3", "f5", "f7", "f12"].includes(k) || (ctrl && ["r", "p", "f", "g", "h", "j", "u", "o", "s"].includes(k)) || (ctrl && e.shiftKey && ["i", "j", "c"].includes(k))) e.preventDefault();
}, true);

// ----------------------------------------------------------------------------- "Improve with Claude"
// Under a request text area: Claude rewrites the request (clearer, richer, same intent); the user's
// version is kept and both can be swapped at will. Edits made to either version are kept too.
const versionsDemande = new WeakMap(); // textarea → { mienne, claude, vue: "mienne" | "claude" }
function barreAmeliorer(id) {
  return `<div class="ameliorer" data-pour="${id}">
    <button type="button" class="btn btn-petit" data-ameliorer="${id}" title="${h(t("Claude rewrites your request: clearer, more complete, same intent. You can go back to yours."))}">✨ ${t("Improve with Claude")}</button>
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
  if (!zone.value.trim()) { toast(t("Write your request first"), "erreur"); zone.focus(); return; }
  const avant = versionsDemande.get(zone);
  const mienne = avant ? avant.mienne : zone.value; // always rewrite from the user's own text
  const etiquette = document.querySelector(`[data-etat-ameliorer="${zone.id}"]`);
  const libelleBouton = bAm.innerHTML;
  bAm.disabled = true; zone.readOnly = true; zone.classList.add("en-attente");
  bAm.textContent = t("Claude is rewriting your request…");
  if (etiquette) etiquette.textContent = t("about 30 seconds");
  try {
    const r = await api("ameliorer", "POST", { texte: mienne });
    versionsDemande.set(zone, { mienne, claude: r.texte, vue: "claude" });
    zone.value = r.texte;
    toast(t("Request improved: check it, edit it, or go back to yours."), "ok");
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
    case "entre_cycles": return { classe: "entre_cycles", titre: t("Between two cycles"), sous: t("Cycle {0} is done, the next one starts with a fresh context.", [e.cycle]) };
    case "erreur": return { classe: "erreur", titre: t("Error, retry scheduled"), sous: e.etat };
    case "connexion": return { classe: "connexion", titre: t("Connection problem"), sous: t("Check the token in Settings, or go through Setup & health.") };
    case "termine": return { classe: "", titre: t("Project finished"), sous: t("The deliverable and the final report are in the project folder.") };
    default: return { classe: "", titre: e.etat ? t(e.etat) : t("Pilot started"), sous: t("cycle {0}", [e.cycle || 0]) };
  }
}

// ----------------------------------------------------------------------------- shell: header, sidebar, project picker
function rendreBandeau(v) {
  const p = phraseEtat(v);
  const boutons = [];
  if (v.kit_valide && v.projet) {
    if (v.actif) {
      boutons.push(`<button class="btn" data-act="arreter-apres-cycle" title="${h(t("Finishes the current milestone, saves it, then stops. Nothing is lost."))}">${t("Stop after cycle")}</button>`);
      boutons.push(`<button class="btn btn-danger" data-act="arreter" title="${h(t("Cuts the current cycle: unsaved work will be redone."))}">${t("Stop now")}</button>`);
    } else if (v.kit_installe && !v.demande_vide) {
      // right after a click, the pilot takes a few seconds to show up: no second start
      const enDemarrage = (etat.demarrage[v.projet.nom] || 0) > Date.now();
      boutons.push(`<button class="btn btn-primaire" data-act="demarrer" ${enDemarrage ? "disabled" : ""}>${enDemarrage ? t("Starting…") : t("Start")}</button>`);
    }
    if (v.bureau && v.bureau.disponible) boutons.push(`<button class="btn" data-bureau="voir" title="${h(t("Go and see what Claude is showing (windows on the “Claude” desktop)"))}">${t("View Claude's desktop")}</button>`);
  }
  const nomProjet = v.projet ? `<div class="muet petit">${h(v.projet.nom)} · ${h(v.projet.dossier)}</div>` : "";
  const nbActifs = (v.projets || []).filter((x) => x.actif).length;
  $("#bandeau").innerHTML = `<div class="etat-phrase">${nomProjet}<h1><span class="point ${p.classe}"></span><span>${h(p.titre)}</span></h1><div class="sous">${h(p.sous)}</div></div><div class="actions">${boutons.join("")}</div>`;
  $("#nav-sous").textContent = nbActifs ? t("{0} project(s) running", [nbActifs]) : t("idle");
  $("#nav-kit").textContent = v.kit || "";
  const sel = $("#sel-projet");
  sel.setAttribute("aria-expanded", etat.selecteurOuvert ? "true" : "false");
  sel.innerHTML = v.projet ? `<span class="dot ${classePhase(v.projet)}"></span><span class="nom">${h(v.projet.nom)}</span><span class="chevron">▾</span>` : `<span class="nom muet">${t("Choose a project")}</span><span class="chevron">▾</span>`;
  const lien = $('#nav-liens a[data-page="demandes"]');
  lien.innerHTML = t("Requests") + (v.attente ? ` <span class="pastille">${v.attente}</span>` : "");
  $("#btn-verrou").hidden = !v.mot_de_passe;
  if (v.projet && etat.projet !== v.projet.nom) etat.projet = v.projet.nom;
  rendreSelecteur(v);
}

function rendreSelecteur(v) {
  const m = $("#menu-projets"); if (!m) return;
  m.hidden = !etat.selecteurOuvert;
  if (!etat.selecteurOuvert) return;
  const q = (etat.rechercheProjet || "").toLowerCase();
  const liste = (v.projets || []).filter((x) => !q || x.nom.toLowerCase().includes(q) || x.dossier.toLowerCase().includes(q));
  const avaitFocus = document.activeElement && document.activeElement.id === "sel-recherche";
  const pos = avaitFocus ? document.activeElement.selectionStart : 0;
  m.innerHTML = `<input type="text" id="sel-recherche" placeholder="${h(t("Search a project…"))}" value="${h(etat.rechercheProjet || "")}">
    <div class="menu-liste">${liste.map((x) => `<a href="#suivi" data-projet="${h(x.nom)}" class="${x.courant ? "actif" : ""}"><span class="dot ${classePhase(x)}"></span><span class="nom">${h(x.nom)}</span><span class="muet petit">${x.actif ? t(x.phase === "quota" ? "waiting" : "running") : t("stopped")}${x.attente ? " · " + t("{0} queued", [x.attente]) : ""}</span></a>`).join("") || `<div class="vide">${t("No project.")}</div>`}</div>
    <a href="#nouveau" class="menu-nouveau">+ ${t("New project")}</a>`;
  const r = $("#sel-recherche"); if (r) { r.focus(); r.setSelectionRange(pos, pos); }
}

$("#bandeau").addEventListener("click", async (ev) => {
  const b = ev.target.closest("button"); if (!b) return;
  if (b.dataset.act) { b.disabled = true; if (b.dataset.act === "demarrer") etat.demarrage[etat.projet] = Date.now() + 25000; await action(api("pilote/" + b.dataset.act, "POST")); setTimeout(rafraichir, 800); }
  if (b.dataset.bureau) await action(api("bureau/" + b.dataset.bureau, "POST"));
});
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
  const libelles = { suivi: t("Overview"), equipe: t("Team"), demandes: t("Requests"), journal: t("Log"), sante: t("Setup & health"), reglages: t("Settings"), options: t("Options") };
  document.querySelectorAll("#nav-liens a, #nav-outil a").forEach((a) => { if (libelles[a.dataset.page]) a.textContent = libelles[a.dataset.page]; });
  $("#nav-titre-projet").textContent = t("Project");
  $("#nav-titre-outil").textContent = t("Tool");
  $("#btn-verrou").textContent = "🔒 " + t("Lock");
  $("#verrou-titre").textContent = t("Application locked");
  $("#verrou-mdp").placeholder = t("Password");
  $("#verrou-bouton").textContent = t("Unlock");
}

function naviguer() {
  const page = (location.hash || "#suivi").slice(1);
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
  if (b.dataset.assistant === "fin") { await action(api("preferences", "POST", { bienvenue_vue: true })); etat.assistant = -1; rendreAssistant(); location.hash = "#suivi"; rendrePage(); }
});

// --- working mode of the project (serial lead, or parallel workers + merge)
async function rendreMode() {
  const m = await api("mode").catch(() => null); if (!m) return "";
  return `<div class="bloc"><h2>${t("Working mode")}</h2>
    <label class="interrupteur"><input type="checkbox" id="md-parallele" ${m.parallele ? "checked" : ""}><span><b>${t("Parallel mode")}</b> — ${t("the lead writes a fine-grained plan with dependencies; workers build independent milestones at the same time, each in its own git working copy; the pilot merges each one as soon as it finishes and starts the next, and the lead verifies and fixes. Much faster; uses more quota (one session per worker).")}</span></label>
    <div class="ligne ${m.parallele ? "" : "muet"}"><span class="petit">${t("Workers at once")}</span><div class="choix m0">${[2, 3, 4, 6, 8, 10].map((n) => `<button type="button" data-max="${n}" class="${m.parallele_max === n ? "actif" : ""}" ${m.parallele ? "" : "disabled"}>${n}</button>`).join("")}</div><span class="muet petit">${t("Applies from the next cycle.")}</span></div>
  </div>`;
}
async function enregistrerMode(parallele, max) {
  const r = await action(api("mode", "POST", { parallele, parallele_max: max }));
  if (r) rendrePage();
}

pages.suivi = async () => {
  const v = etat.vue; if (!v) return "";
  if (!v.kit_installe || !v.projet || v.demande_vide) return rendreGuide(v) + (v.projet ? await rendreMode() : "");
  const e = v.etat || {};
  const plan = v.plan || [];
  const faits = plan.filter((j) => j.fait).length;
  const stats = v.stats || [];
  const somme = (k) => stats.reduce((a, s) => a + (Number(s[k]) || 0), 0);
  const cache = somme("cache_lu"), plein = somme("entree_tokens") + somme("cache_ecrit");
  const ratio = cache + plein ? Math.round((cache / (cache + plein)) * 100) : 0;
  let premier = true;
  return `
  <div class="grille grille-2">
    <div class="bloc"><h2>${t("Right now")}</h2>
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
    <div class="bloc"><h2>${t("Project plan")}</h2>
      ${plan.length ? `<div class="barre"><i style="width:${(faits / plan.length) * 100}%"></i></div><div class="muet petit">${t("{0} / {1} milestones done", [faits, plan.length])}</div>
      <ul class="plan">${plan.map((j) => { const c = etatDuJalon(j, () => premier ? (premier = false, true) : false); return rendreJalon(j, c, false); }).join("")}</ul>`
        : `<div class="vide">${t("No plan yet: Claude writes it during the first cycle (framing).")}</div>`}
    </div>
  </div>
  <div class="bloc"><h2>${t("Usage")}</h2>
    ${stats.length ? `<div class="chiffres">
      <div class="chiffre"><b>${stats.filter((s) => !String(s.cycle).includes("·")).length}</b><span>${t("cycles")}</span></div>
      <div class="chiffre"><b>${Math.floor(somme("minutes") / 60)} h ${String(Math.round(somme("minutes") % 60)).padStart(2, "0")}</b><span>${t("of work")}</span></div>
      <div class="chiffre"><b>${somme("tours")}</b><span>${t("turns")}</span></div>
      <div class="chiffre"><b>${somme("cout_usd").toFixed(2)} $</b><span>${t("at list price (your subscription quota pays)")}</span></div>
      <div class="chiffre"><b>${ratio} %</b><span>${t("of tokens re-read from cache")}</span></div>
      <div class="chiffre"><b>${fmtTokens(somme("sortie_tokens"))}</b><span>${t("tokens produced")}</span></div>
      <div class="chiffre"><b>${fmtTokens(somme("contexte_max") / stats.length)}</b><span>${t("average peak context")}</span></div>
    </div>
    <div class="tableau mt-2"><table class="tab"><thead><tr><th>${t("Cycle")}</th><th>${t("Time")}</th><th>${t("Model")}</th><th>${t("State")}</th><th>${t("Minutes")}</th><th>${t("Turns")}</th><th>${t("Cost $")}</th><th>${t("Peak context")}</th></tr></thead><tbody>
    ${stats.slice(-25).reverse().map((s) => `<tr><td>${s.cycle}</td><td>${h((s.horodatage || "").slice(11, 16))}</td><td>${h(s.template)}</td><td class="${h(s.etat)}">${h(s.etat)}</td><td>${Math.round(s.minutes)}</td><td>${s.tours}</td><td>${Number(s.cout_usd).toFixed(2)}</td><td>${fmtTokens(s.contexte_max)}</td></tr>`).join("")}
    </tbody></table></div>` : `<div class="vide">${t("Numbers appear at the end of the first cycle.")}</div>`}
  </div>
  ${await rendreMode()}`;
};

function rendreGuide(v) {
  const e1 = v.kit_installe, e2 = !!v.projet, e3 = e2 && !v.demande_vide;
  return `<div class="bloc"><h2>${t("First steps")}</h2><p class="intro">${t("Five steps, all from this window. Then you can let it run for days, on several projects at once.")}</p>
  <div class="etapes">
    <div class="etape ${e1 ? "faite" : ""}"><div class="n">1</div><div><b>${t("Install the tooling")}</b><div class="d">${t("WSL2 (the Linux built into Windows), Claude Code, the system sandbox. A window opens; Linux asks for a password once.")}</div></div>
      <button class="btn ${e1 ? "" : "btn-primaire"}" data-installer>${e1 ? t("Run again") : t("Install")}</button></div>
    <div class="etape"><div class="n">2</div><div><b>${t("Check and connect")}</b><div class="d">${t("Checks all the tooling and the token of your Claude subscription. Every problem has its own “Repair” or “Ask Claude” button.")}</div></div>
      <a class="btn" href="#sante">${t("Open the check")}</a></div>
    <div class="etape ${e2 ? "faite" : ""}"><div class="n">3</div><div><b>${t("Add a project")}</b><div class="d">${t("Its name, its development folder (anywhere on the PC), and what you want: a game, a website, an app, a tool. The tool only writes in a .athanor subfolder, to delete at the end.")}</div></div>
      <a class="btn ${e1 && !e2 ? "btn-primaire" : ""}" href="#nouveau">${e2 ? t("Another project") : t("Add a project")}</a></div>
    <div class="etape ${e3 ? "faite" : ""}"><div class="n">4</div><div><b>${t("Say what you want")}</b><div class="d">${t("If not done yet: describe the expected result. Technologies are free unless you impose some.")}</div></div>
      <a class="btn ${e2 && !e3 ? "btn-primaire" : ""}" href="#demandes" ${e2 ? "" : 'aria-disabled="true"'}>${t("Write the request")}</a></div>
    <div class="etape"><div class="n">5</div><div><b>${t("Start")}</b><div class="d">${t("Claude frames, plans milestones, builds, checks and delivers, cycle after cycle. You follow everything here and can add requests along the way.")}</div></div>
      <button class="btn ${e3 ? "btn-primaire" : ""}" data-act="demarrer" ${e3 ? "" : "disabled"}>${t("Start")}</button></div>
  </div></div>`;
}

// --- new project
pages.nouveau = async () => `<div class="grille grille-2">
    <div class="bloc"><h2>${t("New project")}</h2>
      <p class="intro petit">${t("A project = a development folder, anywhere on the PC (existing or new). The tool only adds a .athanor subfolder (request, plan, log, checks): delete it at the end and the project is clean.")}</p>
      <label class="champ"><span>${t("Name")}</span><input type="text" id="np-nom" placeholder="${h(t("e.g. watch-store"))}"></label>
      <label class="champ"><span>${t("Development folder")}</span><input type="text" id="np-dossier" placeholder="${h(t("e.g. D:\\Dev\\watch-store"))}"></label>
      <label class="champ"><span>${t("What you want (you can complete it later)")}</span><textarea id="np-demande" rows="7" placeholder="${h(t("Describe the expected result, for whom, on which platform. Technologies are free unless you impose one."))}"></textarea></label>
      ${barreAmeliorer("np-demande")}
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
pages.equipe = async () => {
  const agents = await api("agents").catch(() => []);
  const v = etat.vue || {};
  if (!agents.length) return `<div class="vide">${t("The team appears here as soon as the pilot works: the project lead and the sub-agents it calls, with what each one does and says.")}</div>`;
  if (!agents.some((a) => a.id === "chef")) agents.unshift({ id: "chef", nom: "", mission: "", statut: v.actif ? "travaille" : "disponible", tours: 0 });
  const initiales = (n) => n.split(/\s+/).map((x) => x[0]).join("").slice(0, 2).toUpperCase();
  const libelle = { travaille: t("working"), attend: t("waiting"), termine: t("finished"), disponible: t("available") };
  const nomAgent = (a) => a.id === "chef" ? t("Project lead") : t(a.nom);
  // Available agents: a short, translated role. Working ones: the task as Claude wrote it (already in the
  // project's language), never half-translated by the prefix rules; workers keep their translated label.
  const ROLES = {
    architect: "Requirements, architecture and the milestone plan",
    creator: "Content: story, world, dialogue, texts, generated assets",
    developer: "One precise development task, with its tests",
    tester: "Automated tests, screenshots, bug reports",
    critic: "Uses the result like a demanding end user",
    executor: "Mechanical work, on the cheap model",
  };
  const mission = (a) => {
    if (a.id === "chef") return t("Leads the current milestone: reads the plan, codes, checks, commits.");
    const role = ROLES[String(nomAgent(a) || "").toLowerCase()];
    if (a.statut === "disponible" && role) return t(role);
    if (/^Builds milestone /.test(a.mission || "")) return t(a.mission);
    return a.mission || "";
  };
  return `<p class="intro">${t("Each card is a Claude session: what it does, its last words, and its report when done. The project lead reviews the sub-agents' work before going on.")} ${t("The lead works alone by default and calls a sub-agent for framing, big independent chunks, testing and the final critic.")}</p>
  <div class="equipe">${agents.map((a) => `
    <div class="agent ${a.id === "chef" ? "chef" : ""} ${h(a.statut)}">
      <div class="agent-tete"><div class="avatar">${h(initiales(nomAgent(a)))}</div>
        <div class="agent-identite"><div class="agent-nom">${h(nomAgent(a))}</div><div class="agent-mission" title="${h(mission(a) || "")}">${h(mission(a) || "")}</div></div>
        <div class="agent-statut ${h(a.statut)}">${libelle[a.statut] || h(a.statut)}${a.statut !== "disponible" ? " · " + (a.tours === 1 ? t("1 turn") : t("{0} turns", [a.tours])) : ""}${a.contexte ? " · " + fmtTokens(a.contexte) + " tokens" : ""}</div></div>
      ${a.bulle ? `<div class="bulle">${h(a.bulle.length > 700 ? a.bulle.slice(0, 700) + "…" : a.bulle)}</div>` : ""}
      ${a.statut === "disponible" ? "" : `<div class="activite">${h(t(a.activite || "—"))}</div>`}
    </div>`).join("")}</div>`;
};

// --- requests
pages.demandes = async () => {
  const d = await api("demandes");
  const plan = d.plan || [];
  let premier = true;
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
        <button class="btn btn-primaire" id="nd-ajouter">${t("Add to the queue")}</button>
      </div>
      <div class="bloc"><h2>${t("Note along the way")}</h2>
        <p class="intro petit">${t("To correct without waiting: Claude reads it at the start of the next cycle, during the current request.")}</p>
        <textarea id="rq-texte" rows="2" placeholder="${h(t("e.g. the footstep sounds are too high-pitched, lower them."))}"></textarea>
        <div class="ligne mt-1"><button class="btn" id="rq-ajouter">${t("Add the note")}</button></div>
      </div>
      <div class="bloc"><details><summary>${t("REQUEST.md, the full file")}</summary>
        <textarea id="dm-texte" class="code" rows="18">${h(d.demande)}</textarea>
        <div class="ligne mt-1"><button class="btn" id="dm-enregistrer">${t("Save the file")}</button><button class="btn" data-ouvrir="demande">${t("Open in the editor")}</button></div></details></div>
    </div>
    <div class="colonne">
      <div class="bloc"><h2>${t("Where things stand")}</h2>
      <div class="flux">
        ${d.faites.map((f) => `<div class="carte-demande faite"><div class="num">✔</div><div class="corps"><div class="titre">${h(f.titre)}</div><div class="meta">${t("done")} · ${h(f.date)}</div></div></div>`).join("")}
        ${enCours ? `<div class="carte-demande en-cours"><div class="num">▶</div><div class="corps"><div class="titre">${h(titreDemande)}</div><div class="meta">${etat.vue && etat.vue.actif ? t("in progress") : t("current request, pilot stopped")}</div>
          ${plan.length ? `<ul class="plan jalons-mini">${plan.map((j) => { const c = etatDuJalon(j, () => premier ? (premier = false, true) : false); return rendreJalon(j, c, true); }).join("")}</ul>` : ""}
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
pages.sante = async () => {
  const verifs = await api("sante");
  const ico = (v) => v.statut === "ok" ? ["✔", "ok"] : v.statut === "en_cours" ? ["◌", "en_cours"] : v.statut === "echec" ? (v.facultatif ? ["!", "facultatif"] : ["✖", "echec"]) : ["○", ""];
  return `<div class="ligne">
    <button class="btn btn-primaire" id="st-tout">${t("Check everything")}</button>
    <button class="btn" id="st-installer" title="${h(t("The full installer in a window: WSL2, network, packages (password), Claude Code, administrator rights. Once."))}">${t("Install everything")}</button>
    <button class="btn" id="st-barriere" title="${h(t("A short Claude session tries to leave the project, read your files, launch cmd.exe: everything must be blocked."))}">${t("Test the sandbox (2 min)")}</button>
  </div>
  <div class="bloc bloc-liste">${verifs.map((v) => { const [i, c] = ico(v); return `
    <div class="verif v-${h(v.statut)} ${v.facultatif ? "v-facultatif" : ""} ${etat.verifChoisie === v.id ? "choisi" : ""}" data-id="${h(v.id)}" data-tache="${h(v.tache_id)}">
      <div class="ico ${c}">${i}</div>
      <div><div class="titre" data-voir="${h(v.tache_id)}" title="${h(t(v.explication))}">${h(t(v.titre))}</div><div class="resume">${h(v.en_reparation ? t("repair in progress…") : v.resume || t(v.explication))}</div></div>
      <div class="ligne">
        ${v.statut === "echec" && !v.en_reparation ? `${v.reparation_auto ? `<button class="btn btn-petit" data-reparer title="${h(t(v.reparation_info))}">${t("Repair")}</button>` : ""}<button class="btn btn-petit" data-claude>${t("Ask Claude")}</button>` : ""}
        <button class="btn btn-petit" data-verifier ${v.statut === "en_cours" ? "disabled" : ""}>${t("Check")}</button>
      </div></div>`; }).join("")}</div>
  <div class="bloc"><h2>${t("Ask Claude")}</h2>
    <p class="intro petit">${t("A Claude session in the tool folder, outside the sandbox and without sudo: it installs, fixes, then summarizes. Whatever needs sudo or Windows is written to logs\\TODO_FOR_YOU.md.")}</p>
    <div class="ligne"><input type="text" id="st-libre" class="croit" placeholder="${h(t("e.g. install Godot 4 · why does the pilot stop?"))}"><button class="btn" id="st-libre-btn">${t("Send")}</button></div>
    <div class="ligne mt-1"><input type="text" id="st-complement" class="croit" placeholder="${h(t("Detail attached to repairs (optional): what you already tried, since when…"))}"></div>
  </div>
  <div id="st-sortie"></div>`;
};

async function rendreSortie() {
  const el = $("#st-sortie"); if (!el || !etat.tacheChoisie) return;
  const r = await api("tache/" + etat.tacheChoisie).catch(() => null); if (!r) return;
  const tk = r.tache;
  const st = { en_cours: [t("running"), "quota"], ok: [t("finished"), "ok"], echec: [t("failed, code {0}", [tk.code]), "echec"], jamais: ["", ""] }[tk.statut] || ["", ""];
  el.innerHTML = `<div class="bloc"><div class="ligne"><h2 class="m0">${h(t(tk.nom))}</h2><span class="${st[1]}">${st[0]} · ${tk.duree_s} s</span><span class="espace"></span><button class="btn btn-petit" id="st-copier">${t("Copy output")}</button></div>
    <div class="sortie">${r.evenements.map((e) => `<div class="ev g-${h(e.genre)} ${e.sous_agent ? "sous" : ""}"><span class="h"></span><span class="m"></span><span class="t">${h(t(e.titre))}</span></div>`).join("") || '<div class="ev"><span class="h"></span><span class="m"></span><span class="t muet">…</span></div>'}</div></div>`;
  const s = $(".sortie", el); s.scrollTop = s.scrollHeight;
  $("#st-copier").onclick = () => navigator.clipboard.writeText(tk.sortie.join("\n")).then(() => toast(t("Copied."), "ok"));
}

// --- settings (.env)
pages.reglages = async () => {
  const lignes = await api("env" + (etat.envSecrets ? "?secrets=1" : ""));
  const modeles = ["", "fable", "claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "haiku"];
  const efforts = ["", "low", "medium", "high", "xhigh", "max"];
  const champ = (l) => {
    const id = "env-" + l.cle;
    if (["MODEL", "FALLBACK_MODEL", "SUBAGENT_MODEL", "CHEAP_MODEL"].includes(l.cle)) return `<input type="text" id="${id}" data-cle="${h(l.cle)}" value="${h(l.valeur)}"><div class="choix">${modeles.map((m) => `<button type="button" data-pour="${id}" data-val="${h(m)}" class="${l.valeur === m ? "actif" : ""}">${m || t("account default")}</button>`).join("")}</div>`;
    if (l.cle === "EFFORT") return `<input type="text" id="${id}" data-cle="${h(l.cle)}" value="${h(l.valeur)}"><div class="choix">${efforts.map((m) => `<button type="button" data-pour="${id}" data-val="${h(m)}" class="${l.valeur === m ? "actif" : ""}">${m || t("normal")}</button>`).join("")}</div>`;
    if (["RTK", "UNREAL_MCP", "CLOSE_AFTER_MILESTONE"].includes(l.cle) && (l.valeur === "0" || l.valeur === "1")) return `<label class="interrupteur m0"><input type="checkbox" id="${id}" data-cle="${h(l.cle)}" ${l.valeur === "1" ? "checked" : ""}> <span>${l.valeur === "1" ? t("enabled") : t("disabled")}</span></label>`;
    return `<input type="${l.secret && !etat.envSecrets ? "password" : "text"}" id="${id}" data-cle="${h(l.cle)}" value="${h(l.valeur)}" ${l.secret ? 'autocomplete="off"' : ""}>`;
  };
  const actif = etat.vue && etat.vue.actif;
  return `<div class="ligne">
    <p class="muet m0 croit">${t("The pilot reads these settings when it starts. The token gives access to your subscription: never share it.")}</p>
    <span class="espace"></span>
    <label class="interrupteur m0"><input type="checkbox" id="rg-secrets" ${etat.envSecrets ? "checked" : ""}> <span>${t("Show secrets")}</span></label>
    <button class="btn btn-primaire" id="rg-enregistrer">${t("Save")}</button>
    ${actif ? `<button class="btn" id="rg-redemarrer" title="${h(t("Saves, lets the current milestone finish, then restarts the pilot with the new settings."))}">${t("Save and restart after cycle")}</button>` : ""}
  </div>
  <div class="bloc">${lignes.map((l) => `<div class="reglage"><div><div class="cle">${h(l.cle)}</div><div class="aide">${h(t(l.aide))}</div></div><div>${champ(l)}</div></div>`).join("")}</div>`;
};

function lireEnvFormulaire() {
  const v = {};
  document.querySelectorAll("[data-cle]").forEach((i) => { v[i.dataset.cle] = i.type === "checkbox" ? (i.checked ? "1" : "0") : i.value; });
  return v;
}

// --- options (saved as soon as a switch or a field changes)
pages.options = async () => {
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
        <label class="interrupteur"><input type="checkbox" id="op-windows" data-option ${o.demarrage_windows ? "checked" : ""}><span>${t("Open the app when Windows starts (without a window)")}</span></label>
        <p class="muet petit">${t("Pilots run in WSL in the background, without a window. Remember to disable the PC's sleep mode.")}</p>
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
        ${(o.projets || []).length ? `<table class="tab"><tbody>${o.projets.map((p) => `<tr><td><b>${h(p.nom)}</b></td><td class="muet">${h(p.dossier)}</td><td class="droite"><button class="btn btn-petit btn-danger" data-retirer-projet="${h(p.nom)}">${t("Remove from list")}</button></td></tr>`).join("")}</tbody></table>` : `<div class="vide">${t("No project.")}</div>`}
        <p class="muet petit mt-2">${t("Removing a project never touches its folder. To clean it, delete its .athanor subfolder.")}</p>
      </div>
      <div class="bloc"${b.message === "Virtual desktop: Windows only." ? " hidden" : ""}><h2>${t("“Claude” virtual desktop")}</h2>
        <label class="interrupteur"><input type="checkbox" id="op-bureau" data-option ${o.bureau_virtuel ? "checked" : ""}><span>${t("Send the windows opened by the pilots to a dedicated desktop")}</span></label>
        <p class="petit ${b.disponible ? "ok" : "attention"}">${h(t(b.message || ""))}</p>
        <div class="ligne"><button class="btn btn-petit" data-bureau="voir" ${b.disponible ? "" : "disabled"}>${t("View Claude's desktop")}</button><button class="btn btn-petit" data-bureau="revenir" ${b.disponible ? "" : "disabled"}>${t("Come back")}</button><button class="btn btn-petit" data-bureau="ramener" ${b.disponible ? "" : "disabled"}>${t("Bring the windows back here")}</button><span class="muet petit">${t("{0} window(s) moved", [b.deplacees || 0])}</span></div>
        ${(b.vues || []).length ? `<ul class="muet petit mt-1">${b.vues.slice(0, 10).map((f) => `<li>${h(f)}</li>`).join("")}</ul>` : ""}
        <label class="champ mt-2"><span>${t("Recognized: programs inside the projects and in tools\\, “Athanor” windows, and these executables (one per line)")}</span><textarea id="op-programmes" data-option class="code" rows="5">${h(o.programmes_supplementaires)}</textarea></label>
        <p class="muet petit">${t("Windows shortcut to switch desktops: Ctrl + Win + ← / →.")}</p>
      </div>
      <div class="bloc"><h2>${t("Tool folder")}</h2>
        <input type="text" id="op-kit" data-option value="${h(o.kit)}"><div class="muet petit mt-1">${o.kit_valide ? "✔ " + t("tool recognized (system\\pilot.sh present)") : "✖ " + t("this folder is not the tool")}</div>
        <div class="ligne mt-1"><button class="btn btn-petit" data-ouvrir="kit">${t("Open the folder")}</button><button class="btn btn-petit" data-ouvrir="logs">${t("Logs folder")}</button><span class="espace"></span><button class="btn btn-petit" id="op-guide">${t("Show the first-launch guide again")}</button></div>
      </div>
    </div>
  </div>`;
};
async function enregistrerOptions() {
  const r = await action(api("options", "POST", { kit: $("#op-kit").value, lancer_pilote_au_demarrage: $("#op-lancer").checked, bureau_virtuel: $("#op-bureau").checked, programmes_supplementaires: $("#op-programmes").value, demarrage_windows: $("#op-windows").checked }));
  if (r && document.activeElement && document.activeElement.matches("input[type=checkbox]")) rendrePage();
}

// ----------------------------------------------------------------------------- rendering and events
async function rendrePage() {
  const p = pages[etat.page] || pages.suivi;
  try { $("#page").innerHTML = await p(); } catch (e) { if (e.message !== t("Locked")) $("#page").innerHTML = `<div class="vide">${h(e.message)}</div>`; }
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

$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("button, .titre[data-voir], .ev.cliquable, .explorateur div[data-chemin]"); if (!b) return;
  const page = etat.page;
  if (b.dataset.langue) return changerLangue(b.dataset.langue);
  if (b.dataset.theme) return changerApparence(b.dataset.theme, etat.accent);
  if (b.dataset.accent) return changerApparence(etat.theme, b.dataset.accent);
  if (b.dataset.ouvrir) return action(api("ouvrir", "POST", { quoi: b.dataset.ouvrir }));
  if (b.dataset.bureau) return action(api("bureau/" + b.dataset.bureau, "POST")).then(rendrePage);
  if (b.dataset.act) { b.disabled = true; if (b.dataset.act === "demarrer") etat.demarrage[etat.projet] = Date.now() + 25000; await action(api("pilote/" + b.dataset.act, "POST")); return setTimeout(rafraichir, 800); }
  if (b.hasAttribute("data-installer")) return action(api("sante/installer", "POST"));
  if (b.dataset.max && page === "suivi") return enregistrerMode($("#md-parallele").checked, Number(b.dataset.max));
  if (page === "nouveau") {
    if (b.dataset.chemin) return rendreExplorateur(b.dataset.chemin);
    if (b.id === "np-utiliser") { $("#np-dossier").value = etat.explorateur; return; }
    if (b.id === "np-creer") {
      if (b.disabled) return;
      b.disabled = true; // one project per click, even on a double-click
      const r = await action(api("projets", "POST", { nom: $("#np-nom").value, dossier: $("#np-dossier").value, demande: $("#np-demande").value }));
      b.disabled = false;
      if (r) { etat.projet = r.nom; etat.evenements = []; etat.dernierJournal = 0; await rafraichir(); location.hash = "#suivi"; }
      return;
    }
  }
  if (page === "demandes") {
    const carte = b.closest("[data-fichier]");
    // one request / note per click, even on a double-click
    if (b.id === "nd-ajouter") { if (b.disabled) return; b.disabled = true; const r = await action(api("demandes", "POST", { titre: $("#nd-titre").value, texte: $("#nd-texte").value })); b.disabled = false; if (r) rendrePage(); return; }
    if (b.id === "rq-ajouter") { if (b.disabled) return; b.disabled = true; const r = await action(api("remarque", "POST", { texte: $("#rq-texte").value })); b.disabled = false; if (r) rendrePage(); return; }
    if (b.id === "dm-enregistrer") return action(api("demande", "PUT", { texte: $("#dm-texte").value }));
    if (carte && b.hasAttribute("data-monter")) return reordonner(carte.dataset.fichier, -1);
    if (carte && b.hasAttribute("data-descendre")) return reordonner(carte.dataset.fichier, 1);
    if (carte && b.hasAttribute("data-retirer")) { if (await confirmer(t("Remove this request from the queue?"), { ok: t("Remove"), danger: true })) { await action(api("demandes/" + encodeURIComponent(carte.dataset.fichier), "DELETE")); rendrePage(); } return; }
  }
  if (page === "journal" && b.classList.contains("ev")) { const n = Number(b.dataset.n); etat.ouverts.has(n) ? etat.ouverts.delete(n) : etat.ouverts.add(n); etat.suiviAutoScroll = false; return rendreJournal(); }
  if (page === "sante") {
    const ligne = b.closest(".verif"); const id = ligne && ligne.dataset.id;
    if (b.id === "st-tout") { await action(api("sante/verifier", "POST", { id: "tout" })); return rendrePage(); }
    if (b.id === "st-installer") return action(api("sante/installer", "POST"));
    if (b.id === "st-barriere") { const r = await action(api("sante/barriere", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-libre-btn") { const r = await action(api("sante/libre", "POST", { texte: $("#st-libre").value })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; rendrePage(); } return; }
    if (b.dataset.voir !== undefined) { etat.tacheChoisie = b.dataset.voir; etat.verifChoisie = id; return rendrePage(); }
    if (b.hasAttribute("data-verifier")) { const r = await action(api("sante/verifier", "POST", { id })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
    if (b.hasAttribute("data-reparer")) { const r = await action(api("sante/reparer", "POST", { id })); if (r && r.tache) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
    if (b.hasAttribute("data-claude")) { const r = await action(api("sante/claude", "POST", { id, complement: $("#st-complement").value })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
  }
  if (page === "reglages") {
    if (b.dataset.pour) { const i = $("#" + b.dataset.pour); i.value = b.dataset.val; b.parentElement.querySelectorAll("button").forEach((x) => x.classList.toggle("actif", x === b)); return; }
    if (b.id === "rg-enregistrer") return action(api("env", "POST", lireEnvFormulaire()));
    if (b.id === "rg-redemarrer") { const r = await action(api("env", "POST", lireEnvFormulaire())); if (r) await action(api("pilote/redemarrer-apres-cycle", "POST")); return; }
  }
  if (page === "options") {
    if (b.dataset.retirerProjet) { if (await confirmer(t("Remove “{0}” from the list? Its folder stays intact.", [b.dataset.retirerProjet]), { ok: t("Remove from list"), danger: true })) { await action(api("projets/" + encodeURIComponent(b.dataset.retirerProjet), "DELETE")); if (etat.projet === b.dataset.retirerProjet) etat.projet = ""; await rafraichir(); rendrePage(); } return; }
    if (b.id === "op-guide") { await action(api("preferences", "POST", { bienvenue_vue: false })); etat.assistant = 0; return rendreAssistant(); }
    if (b.id === "mdp-enregistrer") { const r = await action(api("mot-de-passe", "POST", { ancien: ($("#mdp-ancien") || {}).value || "", nouveau: $("#mdp-nouveau").value })); if (r) { toast(r.defini ? t("Lock enabled: the padlock is at the bottom left.") : t("Lock removed."), "ok"); rendrePage(); rafraichir(); } return; }
  }
});

$("#page").addEventListener("change", (ev) => {
  const i = ev.target;
  if (i.dataset.filtre) { etat.filtres[i.dataset.filtre] = i.checked; rendreJournal(); }
  if (i.id === "rg-secrets") { etat.envSecrets = i.checked; rendrePage(); }
  if (i.type === "checkbox" && i.dataset.cle) i.nextElementSibling.textContent = i.checked ? t("enabled") : t("disabled");
  if (etat.page === "options" && i.hasAttribute("data-option")) enregistrerOptions();
  if (i.id === "md-parallele") { const actif = $("#page .choix button.actif"); enregistrerMode(i.checked, actif ? Number(actif.dataset.max) : 3); }
});
$("#page").addEventListener("input", (ev) => { if (ev.target.id === "jr-recherche") { etat.recherche = ev.target.value; rendreJournal(); } });

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
    if (!v.verrouille) {
      await lireJournal();
      if (etat.page === "equipe" || (etat.page === "suivi" && !document.activeElement.matches("input, textarea"))) rendrePage();
      if (etat.page === "sante" && (document.querySelector(".verif.v-en_cours") || etat.tacheChoisie)) { const tk = etat.tacheChoisie; await rendrePage(); etat.tacheChoisie = tk; }
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
