/* Athanor — interface. Vanilla JS, no dependency. All strings go through t(). */
"use strict";

const CSRF = document.querySelector('meta[name="csrf"]').content;
const $ = (s, r = document) => r.querySelector(s);
const h = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
// pilot phase (STATE.json) -> status dot class
const classePhase = (x) => !x || !x.actif ? "" : ({ travail: "travaille", quota: "quota", budget: "quota", attente_vous: "quota", erreur: "erreur", connexion: "erreur", entre_cycles: "entre_cycles" }[x.phase] || "travaille");
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
  const ico = j.fait ? "✔" : etatJalon === "en-cours" ? "▶" : j.etape ? "◆" : "○";
  const cle = "jalon:" + (j.id || j.titre), ouvert = etat.ouverts.has(cle);
  const peutEditer = !j.fait && !(etatJalon === "en-cours" && etat.vue && etat.vue.actif); // "next up" while stopped is still editable
  const details = ouvert ? `<div class="jalon-details">
      ${j.etape && !j.fait ? `<div class="muet">${t("Next stage: planned in detail when the previous one is done.")}</div>` : ""}
      ${j.detail ? `<div>${md(j.detail)}</div>` : ""}
      ${j.fichiers ? `<div><span class="muet">${t("Files")}</span> <code>${h(j.fichiers)}</code></div>` : ""}
      ${j.depend && j.depend.length ? `<div><span class="muet">${t("Depends on")}</span> ${j.depend.map((d) => `<code>${h(d)}</code>`).join(" ")}</div>` : ""}
      ${j.acceptation ? `<div><span class="muet">${t("Acceptance")}</span> ${md(j.acceptation)}</div>` : ""}
      ${j.notes && j.notes.length ? `<ul class="jalon-notes">${j.notes.map((n) => `<li>${md(n)}</li>`).join("")}</ul>` : ""}
      ${j.id ? `<div class="ligne jalon-actions">
        ${peutEditer ? `<button class="btn btn-petit" data-jalon="${h(j.id)}" data-jalon-action="monter" title="${h(t("Move up"))}">↑</button><button class="btn btn-petit" data-jalon="${h(j.id)}" data-jalon-action="descendre" title="${h(t("Move down"))}">↓</button><button class="btn btn-petit btn-danger" data-jalon="${h(j.id)}" data-jalon-action="retirer">${t("Remove from the plan")}</button>` : `<span class="muet petit">${j.fait ? t("Done: it stays in the plan.") : t("Being built: stop after the cycle to change it.")}</span>`}
      </div>` : ""}
    </div>` : "";
  return `${j.vague ? `<li class="vague">${h(j.vague)}</li>` : ""}<li class="${etatJalon} ${ouvert ? "ouvert" : ""}${j.etape ? " etape" : ""}" data-jalon-cle="${h(cle)}"><span class="jalon-ico">${ico}</span><div class="jalon-corps"><div class="jalon-titre">${md(j.titre)}<span class="jalon-chevron">${ouvert ? "▾" : "▸"}</span></div>${details}</div></li>`;
}

// A plan in stages (a large request): "Stage 2/5 · " before its progress, from the project's view.
const texteEtape = (x) => x && x.etapes ? t("Stage {0}/{1}", [x.etape, x.etapes]) + " · " : "";

// The plan's list: the milestones the pilot archived (done long ago) are folded into one line that opens them.
function listePlan(plan, compact) {
  let premier = true;
  const archives = plan.filter((j) => j.archive), reste = plan.filter((j) => !j.archive);
  const ouvert = etat.ouverts.has("plan:archives");
  const ligne = (j) => rendreJalon(j, etatDuJalon(j, () => premier ? (premier = false, true) : false), compact);
  const tete = archives.length ? `<li class="coche plan-archives" data-plan-archives><span class="jalon-ico">✔</span><div class="jalon-corps"><div class="jalon-titre">${t("{0} milestone(s) done earlier", [archives.length])}<span class="jalon-chevron">${ouvert ? "▾" : "▸"}</span></div></div></li>` : "";
  return tete + (ouvert ? archives.map(ligne).join("") : "") + reste.map(ligne).join("");
}

// Auto-scroll of the log (follows the new lines) and of the plan (keeps the milestone being built in view):
// one small switch each, remembered on this computer. Scrolling up in the log pauses it, back at the end resumes it.
function lireDefilAuto() {
  const d = { journal: true, plan: false };
  try { Object.assign(d, JSON.parse(localStorage.getItem("athanor.defilAuto") || "{}")); } catch (e) { /* storage off */ }
  return d;
}
function ecrireDefilAuto() { try { localStorage.setItem("athanor.defilAuto", JSON.stringify(etat.defilAuto)); } catch (e) { /* storage off */ } }
// a mouse with its wheel: scrolling (not a download)
const ICONE_DEFIL = '<svg viewBox="0 0 16 16" width="14" height="14" aria-hidden="true"><rect x="4.25" y="1.75" width="7.5" height="12.5" rx="3.75" fill="none" stroke="currentColor" stroke-width="1.5"/><path class="molette" d="M8 4.6v2.3" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>';
function boutonDefil(quoi) {
  const actif = etat.defilAuto[quoi];
  const titre = quoi === "plan" ? (actif ? t("Auto-scroll on: the plan keeps the milestone being built in view. Click to turn it off.") : t("Auto-scroll off: the plan stays where you leave it. Click to follow the milestone being built."))
    : (actif ? t("Auto-scroll on: the log follows the new lines. Click to turn it off (scrolling up pauses it too).") : t("Auto-scroll off: the log stays where you leave it. Click to follow the new lines."));
  return `<button type="button" class="defil-auto ${actif ? "actif" : ""}" data-defil="${quoi}" title="${h(titre)}" aria-label="${h(titre)}" aria-pressed="${actif}">${ICONE_DEFIL}</button>`;
}

// ----------------------------------------------------------------------------- state
const etat = {
  pieces: {}, // files waiting to be attached, per form
  pvTic: 0, hubTic: 0, hubSig: "", accueilTexte: "", preRempli: null, pvAnnonces: {}, pvSignature: "",
  pvForm: {}, // answers being written on the For you page (text and secret values, per task; memory only)
  apprForm: {}, // choices being made on an approval of For you (vault entry, environment, variable; never the password)
  // page: none until naviguer() reads the address (no flash of another page at load)
  page: "", projet: "", vue: null, verrouille: false, langue: "en", theme: document.documentElement.dataset.theme || "", accent: document.documentElement.dataset.accent || "", assistant: -1, visite: -1, demarrage: {},
  dernierJournal: 0, evenements: [], ouverts: new Set(),
  filtres: { pilote: true, texte: true, outil: true, resultat: false, sous_agent: true, brut: false }, recherche: "",
  tacheChoisie: null, verifChoisie: null, envSecrets: false, defilAuto: lireDefilAuto(), explorateur: "", selecteurOuvert: false, rechercheProjet: "",
};

// ----------------------------------------------------------------------------- api
function messageDe(r) {
  if (!r) return "";
  if (r.message_key) return t(r.message_key, r.args || []);
  return r.message || "";
}
async function api(chemin, methode = "GET", corps) {
  if (methode !== "GET") etat.hubLu = 0; // a change: the next lireHub() asks again
  if (etat.projet) chemin += (chemin.includes("?") ? "&" : "?") + "p=" + encodeURIComponent(etat.projet);
  const r = await fetch("/api/" + chemin, {
    method: methode, headers: { "X-Athanor": CSRF, "Content-Type": "application/json" },
    body: corps === undefined ? undefined : JSON.stringify(corps),
  });
  const data = await r.json().catch(() => ({}));
  if (r.status === 401 && data.erreur_key === "Not paired") { afficherNonAppaire(); throw new Error(t("Not paired")); }
  if (r.status === 401 && data.erreur_key === "Locked") { etat.verrouille = true; afficherVerrou(); throw new Error(t("Locked")); }
  if (!r.ok) throw Object.assign(new Error(data.erreur_key ? t(data.erreur_key, data.args || []) : (data.erreur || t("Error {0}", [r.status]))), { data });
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
  fond.classList.toggle("dialogue-large", !!options.large);
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

// this computer's connection to GitHub (outil_github_connexion.go): one sign-in, kept, for every project
function rendreConnexionGH(c) {
  if (!c) return `<p class="muet petit">${t("Updating…")}</p>`;
  if (c.connecte) return `<p class="ok m0">✔ ${t("Connected as {0}", [h(c.login)])}${c.lie_le ? ` <span class="muet petit">· ${h(t("since {0}", [dateLisible(c.lie_le)]))}</span>` : ""}</p>
    <p class="muet petit">${t("Every project of this computer is linked to a repository your account can push to (yours, or one you are a collaborator of) without signing in again: Manage › GitHub › Use one of my repositories. Only creating a new repository opens a sign-in.")}</p>
    <p class="muet petit">${t("Athanor keeps a key “Athanor (this computer)” on your GitHub account (Settings › SSH and GPG keys), out of Claude's reach. It opens every repository your account can push to.")}</p>
    <div class="ligne"><button class="btn btn-petit" data-connexion-gh="connecter">${t("Connect again")}</button><button class="btn btn-petit btn-danger" data-connexion-gh="deconnecter">${t("Disconnect")}</button></div>
    <div class="mt-1"><p class="m0">${c.acces_garde ? `<span class="ok">✔</span> ${t("Create repositories without a window: on")}` : `<span class="muet">○</span> ${t("Create repositories without a window: off")}`}</p>
      <p class="muet petit m0">${t("Creating a repository goes through GitHub's API, which the key cannot reach: by default it opens a sign-in each time, and nothing of it is kept. On: one sign-in is kept on this computer, out of Claude's reach, and used only to create your repositories. It opens your account's repositories: keeping it is keeping that power on the disk (revocable on GitHub, Settings › Applications).")}</p>
      <div class="ligne mt-1">${c.acces_garde ? `<button class="btn btn-petit" data-connexion-gh="oublier-acces">${t("Forget this access")}</button>` : `<button class="btn btn-petit" data-connexion-gh="garder-acces">${t("Keep the access (one sign-in)")}</button>`}</div></div>`;
  return `<p class="muet petit m0">${t("Connect this computer to your GitHub account once: then every project is linked to one of your repositories, or one you are a collaborator of, without signing in again.")}</p>
    <div class="ligne mt-1"><button class="btn btn-primaire" data-connexion-gh="connecter">${t("Connect to GitHub")}</button></div>`;
}
// connexionConnueGH: what is known of the connection; read once in the background (the page follows)
function connexionConnueGH() {
  if (etat.ghConnexion || etat.ghConnexionLu) return etat.ghConnexion;
  etat.ghConnexionLu = true;
  majConnexionGH().then((c) => { if (c && ["nouveau", "projet", "suivi"].includes(etat.page)) rendrePage(); });
  return null;
}
async function majConnexionGH() {
  const c = await api("github/connexion").catch(() => null);
  if (c) etat.ghConnexion = c;
  const b = document.getElementById("bloc-connexion-gh"); if (b) b.innerHTML = "<h2>GitHub</h2>" + rendreConnexionGH(etat.ghConnexion || { connecte: false });
  return c;
}
document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-connexion-gh]"); if (!b) return;
  const act = b.dataset.connexionGh;
  if (act === "deconnecter" && !(await confirmer(t("Disconnect this computer from GitHub? The projects that use this connection stay linked: they push again as soon as this computer is connected again. The key stays listed in GitHub's Settings › SSH and GPG keys, where you can remove it."), { ok: t("Disconnect"), danger: true }))) return;
  b.disabled = true;
  await action(api("github/connexion", "POST", { action: act }));
  b.disabled = false;
  etat.ghTest = null; // a former test said what the connection was then
  if (act === "connecter") etat.ghConnexionSuivi = Date.now() + 5 * 60000;
  if (act === "garder-acces") etat.ghAccesSuivi = Date.now() + 5 * 60000;
  majConnexionGH(); if (etat.page === "projet") majGitHub();
});
// what linking with the connection did (lier-compte); false when nothing is linked
async function resultatLierCompte(r, corps) {
  const l = r && r.lie_compte; if (!l) return !!r;
  if (l.resultat === "workflows") {
    const ok = await confirmer(t("{0} runs {1} GitHub Actions workflow(s): every push runs them on the code Claude writes, with the repository's secrets if it has any (a secret able to change other repositories could be used). Prefer a repository without secrets. Link anyway?", [l.depot, l.workflows]), { ok: t("Link anyway"), danger: true });
    if (!ok) return false;
    return resultatLierCompte(await action(api("github", "POST", Object.assign({}, corps, { workflows_ok: true }))), corps);
  }
  const textes = {
    lie: [t("Linked to {0} (branch {1}) with your GitHub connection.", [l.depot, l.branche]), "ok"],
    "lie-sans-equipe": [t("Linked to {0}, but not joined to the other computers: {1}", [l.depot, l.detail || ""]), "erreur"],
    introuvable: [t("Your GitHub account ({0}) cannot see {1}: check the name, or ask its owner to add you as a collaborator.", [l.login || "?", l.depot]), "erreur"],
    refuse: [t("Your GitHub account ({0}) can read {1} but not push to it: its owner must give you write access (Settings › Collaborators).", [l.login || "?", l.depot]), "erreur"],
    reseau: [t("GitHub cannot be reached (network): try again in a moment."), "erreur"],
    deconnecte: [t("This computer is not connected to GitHub: Settings › Options › GitHub."), "erreur"],
  }[l.resultat] || [t("Not linked: {0}", [l.detail || l.resultat]), "erreur"];
  toast(textes[0], textes[1]);
  if (l.resultat === "lie" && l.detail === "ancienne-cle") toast(t("The repository's previous key stays listed in its Settings › Deploy keys, where you can remove it."), "ok");
  return l.resultat === "lie" || l.resultat === "lie-sans-equipe";
}

// working on one repository in turns (outil_github_recuperer.go): GitHub's work brought in, or only counted
async function recupererGitHub(verifier) {
  return api("github", "POST", { action: verifier ? "verifier" : "recuperer" }).catch((e) => { if (!verifier) toast(e.message, "erreur"); return null; });
}
// what bringing GitHub's work in did; true when the pilot can start after it
function annoncerRecuperation(r) {
  const n = r.resultat;
  const textes = {
    "a-jour": [t("Already up to date with GitHub."), "ok"],
    "fusion": [t("{0} commit(s) of GitHub brought into the project.", [r.detail || "?"]), "ok"],
    "rejoint": [t("The project starts from GitHub's work ({0}).", [r.detail || ""]), "ok"],
    "aucun": [t("GitHub's branch {0} has no commit yet: nothing to bring in.", [r.branche || "main"]), "ok"],
    "conflit": [t("GitHub's work and this folder's change the same lines: nothing was changed yet. Claude merges them at the start of the next cycle, keeping both."), "ok"],
    "sale": [t("This folder has changes not committed: save them first (Save and push the work in progress), then get GitHub's work."), "erreur"],
    "pilote": [t("The pilot is running: stop it first (Stop after cycle), then get GitHub's work."), "erreur"],
  }[n] || [t("GitHub's work not brought in: {0}", [r.detail || n || "?"]), "erreur"];
  toast(textes[0], textes[1]);
  return ["a-jour", "fusion", "rejoint", "aucun", "conflit"].includes(n);
}
// before a start: a project behind its repository (someone pushed meanwhile) asks first; GitHub slow or
// unreachable: the start goes on (at most 25 s)
async function avantDemarrageGitHub() {
  const v = await Promise.race([recupererGitHub(true), new Promise((r) => setTimeout(() => r(null), 25000))]);
  if (!v || v.equipe || v.resultat !== "verifie" || !(v.derriere > 0)) return true;
  const quand = v.quand ? dateLisible(v.quand) : "";
  const c = await choisir((v.sans_lien ? t("GitHub's branch {0} has a history of its own, nothing in common with this project's: {1} commit(s), the last one “{2}” by {3}, {4}. Usually the README, licence or .gitignore GitHub adds when it creates the repository.", [v.branche || "main", v.derriere, v.sujet || "?", v.auteur || "?", quand])
    : t("GitHub has {0} commit(s) this folder does not have: someone pushed since your last work (the last one: “{1}”, by {2}, {3}). Bring them in before starting?", [v.derriere, v.sujet || "?", v.auteur || "?", quand]))
    + (v.devant > 0 ? " " + t("This folder also has {0} commit(s) of its own: both are merged.", [v.devant]) : "")
    + " " + t("Starting from here instead: the pushes are refused until GitHub's work is brought in."), [
    { id: "recuperer", texte: t("Bring them in, then start"), primaire: true }, { id: "ici", texte: t("Start from here") }, { id: "", texte: t("Cancel") }],
    { titre: t("Not up to date with GitHub") });
  if (c === "ici") return true;
  if (c !== "recuperer") return false;
  const r = v.sans_lien ? await api("github", "POST", { action: "integrer" }).catch((e) => { toast(e.message, "erreur"); return null; }) : await recupererGitHub();
  return !!r && annoncerRecuperation(r);
}
// GitHub's branch has commits the project has not (a repository made on GitHub with a README, someone's
// push): bring them in (GitHub's files, when its history shares nothing with the project's), or replace
// GitHub's branch with the project, as the user sees it now
async function resoudreBrancheGitHub() {
  const v = await recupererGitHub(true);
  if (!v || v.resultat !== "verifie") { toast(t("GitHub cannot be read now: try again in a moment."), "erreur"); return; }
  if (!(v.derriere > 0)) { toast(t("Already up to date with GitHub."), "ok"); return; }
  const quand = v.quand ? dateLisible(v.quand) : "";
  const intro = v.sans_lien
    ? t("GitHub's branch {0} has a history of its own, nothing in common with this project's: {1} commit(s), the last one “{2}” by {3}, {4}. Usually the README, licence or .gitignore GitHub adds when it creates the repository.", [v.branche || "main", v.derriere, v.sujet || "?", v.auteur || "?", quand])
    : t("GitHub's branch {0} has {1} commit(s) this project does not have (the last one: “{2}”, by {3}, {4}).", [v.branche || "main", v.derriere, v.sujet || "?", v.auteur || "?", quand]);
  const options = [{ id: "integrer", texte: v.sans_lien ? t("Bring GitHub's files in") : t("Bring them in"), primaire: true }];
  if (!v.equipe) options.push({ id: "remplacer", texte: t("Replace GitHub's branch with this project"), danger: true });
  options.push({ id: "", texte: t("Cancel") });
  const c = await choisir(intro + " " + (v.equipe ? t("Several computers work on this project: GitHub's branch holds their work, it is never replaced.") : t("Bring them into the project (nothing is lost on either side), or replace GitHub's branch with this project?")), options, { titre: t("GitHub's branch") });
  if (c === "integrer") {
    const r = await api("github", "POST", { action: v.sans_lien ? "integrer" : "recuperer" }).catch((e) => { toast(e.message, "erreur"); return null; });
    if (r) annoncerRecuperation(r);
    return majGitHub();
  }
  if (c !== "remplacer") return;
  if (!(await confirmer(t("Replace GitHub's branch {0} with this project? Its {1} commit(s) that this project does not have leave the branch (the last one: “{2}”). Nothing of this project is lost.", [v.branche || "main", v.derriere, v.sujet || "?"]), { ok: t("Replace"), danger: true }))) return;
  const r = await action(api("github", "POST", { action: "remplacer", valeur: v.sha }));
  if (r && r.github && r.github.dernier) { const d = r.github.dernier; toast(t(d.modele || d.message || "", d.args || []), d.resultat === "ok" ? "ok" : "erreur"); }
  majGitHub();
}

// Start: always there. When the tooling was not seen installed or the request is empty, the user is asked
// first (install, open the check, or start anyway): nobody stays stuck on the first steps.
async function demarrerPilote(b) {
  const v = etat.vue || {};
  // Several computers: the request comes with the others' work, the pilot brings it in
  if (v.demande_vide && !(v.github && v.github.equipe)) {
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
  const avant = b ? b.textContent : "";
  if (b) b.textContent = t("Checking GitHub…");
  const ok = await avantDemarrageGitHub();
  if (b) { b.textContent = avant; b.disabled = ok; }
  if (!ok) return;
  etat.demarrage[etat.projet] = Date.now() + 25000;
  await action(api("pilote/demarrer", "POST"));
  setTimeout(rafraichir, 800);
}

// ----------------------------------------------------------------------------- confirmation dialog
// The app's own dialog: the browser's confirm()/alert() would show the page address and look like a web page.
// confirmer(message, { ok, annuler, danger, mdp }) → Promise<boolean> (mdp: a password field, the password or false)
function confirmer(message, options = {}) {
  const fond = $("#dialogue"), ok = $("#dialogue-ok"), annuler = $("#dialogue-annuler"), champ = $("#dialogue-mdp");
  $("#dialogue-texte").textContent = message;
  champ.hidden = !options.mdp; champ.value = ""; champ.setAttribute("aria-label", t("Athanor's password"));
  $("#dialogue-titre").textContent = options.titre || "";
  $("#dialogue-titre").hidden = !options.titre;
  ok.textContent = options.ok || t("OK");
  annuler.textContent = options.annuler || t("Cancel");
  ok.className = "btn " + (options.danger ? "btn-danger" : "btn-primaire");
  const avant = document.activeElement;
  fond.hidden = false;
  (options.mdp ? champ : ok).focus();
  return new Promise((resoudre) => {
    const fin = (valeur) => {
      if (options.mdp && valeur) valeur = champ.value; // mdp: the password typed, or false
      champ.value = "";
      fond.hidden = true;
      ok.onclick = annuler.onclick = fond.onclick = null;
      document.removeEventListener("keydown", clavier, true);
      if (avant && avant.focus) avant.focus();
      resoudre(valeur);
    };
    const clavier = (e) => {
      if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); fin(false); }
      else if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); fin(document.activeElement !== annuler); }
      else if (e.key === "Tab") { e.preventDefault(); const l = options.mdp ? [champ, annuler, ok] : [annuler, ok]; l[(l.indexOf(document.activeElement) + (e.shiftKey ? l.length - 1 : 1)) % l.length].focus(); }
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
    : /^Maximum duration reached/.test(p.fin.message || "") ? t("It reached its work time limit (in Settings, or in Manage › Working mode for this project). The work in progress is in the project folder: raise the limit or choose Unlimited, then start it again.")
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
    // a sub-agent's name and description: its card in the chat, or the Team page's form
    const carte = bAm.closest("[data-chat-agent-carte]"), champ = (n, id) => (carte ? carte.querySelector(`[data-ca="${n}"]`) : $(id)) || {};
    const r = await api("ameliorer", "POST", agent ? { texte: mienne, genre: "agent", nom: champ("nom", "#ag-nom").value || "", description: champ("description", "#ag-description").value || "" } : { texte: mienne });
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
// the same commit, from its short ids ("abc1234 subject": two repositories may shorten them differently)
function memeCommit(a, b) {
  const x = String(a || "").split(" ")[0], y = String(b || "").split(" ")[0], n = Math.min(x.length, y.length);
  return n >= 7 && x.slice(0, n) === y.slice(0, n);
}

// the Overview's GitHub line: the last commit GitHub has, under the local one; why a push did not go
function ligneGitHubSuivi(g, local) {
  if (!g) return "";
  if (!g.lie) return `<dt>${t("Last commit on GitHub")}</dt><dd class="muet">${t("not linked to GitHub")}</dd>`;
  const env = g.envoye, d = g.dernier;
  let txt = env ? `${h(env.commit)} <span class="petit">· ${h(heureMaj(env.quand))}</span>` : t("nothing pushed yet");
  const echec = d && d.resultat !== "ok" && d.resultat !== "attente" && (!env || d.quand > env.quand);
  if (env && memeCommit(env.commit, local)) txt += ` <span class="petit">✓ ${t("up to date")}</span>`;
  else if (local && !echec) txt += ` <span class="petit">· ${t("the local one is not pushed yet")}</span>`;
  if (!g.auto) txt += ` <span class="petit">· ${t("automatic push off")}</span>`;
  const msg = echec ? `<div class="quota petit">${h(t(d.modele || "", d.args || []))}</div>` : "";
  return `<dt>${t("Last commit on GitHub")}</dt><dd class="muet">${txt}${msg}</dd>`;
}

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
    case "travail": return { classe: "travaille", titre: e.ouvriers ? t("{0} workers in parallel: {1}", [e.ouvriers.split(/\s+/).length, e.ouvriers.split(/\s+/).join(", ")]) : /^Merging/.test(e.etat || "") ? t("The lead merges the workers' branches") : e.jalon_en_cours ? t("Claude is working on {0}", [titreJalon(e.jalon_en_cours)]) : t("Claude is working"), sous: [e.debut_cycle && t("for {0}", [depuis(e.debut_cycle)]), t("cycle {0}", [e.cycle]), e.template, v.eco && t("eco mode"), v.rapide && t("fast mode"), v.contexte && t("context {0} tokens", [fmtTokens(v.contexte)])].filter(Boolean).join(" · ") };
    case "quota": return { classe: "quota", titre: t("Usage limit reached, Claude is waiting"), sous: t("Resumes {0} · cycle {1}", [e.reprise_prevue || t("as soon as possible"), e.cycle]) };
    case "budget": return { classe: "quota", titre: t("API budget or credit used up, Claude is waiting"), sous: t("Raise the budget, count again (API spend, below) or put another API key in Settings: the pilot resumes at once.") };
    case "attente_vous": return { classe: "quota", titre: t("Claude is waiting for you"), sous: t("{0} task(s) on the For you page: the pilot resumes as soon as you answer one.", [v.pour_vous || 0]) };
    case "entre_cycles": return { classe: "entre_cycles", titre: t("Between two cycles"), sous: t("Cycle {0} is done, the next one starts with a fresh context.", [e.cycle]) };
    case "erreur": return { classe: "erreur", titre: t("Error, retry scheduled"), sous: e.etat };
    case "connexion": return { classe: "connexion", titre: t("Connection problem"), sous: t("Check the token or the API key in Settings, or go through Setup & health: a new one is used at once.") };
    case "termine": return { classe: "", titre: t("Project finished"), sous: t("The deliverable and the final report are in the project folder. To go on, add a request or a note, then Start.") };
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
  const b = v.bureau || {}, act = [boutonCloche(v, "fb-action"), boutonGuide("fb-action")];
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

// ----------------------------------------------------------------------------- every project at once: the bell, the All projects page
// what a project waits for from the user (null: nothing): its For you tasks, a connection to fix, the API budget
function besoinProjet(x) {
  // the rule lives in Go (besoinProjet, hub_dev.go): only its text and its page here
  switch (x.besoin) {
    case "pour_vous": return { texte: t("{0} task(s) on the For you page", [x.pour_vous]), page: "pourvous" };
    case "attente_vous": return { texte: t("Claude is waiting for you"), page: "pourvous" };
    case "connexion": return { texte: t("Connection problem"), page: "suivi" };
    case "budget": return { texte: t("API budget or credit used up, Claude is waiting"), page: "suivi" };
  }
  return null;
}
function phraseProjet(x) {
  if (!x.actif) return x.phase === "termine" ? t("Project finished") : t("Pilot stopped");
  return { travail: t("Claude is working"), quota: t("Usage limit reached, Claude is waiting"), budget: t("API budget or credit used up, Claude is waiting"), attente_vous: t("Claude is waiting for you"), entre_cycles: t("Between two cycles"), erreur: t("Error, retry scheduled"), connexion: t("Connection problem") }[x.phase] || t("Pilot started");
}
const projetsVisibles = (v) => (v.projets || []).filter((x) => !x.archive);
// the bell: in Athanor's title bar (Windows), else at the end of the header; the alerts of every module (/api/hub)
const alertesHub = () => (etat.hub && etat.hub.alertes) || [];
function boutonCloche(v, classe) {
  const n = alertesHub().length;
  if (!n && !projetsVisibles(v).length && !((etat.hub || {}).ignorees || []).length) return "";
  const titre = n ? t("{0} alert(s) need you", [n]) : t("Nothing needs you right now.");
  return `<button type="button" class="${classe} cloche${n ? " sonne" : ""}" data-notifs aria-haspopup="true" aria-expanded="${etat.notifsOuvert ? "true" : "false"}" aria-label="${h(titre)}" title="${h(titre)}"><svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true"><path d="M6 8a6 6 0 0 1 12 0c0 7 3 9 3 9H3s3-2 3-9M10.3 21a1.94 1.94 0 0 0 3.4 0" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>${n ? `<span class="cloche-n">${n}</span>` : ""}</button>`;
}
// the Guide: a "?" beside the bell (it is no menu entry any more)
const boutonGuide = (classe) => `<a href="#guide" class="${classe} btn-guide"${etat.page === "guide" ? ' aria-current="page"' : ""} aria-label="${h(t("Guide"))}" title="${h(t("Guide"))}">?</a>`;
// one alert on one line, "Dev, ma-boutique: why", and its actions as the server gives them: a page (View, through
// allerProjet for a Dev project) or a POST (Ignore, Reopen, an approval's decision). From a paired device only the
// actions marked distance stay active (the server refuses the others: 403).
function ligneAlerte(a, ignoree) {
  const m = ((etat.hub || {}).modules || []).find((x) => x.id === a.module) || { nom: a.module };
  const texte = t("{0}, {1}: {2}", [t(m.nom), a.titre, t(a.raison, (a.args || []).map(texteMessage))]);
  const distant = (etat.vue || {}).distant;
  const actions = (a.actions || []).map((x, i) => x.page ? `<a class="btn btn-petit" href="${h(x.page)}"${x.projet ? ` data-projet="${h(x.projet)}" data-cible="${h(x.page.slice(1))}"` : ""}>${h(t(x.libelle))}</a>`
    : `<button type="button" class="btn btn-petit${x.id === "accepter" ? " btn-primaire" : ""}" data-alerte="${h(x.id)}" data-empreinte="${h(a.empreinte)}" data-i="${i}"${distant && !x.distance ? ` disabled title="${h(t("On the computer only"))}"` : ""}>${h(t(x.libelle))}</button>`).join("");
  return `<div class="alerte${ignoree ? " ignoree" : ""}${a.approbation ? " alerte-appr" : ""}" data-module="${h(a.module)}"><span class="dot${ignoree ? "" : " quota"}" aria-hidden="true"></span><span class="alerte-texte">${h(texte)}</span><span class="alerte-actions">${actions}</span></div>`;
}
function rendreNotifs(v) {
  const m = $("#notifs"), bouton = document.querySelector("[data-notifs]");
  m.hidden = !etat.notifsOuvert || !bouton;
  if (m.hidden) return;
  const liste = alertesHub(), cachees = (etat.hub || {}).ignorees || [];
  const html = `<div class="notifs-titre" id="notifs-titre">${t("Alerts")}</div>
    <div class="alertes">${liste.map((a) => ligneAlerte(a, false)).join("") || `<div class="muet petit notifs-vide">${t("Nothing needs you right now.")}</div>`}</div>
    ${cachees.length ? `<details class="alertes-ignorees"${etat.ignoreesOuvert ? " open" : ""}><summary>${t("Ignored alerts ({0})", [cachees.length])}</summary><div class="alertes">${cachees.map((a) => ligneAlerte(a, true)).join("")}</div></details>` : ""}
    <a href="#global" class="menu-nouveau">${t("Projects")} →</a>`;
  if (m.dataset.html !== html) { m.dataset.html = html; m.innerHTML = html; }
  const r = bouton.getBoundingClientRect();
  m.style.top = r.bottom + 6 + "px";
  m.style.right = Math.max(8, Math.min(window.innerWidth - r.right, window.innerWidth - m.offsetWidth - 8)) + "px"; // never past the left edge (phone)
}
function fermerNotifs(clavier) { etat.notifsOuvert = false; if (etat.vue) rendreBandeau(etat.vue); if (clavier) document.querySelector("[data-notifs]")?.focus(); }
document.addEventListener("click", (ev) => {
  if (ev.target.closest("[data-notifs]")) {
    etat.notifsOuvert = !etat.notifsOuvert; if (etat.vue) rendreBandeau(etat.vue);
    // opened from the keyboard (no pointer: detail 0): the list sits at the end of the page, so the focus goes into it
    if (etat.notifsOuvert && ev.detail === 0) $("#notifs").querySelector("a, button, summary")?.focus();
  }
  else if (etat.notifsOuvert && !ev.target.closest("#notifs")) fermerNotifs();
});
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && etat.notifsOuvert) fermerNotifs(true); });
// a project of the bell or of the All projects page: chosen, then the page it needs shown
async function allerProjet(nom, page) {
  etat.projet = nom; etat.evenements = []; etat.dernierJournal = 0; etat.ouverts.clear();
  await action(api("projets/choisir", "POST", { nom }));
  await rafraichir();
  if (etat.page !== page) location.hash = "#" + page; else rendrePage();
}
function cliqueProjet(ev) {
  const a = ev.target.closest("a[data-cible]"); if (!a) return;
  ev.preventDefault(); fermerNotifs();
  allerProjet(a.dataset.projet, a.dataset.cible);
}
$("#notifs").addEventListener("click", async (ev) => {
  if (ev.target.closest("summary")) { etat.ignoreesOuvert = !ev.target.closest("details").open; return; }
  const b = ev.target.closest("button[data-alerte]");
  if (b) { // the alert's action as the server describes it; the focus goes to the bell's first action left
    const a = [...alertesHub(), ...((etat.hub || {}).ignorees || [])].find((x) => x.empreinte === b.dataset.empreinte);
    const x = a && (a.actions || [])[b.dataset.i];
    if (!x) return;
    b.disabled = true;
    if (a.approbation) return deciderDepuisCloche(a.approbation, x, b);
    const r = await agirAlerte(x, b);
    if (r && r.page) { fermerNotifs(); return allerProjet(r.projet, r.page.slice(1)); } // I take over: the server's sheet
    etat.hub = await api("hub").catch(() => etat.hub);
    if (etat.vue) rendreBandeau(etat.vue);
    const suite = $("#notifs .alerte-actions .btn") || document.querySelector("[data-notifs]"); if (suite) suite.focus();
    return;
  }
  if (ev.target.closest("a") && !ev.target.closest("a[data-cible]")) fermerNotifs(); else cliqueProjet(ev);
});
// an alert's POST action (b its button, disabled by the caller), from the bell or a mission's incident card: the
// answer, null when cancelled or refused (the button is back)
async function agirAlerte(x, b) {
  // M137: a prod incident's Retry and Go back ask for the owner's accord first
  const preuve = x.accord ? await preuveMdp(t("{0} in production: retype Athanor's password.", [t(x.libelle)]), { ok: t(x.libelle) }, t("{0} in production?", [t(x.libelle)])) : {};
  if (preuve === false) { b.disabled = false; return null; }
  if (x.route.endsWith("/incident") && x.corps.action !== "main") { b.textContent = t("Working…"); toast(t("Working… the site is checked from this PC right after.")); }
  const r = await action(api(x.route, "POST", { ...x.corps, ...preuve }));
  if (r && r.deploiement) toast(r.deploiement.etat === "ok" ? t("Done: the site answers again.") : t(r.deploiement.erreur || "Failed"), r.deploiement.etat === "ok" ? "ok" : "erreur");
  if (!r) { b.disabled = false; b.textContent = t(x.libelle); }
  return r || null;
}
$("#page").addEventListener("click", cliqueProjet);
// an approval decided from the bell: an access needs its vault entry and the window a password, so those open
// the approval's card on For you; the others are sent at once
async function deciderDepuisCloche(ap, x, b) {
  const r = await api("hub/approbations").catch(() => ({}));
  if (((ap.action === "demander_acces" || (ap.details || {}).seule_cle === "oui") && x.id !== "refuser") || r.mot_de_passe_requis || r.appareil_requis) {
    fermerNotifs(); etat.apprCible = ap.id;
    if (ap.action === "mission") { location.hash = "#pourvous"; return; } // its project is no Dev project
    return allerProjet(ap.projet, "pourvous");
  }
  if (x.id === "toujours" && !(await confirmerToujours(ap))) { b.disabled = false; return; }
  if (!(await deciderApprobation(x.corps))) b.disabled = false;
}
const confirmerToujours = (ap) => confirmer(ap.action === "plan_serveur" ? t("Accepted, and the next plans of {0} on {1} that only change things (nothing destructive) are accepted without asking you.", [ap.projet, (ap.details || {}).nom || (ap.details || {}).serveur])
  : t("Accepted, and the next “{0}” requests of {1} in {2} are accepted without asking you.", [ap.action, ap.projet, ap.env]), { ok: t("Never ask again"), annuler: t("Cancel") });
// POST /api/hub/approbations, then the bell, the header and the page read again
async function deciderApprobation(corps) {
  const r = await action(api("hub/approbations", "POST", corps));
  if (!r) return null;
  toast(r.etat === "refusee" ? t("Refused: the agent is told and does without it.") : t("Accepted: the agent is told and carries on."), "ok");
  etat.hub = await api("hub").catch(() => etat.hub);
  if (etat.vue) rendreBandeau(etat.vue);
  if (etat.page === "pourvous" || etat.page === "accueil") rendrePage();
  return r;
}

// the header of a page outside a project: the module's name, and what runs and waits in every module (as the bell counts)
function phraseGlobale(m) {
  const n = alertesHub().length, actifs = ((etat.hub || {}).elements || []).filter((e) => e.etat === "actif").length;
  const page = m && m.id === "hub" ? m.pages.find((x) => x.hash === "#" + pageParente(etat.page)) : null; // Home, Projects
  return { classe: n ? "quota" : actifs ? "travaille" : "", titre: page ? t(page.libelle) : m ? t(m.nom) : etat.page === "guide" ? t("Guide") : "Athanor Hub", sous: [actifs ? t("{0} in progress", [actifs]) : t("idle"), n && t("{0} alert(s) need you", [n])].filter(Boolean).join(" · ") };
}
// the pages of a module that works on the selector's project (Studio): Start, access, the project's line
const avecProjet = (m) => !!m && !!m.projet;
// a rebuild keeps the keyboard focus on the same element (same identifying attributes), else the focus fell to the page
function cleFocus(x) {
  const s = ["id", "data-href", "data-page", "data-filtre", "data-act", "data-notifs", "data-guide", "data-projet", "data-cible", "href"].filter((a) => x.hasAttribute(a)).map((a) => `[${a}="${CSS.escape(x.getAttribute(a))}"]`).join("");
  return s && x.tagName.toLowerCase() + s;
}
function garderFocus(zone, rendre) {
  const a = document.activeElement, cle = a && a !== zone && zone.contains(a) ? cleFocus(a) : "";
  rendre();
  const b = cle && zone.querySelector(cle);
  if (b && b !== document.activeElement) b.focus({ preventScroll: true });
}

// ----------------------------------------------------------------------------- shell: header, sidebar, project picker
// the environment given to the pilot and what it receives, next to Start (summary computed by the server)
function blocAccesDemarrer(ac, distant) {
  const pl = (n) => new Intl.PluralRules(document.documentElement.lang || "en").select(n) !== "one";
  const dev = ac.env === "dev"; // C-M218-3: "test keys" only where they are test keys
  const recu = [ac.secrets && (pl(ac.secrets) ? t(dev ? "{0} test keys" : "{0} keys", [ac.secrets]) : t(dev ? "{0} test key" : "{0} key", [ac.secrets])),
    ac.variables && (pl(ac.variables) ? t("{0} variables", [ac.variables]) : t("{0} variable", [ac.variables]))].filter(Boolean);
  // C-M218-2: the badge also counts the server keys Athanor keeps, so the sentence says them
  const phrase = (recu.length ? t("The pilot will have access to: {0}.", [recu.join(", ")]) : t("The pilot receives nothing from the vault."))
    + (ac.infra ? " " + t(pl(ac.infra) ? "{0} server keys kept by Athanor" : "{0} server key kept by Athanor", [ac.infra]) + "." : "");
  const verrou = ac.etat === "verrouille" ? `<span class="badge badge-infra" title="${h(t("The vault is locked: the pilot's file is not updated until you unlock it."))}">🔒 ${t("vault locked")}</span>` : "";
  const envs = (ac.envs || []).map((e) => `<option value="${h(e)}" ${e === ac.env ? "selected" : ""}>${h(e)}</option>`).join("");
  const choix = distant ? `<span class="badge" title="${h(t("Environment"))}">${h(ac.env)}</span>` // a paired device only reads the grants
    : `<select data-env-acces aria-label="${h(t("Environment"))}" title="${h(t("Environment given to the pilot: changing it rewrites its values at once."))}">${envs}</select>`;
  return `<span class="acces-demarrer">${choix}`
    + `<a class="badge" href="#acces" title="${h(t("Open the Access panel"))}">🔑 ${pl(ac.total) ? t("{0} accesses", [ac.total]) : t("{0} access", [ac.total])}</a>${verrou}<span class="muet petit">${h(phrase)}</span></span>`;
}

function rendreBandeau(v) {
  if (document.activeElement && document.activeElement.matches("[data-env-acces]")) return; // an open list is not redrawn under the mouse
  const mp = moduleDePage(pageParente(etat.page)), global = !avecProjet(mp);
  const p = global ? phraseGlobale(mp) : phraseEtat(v);
  const boutons = [];
  if (v.projet && !global && v.projet.acces) boutons.push(blocAccesDemarrer(v.projet.acces, v.distant));
  if (v.kit_valide && v.projet && !global) {
    if (v.actif) {
      // what was changed while this pilot runs and that it has not taken yet (en_attente, from what the pilot
      // itself says it read): the working mode at its next cycle, the settings when it restarts (asked by
      // the app for the end of the cycle; cancelled, a button asks it again)
      const attente = v.en_attente || [];
      if (attente.includes("modes")) boutons.push(`<span class="btn btn-attente inerte" title="${h(t("The pilot reads the working mode when a cycle starts: the cycle under way keeps the one it started with."))}">⏳ ${t("New working mode: taken at the end of this cycle")}</span>`);
      if (attente.includes("reglages") && !v.arret_demande) boutons.push(`<button class="btn btn-attente" data-act="redemarrer-apres-cycle" title="${h(t("The pilot runs with the settings it started with. Click to restart it at the end of this cycle."))}">↻ ${t("New settings: restart after cycle")}</button>`);
      // a stop asked for the end of the cycle: the button says it, and a click withdraws it
      if (v.arret_demande) boutons.push(`<button class="btn btn-attente" data-arret="annuler" title="${h(t("Click to cancel: the pilot then goes on after this cycle."))}">⏳ ${v.arret_demande === "relance" ? t(attente.includes("reglages") ? "Restart at the end of the cycle: new settings" : "Restart requested: end of cycle") : t("Stop requested: end of cycle")}</button>`);
      else boutons.push(`<button class="btn" data-act="arreter-apres-cycle" title="${h(t("Finishes the current milestone, saves it, then stops. Nothing is lost."))}">${t("Stop after cycle")}</button>`);
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
  const maj = v.maj || {};
  if (maj.en_cours === "telechargement") boutons.unshift(`<button class="btn btn-maj" disabled>${t("Downloading the update… {0} %", [maj.avancement || 0])}</button>`);
  else if (maj.en_cours === "redemarrage") boutons.unshift(`<button class="btn btn-maj" disabled>${t("Athanor restarts…")}</button>`);
  else if (maj.disponible) boutons.unshift(`<button class="btn btn-maj" data-maj="voir" title="${h(t("Athanor {0} is available (you have {1})", [maj.derniere, v.version]))}">⬆ ${t("Update to {0}", [h(maj.derniere)])}</button>`);
  if (!cadre.actif) boutons.push(boutonCloche(v, "btn"), boutonGuide("btn"));
  rendreBarre(v);
  const nomProjet = v.projet && !global ? `<div class="muet petit">${h(v.projet.nom)} · ${h(v.projet.dossier)}</div>` : "";
  const nbActifs = (v.projets || []).filter((x) => x.actif).length;
  const html = `<div class="etat-phrase">${nomProjet}<h1 title="${h(p.titre)}"><span class="point ${p.classe}"></span><span>${h(p.titre)}</span></h1><div class="sous" title="${h(p.sous)}">${h(p.sous)}</div></div><div class="actions">${boutons.join("")}</div>`;
  // rebuilt only when it changed, keeping the focus (the bell, the "?" under the keyboard)
  $("#bandeau").classList.toggle("bandeau-global", global); // C-M218-12: on a phone its title is already in the bar above
  if ($("#bandeau").dataset.html !== html) garderFocus($("#bandeau"), () => { $("#bandeau").innerHTML = html; $("#bandeau").dataset.html = html; });
  // only what changed: a refresh with the same data sends no mutation to the menu
  const ecrire = (el, x, attr = "textContent") => { if (el[attr] !== x) el[attr] = x; };
  ecrire($("#nav-sous"), nbActifs ? t("{0} project(s) running", [nbActifs]) : t("idle"));
  ecrire($("#bm-titre"), v.projet && !global ? v.projet.nom : p.titre);
  ecrire($("#nav-kit"), v.kit || ""); ecrire($("#nav-kit"), v.kit || "", "title");
  const nv = $("#nav-version");
  ecrire(nv, v.version ? "v" + v.version : "");
  nv.classList.toggle("maj-dispo", !!maj.disponible);
  ecrire(nv, maj.disponible ? t("Athanor {0} is available (you have {1})", [maj.derniere, v.version]) : t("Version of Athanor: click to look for an update"), "title");
  const sel = $("#sel-projet");
  sel.setAttribute("aria-expanded", etat.selecteurOuvert ? "true" : "false");
  sel.innerHTML = v.projet ? `<span class="dot ${classePhase(v.projet)}"></span><span class="nom">${h(v.projet.nom)}</span><span class="chevron">▾</span>` : `<span class="nom muet">${t("Choose a project")}</span><span class="chevron">▾</span>`;
  marquerNav(v);
  ecrire($("#btn-verrou"), !v.mot_de_passe, "hidden");
  if (v.projet && etat.projet !== v.projet.nom) etat.projet = v.projet.nom;
  rendreSelecteur(v);
  rendreNotifs(v);
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
    <div class="menu-liste">${liste.map((x) => `<a href="#suivi" data-projet="${h(x.nom)}" class="${x.courant ? "actif" : ""}"><span class="dot ${classePhase(x)}"></span><span class="nom">${h(x.nom)}</span><span class="muet petit">${x.actif ? t(x.phase === "quota" || x.phase === "budget" || x.phase === "attente_vous" ? "waiting" : "running") : t("stopped")}${x.attente ? " · " + t("{0} queued", [x.attente]) : ""}${x.pour_vous ? " · " + t("{0} for you", [x.pour_vous]) : ""}</span></a>`).join("") || `<div class="vide">${t("No project.")}</div>`}</div>
    ${v.projet ? `<a href="#projet" class="menu-nouveau menu-gerer">⚙ ${t("Manage this project")}</a>` : ""}
    <a href="#nouveau" class="menu-nouveau">+ ${t("New project")}</a>
    <a href="#rejoindre" class="menu-nouveau">⇄ ${t("Join a project")}</a>
    ${archives ? `<a href="#archives" class="menu-nouveau menu-archives">🗄 ${t("Archived projects ({0})", [archives])}</a>` : ""}`;
  const r = $("#sel-recherche"); if (r) { r.focus(); r.setSelectionRange(pos, pos); }
}

// Stop now cuts the cycle: asked first, with Stop after cycle offered instead. A stop asked for the end of
// the cycle shows on its button at once (then from the state), and can be withdrawn from it.
async function arreterPilote(b) {
  if ((etat.vue || {}).arret_demande) {
    const c = await choisir(t("The current cycle is cut off: what Claude has not saved yet is lost, and will be done again at the next start.\n\nA stop at the end of this cycle is already asked: waiting for it loses nothing."), [
      { id: "", texte: t("Wait for the end of the cycle"), primaire: true }, { id: "maintenant", texte: t("Stop now"), danger: true }],
      { titre: t("Stop the pilot now?"), large: true });
    if (c === "maintenant") { b.disabled = true; await action(api("pilote/arreter", "POST")); setTimeout(rafraichir, 800); }
    return;
  }
  const c = await choisir(t("The current cycle is cut off: what Claude has not saved yet is lost, and will be done again at the next start.\n\nTo lose nothing, choose “Stop after cycle”: Claude finishes the current milestone, saves it, then the pilot stops by itself."), [
    { id: "", texte: t("Cancel") }, { id: "apres", texte: t("Stop after cycle"), primaire: true }, { id: "maintenant", texte: t("Stop now"), danger: true }],
    { titre: t("Stop the pilot now?"), large: true });
  if (c === "maintenant") { b.disabled = true; await action(api("pilote/arreter", "POST")); setTimeout(rafraichir, 800); }
  if (c === "apres") return arreterApresCycle(b.parentElement.querySelector('[data-act="arreter-apres-cycle"]'));
}
async function arreterApresCycle(b) {
  if (b) { b.disabled = true; b.textContent = "⏳ " + t("Stop requested: end of cycle"); }
  await action(api("pilote/arreter-apres-cycle", "POST"));
  await rafraichir();
}
async function annulerArret(b) {
  const c = await choisir(t("The pilot will stop at the end of this cycle. Cancel this stop and let it go on?"), [
    { id: "", texte: t("Keep the stop") }, { id: "annuler", texte: t("Let it go on"), primaire: true }]);
  if (c !== "annuler") return;
  b.disabled = true;
  await action(api("pilote/annuler-arret", "POST"));
  await rafraichir();
}

$("#bandeau").addEventListener("change", async (ev) => {
  const sel = ev.target.closest("[data-env-acces]"); if (!sel || !etat.vue || !etat.vue.projet) return;
  sel.blur();
  let corps = { projet: etat.vue.projet.nom, action: "env", env: sel.value };
  if ((etat.vue.projet.acces?.preuve || []).includes(sel.value)) { // RT2: that environment's keys go to the pilot
    const p = await preuveMdp(t("Moving to {0} gives the pilot this environment's keys: retype Athanor's password.", [sel.value]), { ok: t("Change") });
    if (!p) return rafraichir();
    corps = { ...corps, ...p };
  }
  await action(api("acces", "POST", corps)); // the server rewrites secrets.env
  await rafraichir();
  if (etat.page === "acces") rendrePage();
});
$("#bandeau").addEventListener("click", async (ev) => {
  const b = ev.target.closest("button"); if (!b) return;
  if (b.dataset.act === "demarrer") return demarrerPilote(b);
  if (b.dataset.act === "arreter") return arreterPilote(b);
  if (b.dataset.act === "arreter-apres-cycle") return arreterApresCycle(b);
  if (b.dataset.arret === "annuler") return annulerArret(b);
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
  await allerProjet(a.dataset.projet, avecProjet(moduleDePage(etat.page)) ? etat.page : "suivi"); // outside a project module: its first page
});
document.addEventListener("click", (ev) => { if (etat.selecteurOuvert && !ev.target.closest("#sel-projet, #menu-projets")) fermerSelecteur(); });

// the sidebar's "Remote": how many devices are connected now (a device sees its own notifications there, never the pairing)
function marquerDistance(v) {
  const a = document.querySelector('#onglets-module a[data-page="distance"]'); if (!a) return;
  const n = v.distance_active ? v.connectes || 0 : 0;
  a.dataset.connectes = n ? String(n) : ""; a.title = v.distance_active ? t("Remote access on: {0} device(s) connected", [n]) : "";
  a.classList.toggle("distance-active", !!v.distance_active);
}
// phones: the sidebar is a drawer (☰), closed again by a choice or a tap beside it
// open: the page under the veil is inert (the keyboard stays in the drawer), Escape closes it back to ☰
// closed on a phone, the drawer is inert too: the keyboard never enters it off-screen
const TEL = matchMedia("(max-width: 700px)"); // the one phone threshold (the CSS has the same)
function menuMobile(ouvert) { document.body.classList.toggle("menu-ouvert", ouvert); $("#btn-menu").setAttribute("aria-expanded", String(ouvert)); $("main.principal").inert = ouvert; $("#nav-tiroir").inert = TEL.matches && !ouvert; }
menuMobile(false);
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && document.body.classList.contains("menu-ouvert")) { menuMobile(false); $("#btn-menu").focus(); } });
TEL.addEventListener("change", () => menuMobile(false)); // a drawer left open never freezes the desktop page
$("#btn-menu").addEventListener("click", () => menuMobile(!document.body.classList.contains("menu-ouvert")));
$("#voile-menu").addEventListener("click", () => menuMobile(false));
$("#nav-tiroir").addEventListener("click", (ev) => { if (ev.target.closest("nav a")) menuMobile(false); });

// The one-level menu (Home, Projects, one entry per module with pages, Settings at the bottom) and the
// tabs of the module shown: both from /api/hub, never a list of pages here.
// New project and the archives open from the Projects page: its entry stays lit there
// pageParente: the page whose menu entry stays lit for this one (PageModule.Fils), else itself
const pageParente = (page) => { for (const m of etat.hubModules || []) for (const p of m.pages || []) if ((p.fils || []).includes("#" + page)) return p.hash.slice(1); return page; };
etat.pagesModules = (() => { try { return JSON.parse(localStorage.getItem("athanor.pagesModules") || "{}"); } catch (e) { return {}; } })();
function moduleDePage(page) {
  const ms = etat.hubModules || [];
  return ms.find((m) => (m.pages || []).some((p) => p.hash === "#" + page)) || null;
}
// a module's entry opens the page last seen in it, else its first page
const entreeModule = (m) => (m.pages.some((p) => p.hash === etat.pagesModules[m.id]) ? etat.pagesModules[m.id] : m.pages[0].hash);
function rendreMenu(modules) {
  etat.hubModules = modules;
  const lien = (cible, attrs, icone, libelle) => `<a data-href="${h(cible)}" tabindex="0" ${attrs}><span class="ico ico-${h(icone)}" aria-hidden="true"></span><span class="nav-lib">${h(t(libelle))}</span><span class="nb"></span></a>`;
  let haut = "", bas = "";
  for (const m of modules) {
    if (!(m.pages || []).length) continue;
    // the hub's pages of order 90 and up (the audit log) sit at the bottom, above Settings
    if (m.id === "hub") for (const p of m.pages) { const l = lien(p.hash, `data-module="hub" data-page="${h(p.hash.slice(1))}"`, p.icone, p.libelle); if (p.ordre >= 90) bas += l; else haut += l; }
    else if (m.id === "reglages") bas += lien(entreeModule(m), `data-module="${h(m.id)}"`, m.id, m.nom);
    else haut += lien(entreeModule(m), `data-module="${h(m.id)}"`, m.id, m.nom);
  }
  if (haut + bas !== etat.menuHtml) { etat.menuHtml = haut + bas; garderFocus($("#nav-tiroir"), () => { $("#nav-menu").innerHTML = haut; $("#nav-bas").innerHTML = bas; }); }
  marquerMenu();
  if (etat.vue) rendreBandeau(etat.vue); // the header knows the page's module now
}
// the entry of the page shown lit (aria-current), and its module's tabs
function marquerMenu() {
  const page = pageParente(etat.page), m = moduleDePage(page);
  document.querySelectorAll("#nav-tiroir nav a").forEach((a) => {
    const actif = a.dataset.page ? a.dataset.page === page : !!m && a.dataset.module === m.id;
    a.classList.toggle("actif", actif);
    if (actif !== a.hasAttribute("aria-current")) { if (actif) a.setAttribute("aria-current", "page"); else a.removeAttribute("aria-current"); }
  });
  rendreOnglets(m);
  if (etat.vue) marquerNav(etat.vue);
}
// one line of tabs at the top of the module's area (Dev: with its project selector); none for one page
function rendreOnglets(m) {
  const dev = avecProjet(m), pages = m && m.id !== "hub" ? m.pages : [];
  const html = pages.length > 1 || dev ? pages.map((p) => `<a data-href="${h(p.hash)}" tabindex="0" data-page="${h(p.hash.slice(1))}"${p.hash === "#" + etat.page ? ' class="actif" aria-current="page"' : ""}>${h(t(p.libelle))}<span class="nb"></span></a>`).join("") : "";
  $("#selecteur").hidden = !dev;
  $("#barre-onglets").hidden = !html;
  const nav = $("#onglets-module");
  if (nav.dataset.html === html + etat.langue) return;
  nav.dataset.html = html + etat.langue; nav.innerHTML = html;
  if (m) nav.setAttribute("aria-label", t(m.nom));
  // the active tab centred in the line (phones scroll it sideways), once laid out
  requestAnimationFrame(() => { const a = nav.querySelector(".actif"); if (!a) return; const x = a.getBoundingClientRect(), n = nav.getBoundingClientRect(); nav.scrollLeft += x.left - n.left - (n.width - x.width) / 2; });
}
// the counters on the menu and the tabs: Requests queued, For you, the projects that need the user, Remote
function marquerNav(v) {
  const nb = (sel, n, alerte, titre) => {
    const b = document.querySelector(sel + " .nb"); if (!b) return;
    const c = "nb" + (n ? " pastille" + (alerte ? " pastille-alerte" : "") : ""), txt = String(n || ""), ti = (n && titre) || "";
    if (b.className !== c) b.className = c; // only what changed: an unchanged menu sends no mutation
    if (b.textContent !== txt) b.textContent = txt;
    if (b.title !== ti) b.title = ti;
  };
  const besoins = etat.hub ? alertesHub().length : projetsVisibles(v).filter(besoinProjet).length; // every module, as the bell counts
  nb('#nav-menu a[data-page="global"]', besoins, true, t("{0} alert(s) need you", [besoins]));
  nb('#onglets-module a[data-page="demandes"]', v.attente);
  nb('#onglets-module a[data-page="pourvous"]', v.pour_vous, true);
  marquerDistance(v);
}
// the menu keeps the HTML's entries if it fails (locked: loaded again once unlocked)
// C3: one /api/hub per load or refresh tick: the menu and the pages reuse the answer the tick read less than a second ago
const lireHub = () => (etat.hub && Date.now() - (etat.hubLu || 0) < 1000 ? Promise.resolve(etat.hub) : api("hub"));
const chargerMenu = () => lireHub().then((d) => rendreMenu(d.modules || [])).catch(() => {});
function traduireNav() {
  if (etat.hubModules) rendreMenu(etat.hubModules);
  else document.querySelectorAll("#nav-tiroir [data-libelle]").forEach((x) => { x.textContent = t(x.dataset.libelle); });
  $("#nav-bas").setAttribute("aria-label", t("Settings"));
  $("#btn-verrou").textContent = "🔒 " + t("Lock");
  $("#btn-urgence").textContent = "⛔ " + t("Emergency stop");
  $("#verrou-titre").textContent = t("Application locked");
  $("#verrou-mdp").placeholder = t("Password");
  $("#verrou-bouton").textContent = t("Unlock");
}

function naviguer() {
  let page = (location.hash || "#accueil").slice(1);
  // "#page@block": the page, then that block of it shown (the Guide's buttons: Settings › Options › GitHub…)
  let ancre = "";
  if (page.includes("@")) { [page, ancre] = page.split("@", 2); history.replaceState(null, "", "#" + page); }
  if (ancre === "bloc-pcs") etat.pcsOuvert = true; // a folded block the link points at: shown open
  if (ancre) montrerAncre(ancre);
  // the application's options are a tab of the settings now (older links, and the texts that say "Options")
  if (page === "options") { etat.ongletReglages = "options"; history.replaceState(null, "", "#reglages"); page = "reglages"; }
  // joining a project: the second tab of the new project page
  if (page === "rejoindre") { etat.npRejoindre = true; history.replaceState(null, "", "#nouveau"); page = "nouveau"; } else if (page === "nouveau" && etat.page !== "nouveau") etat.npRejoindre = false;
  // the settings always open on Options (or on the tab asked for, the guided tour)
  if (page === "reglages" && etat.page !== "reglages") { etat.ongletReglages = etat.ongletDemande || "options"; etat.ongletDemande = null; }
  if (page.startsWith("missions/")) { etat.missionId = decodeURIComponent(page.slice(9)); page = "mission"; } // one mission, its id in the hash
  if (page.startsWith("serveur/")) { srv.id = decodeURIComponent(page.slice(8)); page = "serveur"; } // one server's sheet, its id in the hash
  if (!pages[page]) { page = "accueil"; history.replaceState(null, "", "#accueil"); } // an unknown hash: Home
  etat.page = page;
  // T-M218-1: a tour stays on its own pages; another page opened from the menu ends it
  if (etat.visite >= 0 && (etapesVisite()[etat.visite] || {}).page !== page) finirVisite();
  // the page remembered for its module's entry
  const m = moduleDePage(page);
  if (m && m.id !== "hub" && etat.pagesModules[m.id] !== "#" + page) {
    etat.pagesModules[m.id] = "#" + page;
    try { localStorage.setItem("athanor.pagesModules", JSON.stringify(etat.pagesModules)); } catch (e) { /* this device only */ }
    const e = document.querySelector(`#nav-tiroir nav a[data-module="${CSS.escape(m.id)}"]`); if (e) e.dataset.href = "#" + page;
  }
  marquerMenu();
  if (etat.vue) rendreBandeau(etat.vue); // the All projects page has its own header
  rendrePage();
}
window.addEventListener("hashchange", naviguer);
// montrerAncre: once the page has it (some blocks come a moment later), the block scrolled to and lit up
function montrerAncre(id, essais = 40) {
  const x = document.getElementById(id);
  if (!x) { if (essais > 0) setTimeout(() => montrerAncre(id, essais - 1), 150); return; }
  const repli = x.querySelector(":scope > details"); // a folded block (Overview › Other computers): opened
  if (repli && !repli.open) repli.open = true;
  x.scrollIntoView({ behavior: "smooth", block: "center" });
  if (x.matches("input, textarea")) x.focus({ preventScroll: true }); // a field the link points at: ready to type in
  x.classList.remove("surligne"); void x.offsetWidth; x.classList.add("surligne");
  setTimeout(() => x.classList.remove("surligne"), 2600);
}

// ----------------------------------------------------------------------------- pages
const pages = {};

// --- first launch: a four-step wizard (language, a shortcut, appearance, what comes next) in an overlay
const ETAPES_ASSISTANT = 4;
function rendreAssistant() {
  const el = $("#assistant"); const n = etat.assistant;
  el.hidden = n < 0; if (n < 0) return;
  const barre = Array.from({ length: ETAPES_ASSISTANT }, (_, i) => `<i class="${i <= n ? "faite" : ""}"></i>`).join("");
  let corps = "", titre = "";
  if (n === 0) { titre = t("Welcome"); corps = `<p class="muet">${t("Athanor builds whole projects with Claude Code while you do something else. First, pick your language.")}</p>${rendreLangues()}`; }
  if (n === 1) { // the shortcut to the installed Athanor, offered at once (skipping it: Continue)
    const r = raccourciConnu();
    titre = t("A shortcut to Athanor");
    corps = `<p class="muet">${t("Athanor is installed in its own folder and updates itself there. A shortcut opens it directly: choose where to put it, or skip this step (Settings → Options → Shortcut, later).")}</p>
      <div class="assistant-raccourci">${r ? blocRaccourci(r, false, "", "rc-dossier-assistant") : attenteRaccourci()}</div>`;
  }
  if (n === 2) { titre = t("Make it yours"); corps = `<p class="muet">${t("Pick a palette and an accent color. You can change them any time in Options.")}</p>${rendreApparence()}`; }
  if (n === 3) { titre = t("What comes next"); corps = `<p class="muet">${t("Five steps, all from this window. Then you can let it run for days, on several projects at once.")}</p>
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
  const b = ev.target.closest("button"); if (!b || b.dataset.raccourci || b.dataset.parcourir) return; // the document's handlers
  if (b.dataset.langue) { await changerLangue(b.dataset.langue); return rendreAssistant(); }
  if (b.dataset.theme) { await changerApparence(b.dataset.theme, etat.accent); return rendreAssistant(); }
  if (b.dataset.accent) { await changerApparence(etat.theme, b.dataset.accent); return rendreAssistant(); }
  if (b.dataset.assistant === "retour") { etat.assistant--; return rendreAssistant(); }
  if (b.dataset.assistant === "suite") {
    if (etat.assistant === 1 && !((etat.raccourci || {}).raccourcis || []).length) etat.raccourciPlusTard = true; // skipped: not offered again this time
    etat.assistant++; return rendreAssistant();
  }
  if (b.dataset.assistant === "fin") { // then the Home, with its tour (proposerTutoriel): never a third help on top
    await action(api("preferences", "POST", { bienvenue_vue: true })); etat.assistant = -1; if (etat.vue) etat.vue.bienvenue = false; rendreAssistant();
    if (location.hash !== "#accueil") location.hash = "#accueil"; else await rendrePage();
  }
});

// --- the tours: each module declares its own (Module.Tutoriel in /api/hub), shown once at the first visit of
// one of its pages (its "seen" state: config tours, /api/preferences), replayed from the Guide (the "?").
// A bubble over the place explained; the veil dims without blocking anything; Skip or Escape at any time.
function lancerTutoriel(m) {
  const premiere = ((m.pages || [])[0] || {}).hash || "#accueil";
  etat.etapesVisite = (m.tutoriel || []).map((e) => ({ page: (e.page || premiere).slice(1), cibles: e.cible ? [e.cible] : [], titre: e.titre, texte: e.texte }));
  demarrerVisite();
}
// after each render: the tour of the page's module, if never shown here and nothing else is open
function proposerTutoriel() {
  // a project's Access panel is explained by the vault's tour (keys, scopes, grants, badge)
  const v = etat.vue, pg = ((moduleDePage(etat.page) || {}).pages || []).find((p) => p.hash === "#" + etat.page); // a page may borrow another module's tour
  const m = pg && pg.tour ? (etat.hubModules || []).find((x) => x.id === pg.tour) : moduleDePage(etat.page);
  if (!v || v.verrouille || v.bienvenue || etat.assistant >= 0 || etat.visite >= 0 || !m || !(m.tutoriel || []).length) return;
  etat.toursVus ||= new Set(); // a paired device may not write the preference: shown once per session there
  const vu = (id) => (v.tours || {})[id] || etat.toursVus.has(id);
  // M200: the Home's tour seen before the conversation existed: the conversation's own steps, once
  if (m.id === "hub" && etat.page === "accueil" && vu("hub") && !vu(TOUR_CHAT)) {
    etat.toursVus.add(TOUR_CHAT); (v.tours ||= {})[TOUR_CHAT] = true;
    api("preferences", "POST", { tours: { [TOUR_CHAT]: true } }).catch(() => {});
    return lancerTourChat();
  }
  if (vu(m.id)) return;
  const connu = !(m.id in (v.tours || {})) && (m.id === "dev" ? (v.projets || []).some((p) => p.faits > 0) : (m.resume || {}).etat === "ok"); // a module in use (Dev: a milestone done; the vault: a key stored)
  const ids = m.id === "hub" ? [m.id, TOUR_CHAT] : [m.id]; // the Home's tour holds the conversation's steps
  ids.forEach((id) => { etat.toursVus.add(id); (v.tours ||= {})[id] = true; });
  api("preferences", "POST", { tours: Object.fromEntries(ids.map((id) => [id, true])) }).catch(() => {});
  // someone who already uses the module (never this tour, never reset): a reminder, not the tour
  if (connu) return toast(t("New: the tutorial is in the “?”."));
  lancerTutoriel(m);
}
const TOUR_CHAT = "hub-chat";
// the home conversation's tour: the steps of the Home's tour that point into the conversation (hub_modules.go)
function lancerTourChat() {
  const m = (etat.hubModules || []).find((x) => x.id === "hub"); if (!m) return;
  lancerTutoriel({ ...m, tutoriel: (m.tutoriel || []).filter((e) => /^#(accueil-form|hub-)/.test(e.cible || "")) });
}
function etapesVisite() { return etat.etapesVisite || []; }
function demarrerVisite() { const a = document.activeElement; etat.focusVisite = a && a !== document.body ? cleFocus(a) : ""; etat.visite = 0; montrerVisite(); }
function finirVisite() {
  // the focus goes back to what had it before the tour, else to the “?”, never to the top of the page
  const perdu = $("#visite").contains(document.activeElement) || document.activeElement === document.body;
  etat.visite = -1; $("#visite").hidden = true; $("#visite-voile").hidden = true; etat.visiteCible = null;
  document.querySelectorAll(".visite-cible").forEach((x) => x.classList.remove("visite-cible"));
  const retour = perdu && ((etat.focusVisite && document.querySelector(etat.focusVisite)) || document.querySelector(".btn-guide"));
  if (retour) retour.focus({ preventScroll: true });
}
// the veil: everything dimmed but the place explained (a hole the size of the target, or none at all)
function cibleVisite() {
  // the page may have been rebuilt since (the header is, every refresh): the same place, found again
  if (etat.visiteCible && etat.visiteCible.isConnected) return etat.visiteCible;
  for (const c of etat.visiteSelecteurs || []) { const x = document.querySelector(c); if (x && x.offsetParent !== null) { etat.visiteCible = x; x.classList.add("visite-cible"); return x; } }
  return null;
}
function placerVoile() {
  const voile = $("#visite-voile"), c = cibleVisite();
  if (etat.visite < 0) { voile.hidden = true; return; }
  voile.hidden = false;
  if (!c || !c.isConnected || c.offsetParent === null) { voile.classList.add("sans-cible"); Object.assign(voile.style, { top: "50%", left: "50%", width: "0px", height: "0px" }); return; }
  voile.classList.remove("sans-cible");
  const z = c.getBoundingClientRect(), m = 6;
  Object.assign(voile.style, { top: z.top - m + "px", left: z.left - m + "px", width: z.width + 2 * m + "px", height: z.height + 2 * m + "px" });
}
window.addEventListener("resize", () => { if (etat.visite >= 0) { placerVoile(); placerBulle(); } });
document.addEventListener("scroll", () => { if (etat.visite >= 0) { placerVoile(); placerBulle(); } }, true);
function placerBulle() {
  const bulle = $("#visite"), cible = cibleVisite();
  if (cible && cible.isConnected && cible.offsetParent !== null) {
    const z = cible.getBoundingClientRect(), l = bulle.offsetWidth, hb = bulle.offsetHeight;
    let haut = z.bottom + 14; if (haut + hb > window.innerHeight - 8) haut = Math.max(8, z.top - hb - 14);
    bulle.style.top = haut + "px";
    bulle.style.left = Math.max(8, Math.min(z.left, window.innerWidth - l - 8)) + "px";
  } else if ((etat.visiteSelecteurs || []).length) { // its target gone (a form opened over it): aside, bottom right, never over the form
    bulle.style.top = Math.max(8, window.innerHeight - bulle.offsetHeight - 16) + "px"; bulle.style.left = Math.max(8, window.innerWidth - bulle.offsetWidth - 16) + "px";
  } else { bulle.style.top = Math.max(8, (window.innerHeight - bulle.offsetHeight) / 2) + "px"; bulle.style.left = Math.max(8, (window.innerWidth - bulle.offsetWidth) / 2) + "px"; }
}
async function montrerVisite() {
  const etapes = etapesVisite(), n = etat.visite;
  if (n < 0 || n >= etapes.length) return finirVisite();
  const e = etapes[n];
  if (location.hash !== "#" + e.page) location.hash = "#" + e.page; else await rendrePage();
  let cible = null;
  for (let i = 0; i < 40 && !cible; i++) { // the page renders asynchronously; once rendered without the target, the bubble is centred
    await new Promise((r) => setTimeout(r, 100));
    for (const c of e.cibles) { const x = document.querySelector(c); if (x && x.offsetParent !== null) { cible = x; break; } }
    if (!cible && i > 0 && etat.pageRendue === e.page) break;
  }
  if (n !== etat.visite) return;
  document.querySelectorAll(".visite-cible").forEach((x) => x.classList.remove("visite-cible"));
  const bulle = $("#visite");
  bulle.innerHTML = `<div class="visite-titre" id="visite-titre"></div><p></p>
    <div class="visite-pied"><span class="muet petit">${t("Step {0} of {1}", [n + 1, etapes.length])}</span><span class="espace"></span>
      <button class="btn btn-petit" data-visite="passer">${t("Skip the tutorial")}</button>
      ${n > 0 ? `<button class="btn btn-petit" data-visite="retour">${t("Back")}</button>` : ""}
      <button class="btn btn-petit btn-primaire" data-visite="suite">${n === etapes.length - 1 ? t("Finish") : t("Next")}</button></div>`;
  bulle.querySelector(".visite-titre").textContent = t(e.titre); bulle.querySelector("p").textContent = t(e.texte); // module texts: never HTML
  bulle.hidden = false;
  etat.visiteCible = cible; etat.visiteSelecteurs = e.cibles;
  if (cible) {
    cible.classList.add("visite-cible");
    cible.scrollIntoView({ block: "center", behavior: "smooth" });
    await new Promise((r) => setTimeout(r, 350));
  }
  placerVoile();
  placerBulle();
  bulle.querySelector('[data-visite="suite"]').focus();
}
$("#visite").addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-visite]"); if (!b) return;
  if (b.dataset.visite === "passer") return finirVisite();
  etat.visite += b.dataset.visite === "retour" ? -1 : 1;
  montrerVisite();
});
document.addEventListener("keydown", (ev) => { if (ev.key === "Escape" && etat.visite >= 0 && !$("#visite").hidden) finirVisite(); });

// --- working mode of the project: the parallel, eco and fast modes (independent switches), the framing time
// box and the work time limit. The same block serves Manage and New project (there on etat.npMode, sent once
// the project exists).
async function rendreMode() {
  const m = await api("mode").catch(() => null);
  return m ? blocMode(m) : "";
}
// the "i" of a mode: what it does (the line beside its switch stays short)
const NOMS_MODES = { PARALLEL: "Parallel mode", ECO: "Eco mode", FAST: "Fast mode" };
const DETAILS_MODES = {
  PARALLEL: ["The lead writes a fine-grained plan with dependencies. Workers build independent milestones at the same time, each in its own git working copy; the pilot merges each one as soon as it finishes and starts the next.", "Much faster on a project that splits well; it uses more quota (one session per worker). It starts with 2 workers and adds one each time a worker delivers, up to the maximum you choose.", "With the eco mode, every worker reads and prints less. With the fast mode, only quick checks follow the merges. Applies from the next cycle."],
  ECO: ["Every lever but the model and the effort is set for the quota: the lead does the work itself instead of handing it to sub-agents (they use most of the quota), at most 4 images per cycle, shorter command outputs.", "The critic and the tester run once, at the delivery. Slower to polish, much lighter on the quota.", "With the parallel mode, the workers follow the same rules. Applies from the next cycle."],
  FAST: ["Speed first: at each milestone, Claude runs only the quick check on what it just built, then moves on. No full check, no tester and no critic on the way.", "While the project is being built, it may be buggy, unpolished or badly finished: the defects seen on the way are noted, and fixed at once only when they block what comes next.", "At the end of the project, everything is verified again and fixed: the full check, the tester and the critic on the whole project, then every defect found, until nothing is left. The delivery is as finished as without this mode.", "The last milestone is therefore longer: it carries the verification of the whole project. The pilot refuses a delivery made without it.", "Switched off on the way: what was built in the fast mode is verified as a whole first. Applies from the next cycle."],
};
const boutonMode = (cle) => `<button type="button" class="chat-info" data-detail-mode="${cle}" title="${h(t("What this mode does"))}" aria-label="${h(t("What this mode does"))}">i</button>`;
function detailMode(cle) {
  return choisir((DETAILS_MODES[cle] || []).map((l) => "• " + t(l)).join("\n"), [{ id: "ok", texte: t("OK"), primaire: true }], { titre: t(NOMS_MODES[cle] || cle) });
}
function blocMode(m) {
  etat.modeMax = m.parallele_max;
  const ligne = (id, cle, actif, court) => `<div class="ligne m0 mode-suite">
    <label class="interrupteur m0"><input type="checkbox" id="${id}" ${actif ? "checked" : ""}><span><b>${t(NOMS_MODES[cle])}</b></span></label>${boutonMode(cle)}
    <span class="muet petit">${court}</span>`;
  return `<div class="bloc mode-ligne"><div class="mode-grille">
    <h2 class="m0">${t("Working mode")}</h2>
    ${ligne("md-parallele", "PARALLEL", m.parallele, m.parallele ? t("several workers at once: faster, uses more quota") : t("one milestone at a time; parallel: several workers at once, faster, more quota"))}
    ${m.parallele ? `<span class="espace"></span><span class="petit" title="${h(t("Starts with 2, adds one each time a worker delivers, and starts no more once the quota is met. Applies from the next cycle."))}">${t("Workers at most")}</span><div class="choix m0">${[2, 3, 4, 6, 8, 10].map((n) => `<button type="button" data-max="${n}" class="${m.parallele_max === n ? "actif" : ""}">${n}</button>`).join("")}</div>` : ""}
    </div>
    ${ligne("md-eco", "ECO", m.eco, m.eco ? t("the quota first: the lead works alone, one review at delivery, few images, short outputs") : t("spends as little quota as possible"))}
    </div>
    ${m.eco ? `<p class="muet petit m0 mode-suite">${t("To save even more: a smaller model for the lead (Sonnet) and a lower thinking effort (medium or low), in Settings.")}</p>` : ""}
    ${ligne("md-rapide", "FAST", m.rapide, m.rapide ? t("speed first: quick checks at each milestone, everything verified and fixed at the end") : t("builds as fast as possible; everything is verified and fixed at the end"))}
    </div>
    ${m.rapide ? `<p class="attention petit m0 mode-suite">${t("Until the end, the project may be buggy or badly finished. The last milestone is longer: it verifies and fixes the whole project.")}</p>` : ""}
    <p class="muet petit m0 mode-suite">${t("The three modes add up: switch on the ones you want.")}</p>
    <div class="ligne m0 mode-suite">
    <span class="petit"><b>${t("Longest framing")}</b>${boutonDetail("FRAMING_MAX_MIN")}</span>
    <span class="muet petit">${t("the first cycle, which writes the plan")}</span>
    <span class="espace"></span><div class="choix m0">${[0, 25, 45, 60, 90].map((n) => `<button type="button" data-cadrage="${n}" class="${(m.cadrage_max || 0) === n ? "actif" : ""}">${n ? t("{0} min", [n]) : m.cadrage_global ? t("Like Settings ({0} min)", [m.cadrage_global]) : t("Like Settings (no limit)")}</button>`).join("")}</div>
    <input type="number" id="md-cadrage" class="cadrage-autre" min="10" max="240" inputmode="numeric" value="${m.cadrage_max && ![25, 45, 60, 90].includes(m.cadrage_max) ? m.cadrage_max : ""}" placeholder="${h(t("Other (min)"))}" aria-label="${h(t("Other (min)"))}" title="${h(t("Other (min)"))}">
    </div>
    <div class="ligne m0 mode-suite">
    <span class="petit"><b>${t("Work time limit")}</b>${boutonDetail("MAX_HOURS")}</span>
    <span class="muet petit">${t("only the time the pilot works counts")}</span>
    <span class="espace"></span><div class="choix m0">${[-1, 72, 168, 336, 720, 0].map((n) => `<button type="button" data-duree-max="${n}" class="${(m.duree_max ?? -1) === n ? "actif" : ""}">${n >= 0 ? libelleDuree(n) : m.duree_max_global ? t("Like Settings ({0} h)", [m.duree_max_global]) : t("Like Settings (unlimited)")}</button>`).join("")}</div>
    <input type="number" id="md-duree-max" class="cadrage-autre" min="1" max="87600" inputmode="numeric" value="${m.duree_max > 0 && ![72, 168, 336, 720].includes(m.duree_max) ? m.duree_max : ""}" placeholder="${h(t("Other (h)"))}" aria-label="${h(t("Other (h)"))}" title="${h(t("Other (h)"))}">
    </div>
  </div></div>`;
}
// changerMode: the block as it is on the page, with one change. changement: { parallele, parallele_max, eco,
// rapide, cadrage_max (0: back to Settings), duree_max (0: unlimited, -1: back to Settings) }; what is left
// out stays as it is. On New project nothing is sent yet: the choice waits in etat.npMode for the creation.
async function changerMode(changement) {
  const corps = { parallele: $("#md-parallele").checked, parallele_max: maxModeActif(), eco: $("#md-eco").checked, rapide: $("#md-rapide").checked, ...changement };
  if (etat.page === "nouveau") {
    Object.assign(etat.npMode, corps);
    const zone = $("#np-mode"); if (zone) zone.innerHTML = blocMode(etat.npMode);
    return;
  }
  const r = await action(api("mode", "POST", corps));
  if (r) rendrePage();
}
function maxModeActif() { const actif = $("#page .mode-ligne .choix button.actif[data-max]"); return actif ? Number(actif.dataset.max) : etat.modeMax || 3; }

// --- chat with Claude about the project (api_chat.go): by default Claude reads and answers, a change becomes
// a note for the pilot; with "change the files" ticked, Claude changes them (Athanor commits them), after the
// pilot's cycle when it runs (it pauses, then starts again)
const chat = { modele: (() => { try { return localStorage.getItem("chatModele") ?? "sonnet"; } catch (e) { return "sonnet"; } })(), modif: false, piloteActif: false, signature: "", modeles: null, envoi: false,
  // Internet: Claude may search and read the web, only when the message needs it (remembered on this device)
  internet: (() => { try { return localStorage.getItem("chatInternet") === "1"; } catch (e) { return false; } })() };

function mdChat(s) {
  return String(s || "").split(/```/).map((bloc, i) => {
    if (i % 2) return `<pre class="chat-code">${h(bloc.replace(/^[a-zA-Z0-9_+-]*\n/, ""))}</pre>`;
    return h(bloc).replace(/`([^`\n]+)`/g, "<code>$1</code>").replace(/\*\*([^*\n]+)\*\*/g, "<b>$1</b>")
      .replace(/^(\s*)[-*] (.*)$/gm, "$1• $2").replace(/^#{1,4} (.*)$/gm, "<b>$1</b>")
      .replace(/(^|[^\w*])\*(\S(?:[^*\n]*\S)?)\*(?![\w*])/gm, "$1<i>$2</i>").replace(/\n/g, "<br>"); // M195: *italic*, never a*b*c
  }).join("");
}

function messageChat(m, i) {
  const quand = m.quand ? new Date(m.quand).toLocaleTimeString(document.documentElement.lang || "en", { hour: "2-digit", minute: "2-digit" }) : "";
  const pieces = (m.pieces || []).length ? `<div class="petit muet">${m.pieces.map((x) => "📎 " + h(x.split("/").pop())).join("<br>")}</div>` : "";
  if (m.role === "vous") return `<div class="chat-msg chat-vous"><div class="chat-bulle">${mdChat(m.texte)}${pieces}</div><div class="chat-meta">${h(quand)}${m.mode === "modif" ? " · " + (m.pause ? t("changes the files after the pilot's cycle") : t("may change the files")) : ""}${m.internet ? " · 🌐 " + t("Internet") : ""}</div></div>`;
  const relance = m.pilote ? `<div class="chat-meta">${m.pilote === "relance" ? `<span class="ok">▶ ${t("The pilot started again.")}</span>` : `<span class="attention">${t("The pilot did not start again: {0}", [h(t(m.pilote))])}</span>`}</div>` : "";
  if (m.role === "erreur") return `<div class="chat-msg chat-claude"><div class="chat-bulle chat-erreur">${h(t(m.texte))}</div>${relance}</div>`;
  const note = (m.texte.match(/<note>([\s\S]*?)<\/note>/) || [])[1];
  const agent = (m.texte.match(/<agent>([\s\S]*?)<\/agent>/) || [])[1];
  const mcp = (m.texte.match(/<mcp>([\s\S]*?)<\/mcp>/) || [])[1];
  const texte = m.texte.replace(/<(note|agent|mcp)>[\s\S]*?<\/\1>/g, "").trim();
  const actions = (m.outils || []).length ? `<details class="chat-actions"><summary>${t("{0} action(s)", [m.outils.length])}</summary>${m.outils.map((o) => `<div>${h(t(o))}</div>`).join("")}</details>` : "";
  return `<div class="chat-msg chat-claude"><div class="chat-bulle">${mdChat(texte)}
    ${note ? `<div class="chat-note"><div class="muet petit">${t("Note for the pilot")}</div><div>${mdChat(note.trim())}</div>
      <div class="ligne m0 mt-1">${m.note ? `<span class="ok petit">✔ ${t("Sent to the pilot")}</span>` : `<button type="button" class="btn btn-petit btn-primaire" data-chat-note="${i}">${t("Send to the pilot")}</button>`}</div></div>` : ""}
    ${agent !== undefined ? carteAgentChat(agent, m, i) : ""}
    ${mcp !== undefined ? carteMCPChat(mcp, m, i) : ""}
    ${actions}</div>
    <div class="chat-meta">${h(quand)}${m.modele ? " · " + h(m.modele) : ""}${m.cout ? " · " + Number(m.cout).toFixed(3) + " $" : ""}${m.commit ? ` · <span class="ok">${t("committed {0}", [h(m.commit)])}</span>` : ""}</div>${relance}</div>`;
}

// The MCP server Claude found or wrote (between <mcp> and </mcp>): a card the user still checks, then adds
// to the library, switched on in this project (api_chat.go: apiChatMCP).
function carteMCPChat(json, m, i) {
  let a;
  try { a = JSON.parse(json.trim().replace(/^```(json)?|```$/g, "")); } catch (e) { a = null; }
  if (!a || typeof a !== "object" || !a.config || typeof a.config !== "object") return `<div class="chat-note"><div class="muet petit">${t("Claude's proposal could not be read: ask it to write it again.")}</div><pre class="chat-code">${h(json.trim())}</pre></div>`;
  if (m.mcp) return `<div class="chat-note"><span class="ok petit">✔ ${t("Added to the library and switched on: {0}", [h(m.mcp)])}</span> <a href="#extensions" class="petit">${t("Skills & MCP")}</a></div>`;
  const v = (x) => h(String(x || ""));
  const fichiers = a.fichiers && typeof a.fichiers === "object" ? Object.entries(a.fichiers) : [];
  return `<div class="chat-note chat-agent" data-chat-mcp-carte="${i}"><div class="muet petit">${fichiers.length ? t("MCP server written by Claude") : t("MCP server proposed")}</div>
    <input type="hidden" data-cm="source" value="${v(a.source)}">
    <label class="champ"><span>${t("Name")}</span><input type="text" data-cm="nom" value="${v(a.nom)}"></label>
    <label class="champ"><span>${t("What it lets the pilot do")}</span><input type="text" data-cm="description" value="${v(a.description)}"></label>
    <label class="champ"><span>${t("Configuration")}</span><textarea data-cm="config" rows="5" spellcheck="false">${h(JSON.stringify(a.config, null, 2))}</textarea></label>
    ${a.source ? `<div class="petit muet">${t("Source: {0}", [h(a.source)])}</div>` : ""}
    ${fichiers.map(([n, c]) => `<details class="chat-actions"><summary>📄 ${h(n)}</summary><pre class="chat-code">${h(String(c))}</pre></details>`).join("")}
    <label class="interrupteur"><input type="checkbox" data-cm="windows" ${a.windows ? "checked" : ""}><span>${t("Windows program: runs on Windows, outside the cage, with your rights")}</span></label>
    <div class="ligne m0 mt-1"><button type="button" class="btn btn-petit btn-primaire" data-chat-mcp="${i}">${t("Add to the library")}</button>
      <span class="petit muet">${t("Its keys, if it needs any, are filled in on the Skills & MCP page.")}</span></div></div>`;
}
async function ajouterMCPChat(b) {
  const carte = b.closest("[data-chat-mcp-carte]"); if (!carte) return;
  const champ = (n) => carte.querySelector(`[data-cm="${n}"]`);
  b.disabled = true;
  const r = await action(api("chat/mcp", "POST", { index: Number(b.dataset.chatMcp), nom: champ("nom").value.trim(), description: champ("description").value, source: champ("source").value, config: champ("config").value, windows: champ("windows").checked }));
  b.disabled = false;
  if (r) majChat(true);
}

// The sub-agent Claude proposes (between <agent> and </agent>): a card the user still edits, then adds to
// the team with one click (api_chat.go: apiChatAgent).
function carteAgentChat(json, m, i) {
  let a;
  try { a = JSON.parse(json.trim().replace(/^```(json)?|```$/g, "")); } catch (e) { a = null; }
  if (!a || typeof a !== "object") return `<div class="chat-note"><div class="muet petit">${t("Claude's proposal could not be read: ask it to write it again.")}</div><pre class="chat-code">${h(json.trim())}</pre></div>`;
  if (m.agent) return `<div class="chat-note"><span class="ok petit">✔ ${t("Added to the team: {0}", [h(m.agent)])}</span> <a href="#equipe" class="petit">${t("Team")}</a></div>`;
  const v = (x) => h(String(x || ""));
  return `<div class="chat-note chat-agent" data-chat-agent-carte="${i}"><div class="muet petit">${a.ancien ? t("Changes to the sub-agent {0}", [v(a.ancien)]) : t("Sub-agent proposed")}</div>
    <input type="hidden" data-ca="ancien" value="${v(a.ancien)}">
    <div class="grille grille-2">
      <label class="champ"><span>${t("Name")}</span><input type="text" data-ca="nom" value="${v(a.nom)}"></label>
      <label class="champ"><span>${t("Model")}</span><select data-ca="modele">
        <option value="" ${!a.modele ? "selected" : ""}>${t("Like the other sub-agents")}</option>
        <option value="eco" ${a.modele === "eco" ? "selected" : ""}>${t("Cheap model (mechanical work)")}</option>
        <option value="fort" ${a.modele === "fort" ? "selected" : ""}>${t("Most capable model (hard reasoning)")}</option></select></label>
    </div>
    <label class="champ"><span>${t("When the lead should call it")}</span><input type="text" data-ca="description" value="${v(a.description)}"></label>
    <label class="champ"><span>${t("Its instructions")}</span><textarea data-ca="consignes" id="ca-consignes-${i}" rows="8">${v(a.consignes)}</textarea></label>
    ${barreAmeliorer("ca-consignes-" + i, "agent")}
    <label class="interrupteur"><input type="checkbox" data-ca="lecture_seule" ${a.lecture_seule ? "checked" : ""}><span>${t("Read only: it reads and reports, never writes a file")}</span></label>
    <div class="ligne m0 mt-1"><button type="button" class="btn btn-petit btn-primaire" data-chat-agent="${i}">${a.ancien ? t("Save the changes") : t("Add to the team")}</button></div></div>`;
}
async function ajouterAgentChat(b) {
  const carte = b.closest("[data-chat-agent-carte]"); if (!carte) return;
  const champ = (n) => carte.querySelector(`[data-ca="${n}"]`);
  const agent = { nom: champ("nom").value.trim().toLowerCase(), description: champ("description").value, consignes: champ("consignes").value, modele: champ("modele").value, lecture_seule: champ("lecture_seule").checked };
  b.disabled = true;
  const r = await action(api("chat/agent", "POST", { index: Number(b.dataset.chatAgent), ancien: champ("ancien").value.trim(), agent }));
  b.disabled = false;
  if (r) majChat(true);
}

function filChat(d) {
  const msgs = d.messages || [];
  let html = msgs.length ? msgs.map(messageChat).join("") : `<div class="vide">${t("Ask anything about the project: how a part works, where something is, what the pilot did, why a milestone failed. A change you ask for becomes a note for the pilot; tick “Change the files” for Claude to make it itself. The i at the top shows everything the chat can do.")}</div>`;
  if (d.en_cours && (d.partiel || {}).mode === "attente") {
    html += `<div class="chat-msg chat-claude"><div class="chat-bulle chat-attente"><div class="muet petit"><span class="chat-points"><i></i><i></i><i></i></span> ${t("Waiting for the end of the pilot's cycle: Claude then changes the files, and the pilot starts again. Stop cancels it (the pilot goes on).")}</div></div></div>`;
  } else if (d.en_cours) {
    const p = d.partiel || {};
    const textes = p.textes || [], outils = p.outils || [];
    html += `<div class="chat-msg chat-claude"><div class="chat-bulle chat-attente">${textes.length ? mdChat(textes[textes.length - 1].replace(/<(note|agent|mcp)>[\s\S]*$/, "")) : ""}
      <div class="muet petit"><span class="chat-points"><i></i><i></i><i></i></span> ${outils.length ? h(t(outils[outils.length - 1])) : t("Claude is thinking…")}</div></div></div>`;
  }
  return html;
}

async function majChat(force) {
  if (etat.page !== "chat") return;
  const d = await api("chat").catch(() => null); if (!d) return;
  const sig = JSON.stringify([d.messages.length, d.en_cours, d.partiel, d.pilote_actif, d.messages.map((m) => (m.agent || "") + (m.mcp || "")).join(","), (d.messages[d.messages.length - 1] || {}).note]);
  chat.piloteActif = !!d.pilote_actif;
  if (!force && sig === chat.signature) return;
  chat.signature = sig;
  const fil = $("#chat-fil"); if (!fil) return;
  // on a phone the conversation scrolls with the content area (app.css), elsewhere in its own box
  const boite = TEL.matches ? $("#page") : fil;
  const enBas = boite.scrollHeight - boite.scrollTop - boite.clientHeight < (boite === fil ? 80 : 120);
  fil.innerHTML = filChat(d);
  if (enBas || force) boite.scrollTop = boite.scrollHeight;
  const b = $("#chat-envoyer"), s = $("#chat-stop"), m = $("#chat-mode");
  if (b) b.hidden = d.en_cours;
  if (s) s.hidden = !d.en_cours;
  if (m) m.innerHTML = modeChat();
}

// what Claude may do with the next message: the pilot's state, and the "change the files" switch (at the bottom)
function modeChat() {
  const point = chat.piloteActif ? `<span class="attention">●</span>` : `<span class="ok">●</span>`;
  if (!chat.modif) return `${point} ${t("Claude reads and answers: a change you ask for becomes a note for the pilot, sent with one click.")}`;
  if (chat.piloteActif) return `${point} ${t("The pilot stops at the end of its current cycle, Claude changes the files, Athanor commits them, then the pilot starts again on its own with a note of the change.")}`;
  return `${point} ${t("The pilot is stopped: Claude can change the project's files (no commands), and Athanor commits them.")}`;
}

// the ⓘ of the chat: what it can do
function aideChat() {
  return choisir([
    t("• Questions: how a part works, where something is, what the pilot did, why a milestone failed. Claude reads the project's files to answer."),
    t("• Notes for the pilot: by default Claude changes nothing. A change you ask for becomes a note with a “Send to the pilot” button; the pilot reads it at its next cycle."),
    t("• ✏️ Change the files: switched on, Claude makes the change you ask for itself (no commands), and Athanor commits it. If the pilot runs, it pauses at the end of its cycle, then starts again."),
    t("• 📎 Files: attach or paste images, PDF, documents; Claude reads them, and a note sent to the pilot names them."),
    t("• 🌐 Internet: Claude may search the web, only when a message needs it."),
    t("• Sub-agents: describe a specialist for the team; Claude shapes it with you, then a card adds it with one click."),
    t("• MCP servers: name a program or a service the pilot should drive; Claude finds a server (or writes one), then a card adds it to the library."),
  ].join("\n"), [{ id: "ok", texte: t("OK"), primaire: true }], { titre: t("What the chat can do"), large: true });
}

// tactile: a phone or a tablet (a finger, no mouse)
const tactile = () => matchMedia("(pointer: coarse)").matches;
// the field of the chat grows with what is written, up to a third of the screen (then it scrolls)
document.addEventListener("input", (ev) => {
  const z = ev.target; if (z.id !== "chat-texte" || !TEL.matches) return;
  z.style.height = "auto"; z.style.height = Math.min(z.scrollHeight + 2, Math.round(innerHeight * 0.32)) + "px";
});
pages.chat = async () => {
  const [d, mod] = await Promise.all([api("chat"), chat.modeles ? Promise.resolve({ modeles: chat.modeles }) : api("modeles").catch(() => ({ modeles: [] }))]);
  chat.modeles = mod.modeles || [];
  chat.signature = ""; chat.piloteActif = !!d.pilote_actif;
  const modeles = ["", ...chat.modeles];
  if (!modeles.includes(chat.modele)) modeles.push(chat.modele);
  setTimeout(() => majChat(true), 0);
  return `<div class="bloc chat">
    <div class="ligne m0 chat-tete"><h2 class="m0">${t("Chat with Claude")} <button type="button" class="chat-info" id="chat-info" title="${h(t("What the chat can do"))}" aria-label="${h(t("What the chat can do"))}">i</button></h2><span class="muet petit croit" id="chat-mode">${modeChat()}</span>
      <button type="button" class="btn btn-petit" id="chat-nouveau">${t("New conversation")}</button></div>
    <div class="chat-fil" id="chat-fil"></div>
    <div class="chat-saisie">
      <textarea id="chat-texte" rows="${tactile() ? 2 : 3}" placeholder="${h(tactile() ? t("Your question or the change you want…") : t("Your question or the change you want… (Enter sends, Shift+Enter: new line)"))}"></textarea>
      ${zonePieces("ct")}
      <div class="ligne m0 mt-1 chat-options">
        <label class="interrupteur m0 petit" title="${h(t("Off: Claude changes nothing, a change becomes a note for the pilot. On: Claude changes the files you ask it to change, and Athanor commits them (a running pilot pauses at the end of its cycle)."))}"><input type="checkbox" id="chat-modif" ${chat.modif ? "checked" : ""}><span>✏️ ${t("Change the files")}</span></label>
        <label class="interrupteur m0 petit" title="${h(t("Claude may search and read the web for your messages, only when one needs it (look online, compare with what exists elsewhere, a current fact). A question about the project is answered from its files."))}"><input type="checkbox" id="chat-internet" ${chat.internet ? "checked" : ""}><span>🌐 ${t("Internet")}</span></label></div>
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
  const message = zone.value.trim(), fichiers = etat.pieces.ct || [];
  if (!message && !fichiers.length) return;
  const mode = chat.modif ? "modif" : "";
  if (mode && chat.piloteActif && !(await confirmer(t("The pilot is running. It stops at the end of its current cycle (you see it at the top), then Claude changes the files, Athanor commits them, and the pilot starts again on its own with a note of the change."), { ok: t("Change the files") }))) return;
  chat.envoi = true;
  // the files first (into the workshop), then the message that lists them; one that fails stops the sending
  const pieces = [];
  for (const f of fichiers) {
    try { pieces.push((await envoyerPiece(f, "chat", etat.projet)).chemin); } catch (e) { toast(t("{0} could not be attached: {1}", [f.name, e.message]), "erreur"); chat.envoi = false; return; }
  }
  const r = await action(api("chat", "POST", { message, modele: chat.modele, mode, internet: chat.internet, pieces }));
  chat.envoi = false;
  if (r) {
    zone.value = ""; zone.style.height = ""; etat.pieces.ct = [];
    const ul = $("#ct-liste"); if (ul) ul.innerHTML = "";
    await majChat(true);
  }
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

// rendreDepense: API mode (connexion_claude.go): what Claude cost since the count started, against the
// budget when one is set, in place of the subscription's quota
function rendreDepense(d) {
  const dol = (x) => (x || 0).toFixed(2) + " $";
  const depuis = d.depuis ? t("since {0}", [quandLisible(d.depuis)]) : t("since Athanor 2.49");
  const pied = [d.deja ? t("of which {0} before the count", [dol(d.deja)]) : "", depuis, t("{0} runs of Claude", [d.runs || 0])].filter(Boolean).map(h).join(" · ");
  // the bar: against the budget (the account's credit), else against the automatic stop
  const plafond = d.budget || d.arret || 0;
  let corps;
  if (plafond) {
    const pct = Math.min(100, Math.round((d.depense / plafond) * 100));
    const classe = d.atteint || d.arret_atteint || pct >= 90 ? "plein" : pct >= 70 ? "haut" : "";
    const marque = d.budget && d.arret ? `<b class="quota-seuil" style="left:${Math.min(100, (d.arret / d.budget) * 100)}%" title="${h(t("Auto stop"))} ${h(dol(d.arret))}"></b>` : "";
    corps = `<div class="quota-ligne ${classe}">
      <div class="ligne m0"><b>${h(d.budget ? t("{0} of {1}", [dol(d.depense), dol(d.budget)]) : dol(d.depense))}</b><span class="espace"></span><span>${pct} %</span></div>
      <div class="barre m0 quota-barre"><i style="width:${pct}%"></i>${marque}</div>
      <div class="ligne m0 quota-pied"><span class="muet petit">${d.budget ? h(t("{0} left", [dol(Math.max(0, d.budget - d.depense))])) + " · " : ""}${pied}</span></div></div>`;
  } else {
    corps = `<div class="quota-ligne"><div class="ligne m0"><b>${h(dol(d.depense))}</b><span class="espace"></span><span class="muet petit">${pied}</span></div></div>
      <p class="muet petit">${t("No budget: the pilot never stops for the spend. Set one in Settings › Claude › API budget (the credit left on the API account).")}</p>`;
  }
  const etat = d.arret_atteint ? `<p class="petit"><span class="attention">${t("Automatic stop reached: the pilot starts no other cycle. Raise it or switch it off (Auto stop), then Start.")}</span></p>`
    : d.atteint ? `<p class="petit"><span class="attention">${t("Budget spent: the pilot waits. Raise it in Settings, count again from now, or put another API key: it resumes at once.")}</span></p>` : "";
  const arret = `<div class="quota-arret petit"><span class="muet">${t("Auto stop")}</span>
    <input type="text" inputmode="decimal" id="depense-arret" value="${d.arret || ""}" placeholder="${h(t("off"))}" title="${h(t("The dollars spent at which the pilot stops, after the cycle that reaches them (the cost of a cycle is known at its end). Empty: no automatic stop."))}"> $
    <span class="muet">${t("after the cycle")}</span></div>`;
  return `<div class="bloc quota-bloc"><h2>${t("API spend")}</h2>${!d.cle ? `<p class="petit"><span class="attention">${t("API mode is chosen, but no API key is set: Settings › Claude.")}</span></p>` : ""}${corps}${etat}
    <div class="ligne m0 quota-pied"><span class="espace"></span>${arret}</div>
    <div class="ligne mt-1"><span class="muet petit croit">${t("Counted by Athanor from what Claude Code reports at the end of each run (list price): the API account's own balance is on console.anthropic.com.")}</span><button class="btn btn-petit" type="button" id="depense-zero" title="${h(t("Credit added, or a new count: the spend starts again from 0 now."))}">${t("Count again from now")}</button></div></div>`;
}

// --- progress: the milestones done over time, the projection to the last one (time and cost), the cost of
// each cycle. A cycle that ends with a new commit is a milestone done (one milestone per cycle); the curve
// is anchored on the true count of the plan.
const PAUSE_AV = 20 * 60000; // Progress: a gap between two cycles longer than this is a pause, not work
function rendreAvancement(stats, plan, faits, actif) {
  const total = plan.length;
  const lignes = stats.map((s) => ({ ...s, fin: new Date(s.horodatage).getTime() })).filter((s) => !isNaN(s.fin)).sort((a, b) => a.fin - b.fin);
  if (lignes.length < 2 || !total) return "";
  let precedent = "";
  lignes.forEach((s) => { s.jalon = s.etat === "ok" && s.commit && s.commit !== precedent; if (s.commit) precedent = s.commit; });
  let n = faits;
  for (let i = lignes.length - 1; i >= 0; i--) { lignes[i].faits = Math.max(0, n); if (lignes[i].jalon) n--; }
  const dernier = lignes[lignes.length - 1];
  // the working time only: a gap between two cycles longer than PAUSE_AV (Athanor closed, the pilot stopped
  // for days) is a pause: left out of the pace and drawn as a short break on the time axis (the dates under
  // it go on from the day work resumed), never a long line across it. s.tc: the cycle's end on that axis.
  const debutCycle = (s) => s.fin - (Number(s.minutes) || 0) * 60000;
  const actif0 = lignes.reduce((a, s, i) => a + (s.fin - (i ? Math.max(lignes[i - 1].fin, debutCycle(s)) : debutCycle(s))), 0);
  const blanc = Math.max(60000, Math.min(PAUSE_AV, actif0 * 0.03)); // the break's width on the axis
  const coupures = [];
  let decale = 0;
  lignes.forEach((s, i) => {
    const trou = i ? debutCycle(s) - lignes[i - 1].fin : 0;
    if (trou > PAUSE_AV) {
      const tcAvant = lignes[i - 1].tc;
      decale += trou - blanc;
      coupures.push({ tcAvant, tcApres: tcAvant + blanc, avant: lignes[i - 1].fin, apres: debutCycle(s), decale });
    }
    s.tc = s.fin - decale;
  });
  const debut = lignes[0].tc - (Number(lignes[0].minutes) || 0) * 60000;
  const reel = (tc) => { let o = 0; coupures.forEach((c) => { if (tc >= c.tcApres) o = c.decale; }); return tc + o; };
  // pace: over the last 8 milestones (their cycles' working time, the pauses left out), and their cost
  const jalons = lignes.filter((s) => s.jalon).slice(-8);
  let eta = null, coutReste = null, heuresReste = null, finTc = null;
  const reste = total - faits;
  if (reste > 0 && jalons.length >= 2) {
    const i0 = lignes.indexOf(jalons[0]);
    const t0 = i0 > 0 ? lignes[i0 - 1].tc : jalons[0].tc - (Number(jalons[0].minutes) || 0) * 60000;
    const parJalon = (jalons[jalons.length - 1].tc - t0) / jalons.length;
    const cout = lignes.slice(i0).reduce((a, s) => a + (Number(s.cout_usd) || 0), 0) / jalons.length;
    eta = Math.max(Date.now(), dernier.fin) + parJalon * reste; // from now: a pause since the last cycle is left out too
    finTc = dernier.tc + parJalon * reste;
    heuresReste = (parJalon * reste) / 3600000;
    coutReste = cout * reste;
  }
  const largeur = ($("#page") || {}).clientWidth || 1000;
  const fin = finTc || dernier.tc, L = Math.round(Math.max(520, Math.min(1800, largeur - 110))), H = 210, g = 34, d = 40, hautB = 168, basB = 186;
  const x = (tm) => g + ((tm - debut) / Math.max(1, fin - debut)) * (L - g - d);
  const y = (k) => hautB - (k / total) * (hautB - 12);
  const maxCout = Math.max(...lignes.map((s) => Number(s.cout_usd) || 0), 0.01);
  const lang = document.documentElement.lang || "en";
  const plusieursJours = new Date(reel(debut)).toDateString() !== new Date(eta || dernier.fin).toDateString();
  const etiquette = (tm) => { const dt = new Date(tm); const hm = dt.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" }); return plusieursJours ? dt.toLocaleDateString(lang, { weekday: "short" }) + " " + hm : hm; };
  const points = [[debut, 0], ...lignes.map((s) => [s.tc, s.faits])];
  const chemin = points.map(([tm, k], i) => `${i ? "L" : "M"}${x(tm).toFixed(1)},${y(k).toFixed(1)}`).join(" ");
  const aire = `${chemin} L${x(dernier.tc).toFixed(1)},${hautB} L${x(debut).toFixed(1)},${hautB} Z`;
  const barres = lignes.map((s) => { const hb = ((Number(s.cout_usd) || 0) / maxCout) * 46; return `<rect class="av-cout" x="${(x(s.tc) - 3).toFixed(1)}" y="${(hautB - hb).toFixed(1)}" width="6" height="${hb.toFixed(1)}" rx="1.5"><title>${h(t("Cycle {0}", [s.cycle]))} · ${h(etiquette(s.fin))} · ${Number(s.cout_usd).toFixed(2)} $ · ${Math.round(s.minutes)} min${s.jalon ? " · " + h(t("milestone done")) : ""}</title></rect>`; }).join("");
  const grilles = [0.25, 0.5, 0.75, 1].map((f) => `<line class="av-grille" x1="${g}" x2="${L - d}" y1="${y(total * f).toFixed(1)}" y2="${y(total * f).toFixed(1)}"/><text class="av-txt" x="${g - 6}" y="${(y(total * f) + 4).toFixed(1)}" text-anchor="end">${Math.round(total * f)}</text>`).join("");
  // the dates under the axis: the start, each restart after a pause, the end (with evenly spaced ones while
  // there is no pause); a pause is a dashed break, its dates in a bubble
  const reperes = [[debut, reel(debut), "start"]];
  if (coupures.length) coupures.forEach((c) => reperes.push([c.tcApres, c.apres, "middle"]));
  else [0.25, 0.5, 0.75].forEach((f) => { const tc = debut + f * (fin - debut); reperes.push([tc, reel(tc), "middle"]); });
  reperes.push([fin, finTc ? eta : dernier.fin, "end"]);
  let dernierX = -1e9;
  const gardes = reperes.filter(([tc], i) => { const px = x(tc); if (i === reperes.length - 1) return true; if (px - dernierX < 95 || x(fin) - px < 95) return false; dernierX = px; return true; });
  const ticks = gardes.map(([tc, tm, ancre]) => `<text class="av-txt" x="${x(tc).toFixed(1)}" y="${basB + 14}" text-anchor="${ancre}">${h(etiquette(tm))}</text>`).join("");
  const breaks = coupures.map((c) => { const cx = x((c.tcAvant + c.tcApres) / 2).toFixed(1); return `<g class="av-coupure"><line x1="${cx}" x2="${cx}" y1="8" y2="${hautB + 6}"/><line class="av-coupure-zone" x1="${cx}" x2="${cx}" y1="8" y2="${hautB + 6}"/><title>${h(t("Pause: {0} → {1}", [etiquette(c.avant), etiquette(c.apres)]))}</title></g>`; }).join("");
  const projection = eta ? `<line class="av-projection" x1="${x(dernier.tc).toFixed(1)}" y1="${y(faits).toFixed(1)}" x2="${x(finTc).toFixed(1)}" y2="${y(total).toFixed(1)}"/><circle class="av-but" cx="${x(finTc).toFixed(1)}" cy="${y(total).toFixed(1)}" r="4"><title>${h(t("Estimated end: {0}", [quandLisible(eta / 1000)]))}</title></circle>` : "";
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
  infos.forEach((p) => { p.cx = x(p.s.tc); p.cy = y(p.s.faits); });
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
      ${grilles}<path class="av-aire" d="${aire}"/>${breaks}${barres}<path class="av-ligne" d="${chemin}"/>${projection}${pts}
      <line class="av-axe" x1="${g}" x2="${L - d}" y1="${hautB}" y2="${hautB}"/>${ticks}
    </svg>
    <div class="av-legende muet petit"><span><i class="av-l-ligne"></i>${t("milestones done")}</span>${eta ? `<span><i class="av-l-proj"></i>${t("projection")}</span>` : ""}<span><i class="av-l-cout"></i>${t("cost of each cycle (max {0} $)", [maxCout.toFixed(2)])}</span></div>
    ${detail}
  </div>`;
}

// lignesGains: Ponytail's state, then the cycles run with and without Ponytail or the eco mode, side by
// side on this project (gains.go: a trend, the milestones differ)
function lignesGains(g) {
  if (!g) return "";
  const noms = { ponytail: "Ponytail", eco: t("Eco mode") };
  const lignes = (g.comparaisons || []).map((c) => `<li>${t("{0}: {1} $ per successful cycle with it ({2} cycles), {3} $ without ({4}): {5}", [h(noms[c.option] || c.option), c.avec_cout.toFixed(2), c.avec_n, c.sans_cout.toFixed(2), c.sans_n,
    `<b class="${c.ecart <= 0 ? "gain" : "perte"}">${c.ecart > 0 ? "+" : ""}${c.ecart} %</b>`])}</li>`).join("");
  const etat = g.ponytail ? t("Ponytail {0} on: Claude builds each milestone with the smallest complete change.", [h(g.version_ponytail)]) : t("Ponytail off (Settings → Simplest code).");
  return `<div class="gains muet petit mt-1"><p class="m0">${etat}</p><p class="m0">${t("The cache is Claude's own, always on: what Ponytail, RTK and the eco mode save is measured apart, here.")}</p>${lignes ? `<ul class="m0">${lignes}</ul><p class="m0">${t("Measured on this project's own cycles: their milestones differ, so it is a trend, not a test.")}</p>` : ""}${referencePonytail(g)}</div>`;
}
// referencePonytail: while Ponytail has no comparison, how many successful cycles ran with it and without
// it (no gain is shown without cycles of both kinds: one is never invented)
function referencePonytail(g) {
  if ((g.comparaisons || []).some((c) => c.option === "ponytail")) return "";
  const avec = g.ponytail_avec || 0, sans = g.ponytail_sans || 0;
  let x = "";
  if (!avec && !sans) x = g.ponytail ? t("The comparison with and without appears once 3 successful cycles of each kind are done.") : "";
  else if (!sans) x = t("All {0} successful cycle(s) of this project ran with Ponytail: none without it to compare to, so no gain can be measured here.", [avec]);
  else if (!avec) x = t("All {0} successful cycle(s) of this project ran without Ponytail: none with it to compare to.", [sans]);
  else x = t("{0} successful cycle(s) with Ponytail, {1} without: the comparison appears once there are 3 of each.", [avec, sans]);
  return x ? `<p class="m0">${x}</p>` : "";
}

// Projects: every module's elements from /api/hub (the hub knows no module's details), those that need the owner first
const ETATS_ELEMENT = { actif: ["travaille", "In progress"], termine: ["entre_cycles", "Finished"], inactif: ["", "Idle"] };
function activiteElement(e) {
  const a = e.avancement, j = e.etat === "actif" && a && a.total && a.etape ? t(a.etape, [Math.min(a.fait + 1, a.total), a.total]) : "";
  const quoi = [e.activite ? titreJalon(e.activite) : "", e.action ? t(e.action) : ""].filter(Boolean).join(", ");
  return j && quoi ? t("{0}: {1}", [j, quoi]) : j || quoi;
}
// M189: a remote Athanor's project (nom@instance): no page here, its pilot's buttons; greyed when the remote is offline
function boutonsDistant(e) {
  if (e.hors_ligne) return `<div class="muet petit">${t("Offline: last state known")}</div>`;
  const b = (ordre, lib, cl = "") => `<button type="button" class="btn btn-petit${cl}" data-pilote-distant="${h(ordre)}">${t(lib)}</button>`;
  return `<div class="ligne m0">${e.etat === "actif" ? b("pause_fin_cycle", "Stop after cycle") + b("arreter", "Stop now", " btn-danger") : b("demarrer", "Start", " btn-primaire")}</div>`;
}
function ligneElement(e, modules) {
  const m = modules.find((x) => x.id === e.module) || { id: e.module, nom: e.module };
  if (e.distant) {
    const [classe, libelle] = e.hors_ligne ? ["", "Offline"] : ETATS_ELEMENT[e.etat] || ETATS_ELEMENT.inactif, a = e.avancement, act = activiteElement(e);
    return `<div class="bloc carte-projet ligne-element element-distant${e.attente ? " besoin" : ""}${e.hors_ligne ? " hors-ligne" : ""}" data-distant-id="${h(e.distant)}" data-distant-projet="${h(e.nom_distant)}">
    <div class="cp-tete"><span class="dot ${classe}" aria-hidden="true"></span><b class="cp-nom">${h(e.titre)}</b><span class="pastille-module"><span class="ico ico-${h(m.id)}" aria-hidden="true"></span>${h(t(m.nom))}</span></div>
    <div class="el-etat petit">${e.attente ? `<span class="cp-besoin">${t("Needs you")}</span> <span>${h(t(e.attente.raison, e.attente.args || []))}</span>` : `<span class="muet">${t(libelle)}</span>`}</div>
    ${act ? `<div class="cp-jalon petit" title="${h(act)}">${h(act)}</div>` : ""}
    ${a && a.total ? `<div class="barre"><i style="width:${(a.fait / a.total) * 100}%"></i></div>` : ""}${a && a.texte ? `<div class="muet petit">${h(t(a.texte, [a.fait, a.total]))}</div>` : ""}
    ${boutonsDistant(e)}
  </div>`;
  }
  const [classe, libelle] = ETATS_ELEMENT[e.etat] || ETATS_ELEMENT.inactif, a = e.avancement, act = activiteElement(e);
  const cible = e.attente ? e.attente.page : e.lien;
  // a Dev project: the project is selected before its page opens (cliqueProjet), any other module: a plain link
  const lien = e.projet ? `href="${h(cible)}" data-projet="${h(e.projet)}" data-cible="${h(cible.replace(/^#/, ""))}"` : `href="${h(cible)}"`;
  return `<a ${lien} class="bloc carte-projet ligne-element${e.attente ? " besoin" : ""}${e.projet && etat.vue && etat.vue.projet && etat.vue.projet.nom === e.projet ? " courant" : ""}">
    <div class="cp-tete"><span class="dot ${classe}" aria-hidden="true"></span><b class="cp-nom">${h(e.titre)}</b><span class="pastille-module"><span class="ico ico-${h(m.id)}" aria-hidden="true"></span>${h(t(m.nom))}</span></div>
    <div class="el-etat petit">${e.attente ? `<span class="cp-besoin">${t("Needs you")}</span> <span>${h(t(e.attente.raison, (e.attente.args || []).map(texteMessage)))}</span>` : `<span class="muet">${t(libelle)}</span>`}</div>
    ${act ? `<div class="cp-jalon petit" title="${h(act)}">${h(act)}</div>` : ""}
    ${a && a.total ? `<div class="barre"><i style="width:${(a.fait / a.total) * 100}%"></i></div>` : ""}${a && a.texte ? `<div class="muet petit">${a.total ? texteEtape({ etape: a.etape_plan, etapes: a.etapes_plan }) : ""}${h(t(a.texte, [a.fait, a.total]))}</div>` : ""}
  </a>`;
}
pages.global = async () => {
  const v = etat.vue; if (!v) return "";
  const d = await lireHub(), modules = d.modules || [], tous = d.elements || [];
  etat.globalSig = JSON.stringify([modules, tous]);
  const archives = (v.projets || []).filter((x) => x.archive).length;
  const actions = `<div class="ligne projets-actions"><a class="btn btn-petit" href="#nouveau">+ ${t("New project")}</a><a class="btn btn-petit" href="#rejoindre">⇄ ${t("Join a project")}</a>${archives ? `<a class="btn btn-petit btn-fantome" href="#archives">🗄 ${t("Archived projects ({0})", [archives])}</a>` : ""}</div>`;
  // filters: all, needs me, in progress, then one per module that has elements
  const avec = modules.filter((m) => tous.some((e) => e.module === m.id));
  const filtres = [["tous", t("All")], ["besoin", t("Needs me")], ["actif", t("In progress")], ...(avec.length > 1 ? avec.map((m) => ["m:" + m.id, t(m.nom)]) : [])];
  const f = filtres.some(([k]) => k === etat.filtreProjets) ? etat.filtreProjets : "tous";
  const garde = (e) => f === "tous" || (f === "besoin" ? !!e.attente : f === "actif" ? e.etat === "actif" : e.module === f.slice(2));
  const nb = (k) => tous.filter((e) => k === "tous" || (k === "besoin" ? !!e.attente : k === "actif" ? e.etat === "actif" : e.module === k.slice(2))).length;
  const barre = `<div class="filtres-projets" role="group" aria-label="${h(t("Filter"))}">${filtres.map(([k, lib]) => `<button type="button" class="filtre${k === f ? " actif" : ""}" data-filtre-projets="${h(k)}" aria-pressed="${k === f}">${h(lib)} <span class="muet">${nb(k)}</span></button>`).join("")}</div>`;
  const liste = tous.filter(garde);
  if (!tous.length) return `<div class="projets">${actions}<div class="vide">${t("No project.")}</div></div>`;
  return `<div class="projets">${actions}${barre}${liste.length ? `<div class="grille-projets">${liste.map((e) => ligneElement(e, modules)).join("")}</div>` : `<div class="vide">${t("Nothing here right now.")}</div>`}</div>`;
};
document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-pilote-distant]"); if (!b) return;
  const c = b.closest("[data-distant-id]"); b.disabled = true;
  const v = await action(api("distants", "POST", { action: "pilote", id: c.dataset.distantId, projet: c.dataset.distantProjet, ordre: b.dataset.piloteDistant }));
  if (v) toast(t("Order sent to {0}.", [c.querySelector(".cp-nom").textContent]));
  rendrePage();
});
document.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-filtre-projets]"); if (!b) return;
  etat.filtreProjets = b.dataset.filtreProjets; rendrePage();
});

// --- Home of the hub: the "What do you want to do?" bar, one tile per module of /api/hub, the missions
const ETATS_MODULE = { ok: ["travaille", "Active"], attention: ["quota", "Needs attention"], inactif: ["", "Idle"] };
const STATUTS_QUOTA = { allowed: "OK", allowed_warning: "Near the limit", rejected: "Limit reached" };
function statModule(s) {
  let valeur = `<b class="hub-valeur">${h(String(s.valeur))}</b>`;
  // the quota: a percentage drawn as the Overview's bar; a text figure (a status) is an i18n key
  if (s.cle === "Quota used" && typeof s.valeur === "number") valeur = `<b class="hub-valeur">${s.valeur} %</b><span class="quota-ligne ${s.valeur >= 90 ? "plein" : s.valeur >= 70 ? "haut" : ""}"><span class="barre m0"><i style="width:${Math.min(100, s.valeur)}%"></i></span></span>`;
  else if (typeof s.valeur === "string") valeur = `<b class="hub-valeur hub-statut">${h(t(STATUTS_QUOTA[s.valeur] || String(s.valeur)))}</b>`;
  return `<a class="hub-stat" href="${h(s.page || "#accueil")}">${valeur}<span class="muet petit">${h(t(s.cle))}</span></a>`;
}
function tuileModule(m) {
  const r = m.resume || {}, [classe, libelle] = ETATS_MODULE[r.etat] || ETATS_MODULE.inactif;
  const ouvrir = (m.pages || [])[0];
  return `<section class="bloc tuile-module${r.etat === "attention" ? " besoin" : ""}" data-module="${h(m.id)}">
    <div class="ligne m0 tuile-tete"><span class="dot ${classe}" aria-hidden="true"></span><h2 class="m0">${h(t(m.nom))}</h2><span class="muet petit">${h(t(libelle))}</span><span class="espace"></span>
      ${ouvrir ? `<a class="btn btn-petit" href="${h(ouvrir.hash)}">${t("Open")} →</a>` : ""}</div>
    <div class="hub-stats">${(r.stats || []).map(statModule).join("")}</div>
    ${m.id === "dev" ? raccourcisDev() : ""}
  </section>`;
}
// B6: New project, Manage and Archives one tap from Home (the phone needed three: menu, selector, entry)
function raccourcisDev() {
  const v = etat.vue || {}, archives = (v.projets || []).filter((x) => x.archive).length;
  return `<div class="ligne m0 tuile-raccourcis"><a class="btn btn-petit btn-fantome" href="#nouveau">+ ${t("New project")}</a>${v.projet ? `<a class="btn btn-petit btn-fantome" href="#projet">⚙ ${t("Manage this project")}</a>` : ""}${archives ? `<a class="btn btn-petit btn-fantome" href="#archives">🗄 ${t("Archived projects ({0})", [archives])}</a>` : ""}</div>`;
}
// Home's "In progress": the same elements as Projects, those that need the owner first (the API's order), then the working ones
function blocEnCours(tous, modules) {
  const liste = tous.filter((e) => e.attente || e.etat === "actif").slice(0, 6);
  return `<section class="bloc en-cours" aria-labelledby="en-cours-titre">
    <div class="ligne m0 tuile-tete"><h2 class="m0" id="en-cours-titre">${t("In progress")}</h2><span class="espace"></span><a class="btn btn-petit" href="#global">${t("See all")} →</a></div>
    ${liste.length ? `<div class="grille-projets">${liste.map((e) => ligneElement(e, modules)).join("")}</div>` : `<div class="vide">${t("Nothing in progress right now.")}</div>`}
  </section>`;
}
// M151: mentions of the bar. A chip = { type: projet|serveur|cle|nouveau, ref, nom }; the Go checks the refs again.
const TYPES_MENTION = { projet: "Project", serveur: "Server", cle: "Vault key", dossier: "Folder", nouveau: "New project" };
const pucesMentions = () => (etat.accueilMentions || []).map((m, i) => `<span class="puce-mention puce-${m.type}" title="${h(t(TYPES_MENTION[m.type]))}">${m.type === "nouveau" ? "" : "@"}${h(m.nom)}<button type="button" class="puce-x" data-retirer-mention="${i}" aria-label="${h(t("Remove {0}", [m.nom]))}">×</button></span>`).join("");
function suggestionsMentions() {
  const pris = new Set((etat.accueilMentions || []).map((m) => m.type + "\0" + m.ref)), l = etat.mentionsSugg || [];
  const choix = [...l.filter((m) => m.type === "projet").slice(0, 3), ...l.filter((m) => m.type === "serveur").slice(0, 3), { type: "nouveau", ref: "", nom: t("New project") }];
  return choix.filter((m) => !pris.has(m.type + "\0" + m.ref)).map((m) => `<button type="button" class="btn btn-petit puce-sugg" data-mention="${h(JSON.stringify(m))}">${m.type === "nouveau" ? "+ " : "@"}${h(m.nom)}</button>`).join("");
}
function majMentions() {
  const z = $("#accueil-puces"), s = $("#accueil-sugg");
  if (z) z.innerHTML = pucesMentions();
  if (s) s.innerHTML = suggestionsMentions();
}
function ajouterMention(m) {
  const l = etat.accueilMentions = etat.accueilMentions || [];
  if (!l.some((x) => x.type === m.type && x.ref === m.ref)) l.push({ type: m.type, ref: m.ref, nom: m.nom });
  majMentions();
}
// the @word right before the cursor, or null
function motMention(champ) {
  const r = /(^|\s)@([^\s@]*)$/.exec(champ.value.slice(0, champ.selectionStart));
  return r ? { q: r[2], debut: champ.selectionStart - r[2].length - 1 } : null;
}
function fermerListeMentions() {
  const liste = $("#accueil-liste"), champ = $("#accueil-texte");
  etat.mentionListe = null;
  if (liste) { liste.hidden = true; liste.innerHTML = ""; }
  if (champ) { champ.setAttribute("aria-expanded", "false"); champ.removeAttribute("aria-activedescendant"); }
}
function rendreListeMentions() {
  const liste = $("#accueil-liste"), champ = $("#accueil-texte"), ml = etat.mentionListe;
  if (!liste || !champ || !ml) return;
  liste.innerHTML = ml.items.length ? ml.items.map((m, i) => `<li role="option" id="mention-opt-${i}" class="mention-opt${i === ml.actif ? " actif" : ""}" aria-selected="${i === ml.actif}" data-opt="${i}">
    <span class="etiquette">${h(t(TYPES_MENTION[m.type]))}</span><b>${h(m.nom)}</b>${m.detail ? `<span class="muet petit">${h(m.detail)}</span>` : ""}</li>`).join("")
    : `<li class="mention-vide muet petit" role="option" aria-disabled="true">${t("Nothing matches.")}</li>`;
  liste.hidden = false; champ.setAttribute("aria-expanded", "true");
  if (ml.items.length) { champ.setAttribute("aria-activedescendant", "mention-opt-" + ml.actif); $("#mention-opt-" + ml.actif).scrollIntoView({ block: "nearest" }); } else champ.removeAttribute("aria-activedescendant");
}
let numeroMentions = 0;
async function chercherMentions(champ) {
  const mot = motMention(champ);
  if (!mot) { fermerListeMentions(); return; }
  const n = ++numeroMentions;
  let items = [];
  try { // the chips already chosen are not offered again
    const pris = new Set((etat.accueilMentions || []).map((m) => m.type + "\0" + m.ref));
    items = ((await api("mentions?q=" + encodeURIComponent(mot.q))).mentions || []).filter((m) => !pris.has(m.type + "\0" + m.ref));
  } catch { /* no list: the text stays as typed */ }
  if (n !== numeroMentions || !motMention(champ)) return; // a later keystroke won
  etat.mentionListe = { items, actif: 0, debut: mot.debut };
  rendreListeMentions();
}
function choisirMention(i) {
  const champ = $("#accueil-texte"), ml = etat.mentionListe, m = ml && ml.items[i];
  if (!champ || !m) return;
  const v = champ.value, fin = champ.selectionStart;
  champ.value = v.slice(0, ml.debut) + v.slice(fin).replace(/^\s*/, ""); // the @word goes, the text before keeps its space
  etat.accueilTexte = champ.value; champ.setSelectionRange(ml.debut, ml.debut);
  fermerListeMentions(); ajouterMention(m); champ.focus();
}
document.addEventListener("keydown", (ev) => {
  if (ev.target.id !== "accueil-texte") return;
  const ml = etat.mentionListe;
  if (ml && ev.key === "Escape") { ev.preventDefault(); ev.stopPropagation(); fermerListeMentions(); return; }
  if (ml && ml.items.length && (ev.key === "ArrowDown" || ev.key === "ArrowUp")) {
    ev.preventDefault(); ml.actif = (ml.actif + (ev.key === "ArrowDown" ? 1 : ml.items.length - 1)) % ml.items.length; rendreListeMentions(); return;
  }
  if (ml && ml.items.length && ev.key === "Enter") { ev.preventDefault(); choisirMention(ml.actif); return; }
  // Backspace at the very start removes the last chip
  if (ev.key === "Backspace" && ev.target.selectionStart === 0 && ev.target.selectionEnd === 0 && (etat.accueilMentions || []).length) { etat.accueilMentions.pop(); majMentions(); }
});
document.addEventListener("mousedown", (ev) => { const o = ev.target.closest("#accueil-liste [data-opt]"); if (o) { ev.preventDefault(); choisirMention(+o.dataset.opt); } });
document.addEventListener("click", (ev) => {
  const x = ev.target.closest("[data-retirer-mention]"), s = ev.target.closest("[data-mention]");
  if (x) { (etat.accueilMentions || []).splice(+x.dataset.retirerMention, 1); majMentions(); $("#accueil-texte")?.focus(); }
  else if (s) { ajouterMention(JSON.parse(s.dataset.mention)); $("#accueil-texte")?.focus(); }
  else if (etat.mentionListe && !ev.target.closest("#accueil-champ, #accueil-liste")) fermerListeMentions();
});
// Tab leaves the field: the list goes too
document.addEventListener("focusout", (ev) => { if (ev.target.id === "accueil-texte" && etat.mentionListe) fermerListeMentions(); });
pages.accueil = async () => {
  // B4: /api/hub failing still shows the chat bar, with a readable notice above it
  const [d, sugg, mis] = await Promise.all([lireHub().catch((e) => ({ panne: e.message || String(e) })), api("mentions").catch(() => ({})), api("missions").catch(() => ({}))]), modules = d.modules || [];
  // the card shown: the one followed in this window, else the newest still being prepared or proposed
  const lm = mis.missions || [], vue = etat.missionVue, courante = (vue && lm.find((x) => x.id === vue.mission.id)) || lm.find((x) => ACTIVE_MISSION.includes(x.etat));
  etat.missionVue = courante ? await api("missions/" + encodeURIComponent(courante.id)).catch(() => null) : null;
  clearTimeout(minuterieMission);
  if (etat.missionVue && travailleMission(etat.missionVue)) minuterieMission = setTimeout(lireMissionVue, 2000);
  etat.hubSig = JSON.stringify([modules, d.elements || []]); if (!d.panne) rendreMenu(modules);
  etat.mentionsSugg = sugg.mentions || []; etat.mentionListe = null;
  const hc = etat.hubChat || { messages: [], pouvoirs: {} };
  setTimeout(() => majHubChat(true), 0);
  return `<div class="accueil">${d.panne ? `<p class="bloc erreur" role="alert">${h(t("Athanor could not read its modules ({0}): the menu keeps its first entries. Reload the page in a moment.", [d.panne]))}</p>` : ""}
    <form class="bloc accueil-barre accueil-chat" id="accueil-form" data-pieces="hc">
      <div class="ligne m0 chat-tete"><label for="accueil-texte" class="accueil-titre croit">${t("What do you want to do?")}</label>
        <button type="button" class="btn btn-petit" id="hub-chat-aide" title="${h(t("How the assistant works"))}" aria-label="${h(t("How the assistant works"))}">?</button>
        <button type="button" class="btn btn-petit local-seul" id="hub-chat-nouveau">${t("New conversation")}</button></div>
      <div class="chat-fil hub-chat-fil" id="hub-chat-fil" role="log" aria-label="${h(t("Conversation"))}">${filHub(hc)}</div>
      <p class="petit muet m0 distant-seul">${t("From a device, this conversation is read only: write to the assistant on the computer.")}</p>
      <div class="champ-mentions local-seul" id="accueil-champ"><span id="accueil-puces" class="puces-mentions">${pucesMentions()}</span><textarea id="accueil-texte" class="croit" rows="${tactile() ? 2 : 3}" autocomplete="off" role="combobox" aria-autocomplete="list" aria-expanded="false" aria-controls="accueil-liste" aria-describedby="accueil-aide" placeholder="${h(tactile() ? t("Ask, or say what you want done…") : t("Ask, or say what you want done… (Enter sends, Shift+Enter: new line)"))}"></textarea></div>
      <ul id="accueil-liste" class="liste-mentions" role="listbox" aria-label="${h(t("Mentions"))}" hidden></ul>
      <input type="file" multiple hidden id="hc-fichiers" accept="image/png,image/jpeg,image/gif,image/webp,.pdf,.txt,.md,.csv,.tsv,.json,.yaml,.yml,.xml,.toml,.ini,.log,.html,.css,.js,.ts,.go,.py,.sql">
      <ul class="pieces-liste" id="hc-liste">${listePieces("hc")}</ul>
      <div class="ligne m0 puces-suggestions local-seul" id="accueil-sugg" role="group" aria-label="${h(t("Add to your request"))}">${suggestionsMentions()}</div>
      <div class="ligne m0 mt-1 hub-chat-bas">${pouvoirsHub(hc.pouvoirs || {})}<span class="espace"></span>
        <button type="button" class="btn local-seul" data-choisir-pieces="hc" title="${h(t("Images, PDF and text files, 20 MB at most each; you can also drop or paste them"))}">📎 ${t("Attach files")}</button>
        <button type="button" class="btn local-seul" id="accueil-mission">${t("Create a mission")}</button>
        <button type="button" class="btn local-seul" id="hub-chat-stop" ${hc.en_cours ? "" : "hidden"}>${t("Stop")}</button>
        <button type="submit" class="btn btn-primaire local-seul" id="hub-chat-envoyer" ${hc.en_cours ? "hidden" : ""}>${t("Send")}</button></div>
      <div class="muet petit local-seul" id="accueil-aide">${t("Type @ to name a project, a server or a vault key.")} ${t("The orchestrator prepares a mission card first: nothing starts before you press Launch.")}</div>
    </form>
    <div id="mission-zone" aria-live="polite">${zoneMission()}</div>
    <div class="grille-hub">${modules.filter((m) => ((m.resume || {}).stats || []).length).map(tuileModule).join("")}</div>
    ${blocEnCours(d.elements || [], modules)}
    ${blocMissions(lm)}
  </div>`;
};
// M156: Send creates a mission; its card shows under the bar (DESIGN §Missions). etat.missionVue = GET /api/missions/{id}.
const ACTIVE_MISSION = ["preparation", "proposee"];
const APPROBATIONS_PREVUES = { mise_en_prod: "Putting {0} in production", plan_serveurs: "The Servers plan of {0}", deploiement: "Each deployment to {0}", durcissement: "Hardening the server {0}", deverrouiller_coffre: "Unlocking the infra vault" };
const POLITIQUES_MISSION = [["prod", "Ask me for production", "Production and its servers' plans wait for your approval."],
  ["tout", "Ask me for everything", "Every Servers plan and every deployment waits for your approval."], ["autonome", "Autonomous", "Production still always waits for your approval, even in autonomous mode; everything else (Servers plans, dev and staging) runs without you."]];
const travailleMission = (d) => d.orchestrateur || d.mission.etat === "preparation";
const heuresMission = (m) => m < 90 ? `${m} min` : `${Math.round(m / 60)} h`;
// C81: an environment of the card (protection, servers named by nom, policy); sansUrl: the URL is shown apart
const envMission = (x, sansUrl, nom) => [sansUrl ? "" : x.url || "—", t(PROTECTIONS_ENV[x.protection] || x.protection || ""), (x.serveurs || []).length ? t("Servers") + " " + x.serveurs.map(nom).join(", ") : "",
  x.politique ? t(x.politique === "auto" ? "Automatic when nothing is destroyed" : "Ask for my approval") : ""].filter(Boolean).join(" · ");
function carteMission(d) {
  const m = d.mission, c = m.carte, ed = etat.missionEdition && etat.missionEdition.id === m.id ? etat.missionEdition : null;
  const nomServeur = (r) => ((etat.mentionsSugg || []).find((x) => x.type === "serveur" && x.ref === r) || {}).nom || r;
  const liste = (l) => l && l.length ? `<ul class="mis-puces">${l.map((x) => `<li>${h(x)}</li>`).join("")}</ul>` : `<span class="muet">—</span>`;
  const e = c.estimation || {}, actif = !travailleMission(d) && m.etat === "proposee", dis = ed ? "" : " disabled";
  return `<div class="mis-voies">
      <div class="mis-voie"><h3>${t((etat.hubModules || []).find((m) => m.id === "dev")?.nom || "dev")}</h3><p class="m0"><b>${h(c.projet.nom)}</b> <span class="etiquette">${t(c.projet.nouveau ? "New project" : "Existing project")}</span></p><p class="mis-brief">${h(c.dev.brief || "")}</p>${c.dev.modes ? `<p class="m0">${[["parallele", "Parallel mode"], ["eco", "Eco mode"], ["rapide", "Fast mode"]].filter(([k]) => c.dev.modes[k]).map(([, n]) => `<span class="etiquette">${t(n)}</span>`).join(" ")}</p>` : ""}</div>
      <div class="mis-voie"><h3>${t("Servers")}</h3>${c.avertissement_serveurs ? `<p class="mis-avert" role="note">⚠ ${t("Native Windows mode: the Servers lane needs WSL, or Force on the project's Access page.")}</p>` : ""}<p class="mis-brief">${h(c.serveurs.brief || t("Nothing to prepare on the servers."))}</p></div>
    </div>
    <div class="mis-grille">
      <div><h3>${t("Environments")}</h3>${(c.envs || []).length ? `<ul class="mis-envs">${c.envs.map((x) => `<li><b>${h(x.nom)}</b> <span class="mis-url">${h(x.url || "—")}</span> <span class="muet petit">${h(envMission(x, true, nomServeur))}</span>${x.avant ? `<div class="muet petit mis-avant">${h(t("Before: {0}", [envMission(x.avant, false, nomServeur)]))}</div>` : ""}</li>`).join("")}</ul>` : `<span class="muet">—</span>`}
        <h3>${t("Access used")}</h3>${c.avertissement_infra ? `<p class="mis-avert" role="note">⚠ ${t("Native Windows mode: an infra key is ticked. Untick it, or switch to WSL (an existing project can also Force on its Access page).")}</p>` : ""}${(c.acces || []).length ? `<ul class="mis-acces">${c.acces.map((a, i) => `<li><label class="case"><input type="checkbox" data-mis-acces="${i}"${(ed ? !ed.decoches.includes(i) : !a.decoche) ? " checked" : ""}${dis}> <b>${h(a.entree)}</b></label> ${a.type ? `<span class="etiquette">${h(a.type)}</span>` : ""}${a.a_remplir ? ` <span class="badge mis-remplir">${t("to fill")}</span>` : ""} <span class="muet petit">${t("used for {0}", [h(nomServeur(a.pour))])}</span></li>`).join("")}</ul>` : `<span class="muet">${t("No vault entry.")}</span>`}</div>
      <div><h3>${t("I will ask for your approval for")}</h3>${liste((c.approbations || []).map((a) => t(APPROBATIONS_PREVUES[a.type] || a.type, [a.cible || ""])))}
        <h3>${t("What will be touched")}</h3><dl class="fiche mis-touche">
          <dt>${t("Servers")}</dt><dd>${liste(c.touche.serveurs)}</dd><dt>${t("DNS names")}</dt><dd>${liste(c.touche.dns)}</dd>
          <dt>${t("Files")}</dt><dd>${liste(c.touche.fichiers)}</dd><dt>${t("Folder")}</dt><dd>${c.touche.dossier ? h(c.touche.dossier) : t("A new folder for the project")}</dd></dl>
        <h3>${t("Estimate")}</h3><p class="m0">${t("{0} to {1} of work, about {2} milestones", [heuresMission(e.minutes_min), heuresMission(e.minutes_max), e.jalons])}</p>
        <p class="muet petit m0">${e.cout_max ? t("About {0} $ to {1} $ of quota (list price)", [e.cout_min, e.cout_max]) : t("Quota unknown until a first cycle is measured on this PC.")}</p></div>
    </div>
    <fieldset class="mis-politique"><legend>${t("Policy")}</legend>${POLITIQUES_MISSION.map(([v, l, aide]) => `<label class="radio"><input type="radio" name="mis-politique" value="${v}"${(ed ? ed.politique : m.politique) === v ? " checked" : ""}${dis}> <span><b>${t(l)}</b><br><span class="muet petit">${t(aide)}</span></span></label>`).join("")}</fieldset>
    ${ed ? `<label for="mis-consigne" class="petit">${t("Ask for a change (optional)")}</label><textarea id="mis-consigne" rows="2" maxlength="2000" placeholder="${h(t("For example: put the dev on the home server"))}">${h(ed.consigne)}</textarea>
      <div class="ligne"><button type="button" class="btn btn-primaire" data-mis="appliquer">${t("Apply")}</button><button type="button" class="btn" data-mis="fermer">${t("Cancel")}</button></div>`
    : actif ? `<div class="ligne"><button type="button" class="btn" data-mis="modifier">${t("Edit")}</button><button type="button" class="btn btn-primaire" data-mis="lancer">${t("Launch")}</button><span class="espace"></span><button type="button" class="btn" data-mis="annuler">${t("Cancel the mission")}</button></div>` : ""}`;
}
function zoneMission() {
  const d = etat.missionVue; if (!d) return "";
  const m = d.mission, q = (m.questions || []).find((x) => !x.reponse), fin = (m.historique || []).slice(-1)[0] || {};
  if (m.etat === "echec") return `<section class="bloc mis-carte mis-echec" role="alert"><b>${t("The orchestrator could not prepare the card.")}</b> <span class="muet">${h(fin.detail || "")}</span>
    <div class="ligne"><button type="button" class="btn btn-petit" data-mis="oublier">${t("Close")}</button></div></section>`;
  if (!ACTIVE_MISSION.includes(m.etat)) return m.etat === "annulee" ? "" : `<section class="bloc mis-carte"><b>${h(t(ETATS_MISSION[m.etat] || m.etat))}</b> · ${h((m.carte || {}).titre || "")} <a href="#missions">${t("See the missions")} →</a></section>`;
  const prep = travailleMission(d) && !q ? `<div class="mis-prep" role="status"><span class="mis-anim" aria-hidden="true"></span>${t("The orchestrator is preparing the card…")}<span class="espace"></span>${m.carte ? "" : `<button type="button" class="btn btn-petit" data-mis="annuler">${t("Cancel")}</button>`}</div>` : "";
  const question = q ? `<div class="mis-question" role="group" aria-labelledby="mis-q-texte"><b>${t("The orchestrator asks")}</b><p id="mis-q-texte" class="m0">${h(q.texte)}</p>
    ${(q.choix || []).length ? `<div class="ligne m0">${q.choix.map((c, i) => `<button type="button" class="btn btn-petit" data-mis-choix="${i}">${h(c)}</button>`).join("")}</div>` : ""}
    <form class="ligne m0" id="mis-q-form" data-question="${h(q.id)}"><input type="text" id="mis-q-libre" class="croit" maxlength="1000" aria-label="${h(t("Your answer"))}" placeholder="${h(t("Or write your answer…"))}" value="${h(etat.missionReponse || "")}"><button type="submit" class="btn">${t("Answer")}</button></form>
    ${m.carte ? "" : `<div class="ligne m0"><span class="espace"></span><button type="button" class="btn btn-petit" data-mis="annuler">${t("Cancel the mission")}</button></div>`}</div>` : "";
  return `<section class="bloc mis-carte${prep && m.carte ? " mis-attente" : ""}" aria-labelledby="mis-titre">
    <div class="ligne m0 tuile-tete"><h2 class="m0" id="mis-titre">${h((m.carte || {}).titre || t("New mission"))}</h2><span class="badge mis-etat-${h(m.etat)}">${h(t(ETATS_MISSION[m.etat] || m.etat))}</span></div>
    <p class="muet petit mis-phrase">${h(m.phrase)}</p>${prep}${question}${m.carte ? carteMission(d) : ""}</section>`;
}
let minuterieMission;
function majZoneMission() {
  const z = $("#mission-zone"); if (!z) return;
  garderFocus(z, () => { z.innerHTML = zoneMission(); });
  clearTimeout(minuterieMission);
  const d = etat.missionVue;
  if (d && travailleMission(d) && ACTIVE_MISSION.includes(d.mission.etat)) minuterieMission = setTimeout(lireMissionVue, 2000); // the state is read every 2 s
}
async function lireMissionVue() {
  const d = etat.missionVue;
  if (!d || !["accueil", "mission"].includes(etat.page) || !$("#mission-zone")) return;
  try { etat.missionVue = await api("missions/" + encodeURIComponent(d.mission.id)); } catch { /* next try in 2 s */ }
  majZoneMission();
}
async function actionMission(chemin, corps) {
  const d = etat.missionVue; if (!d) return null;
  const r = await action(api("missions/" + encodeURIComponent(d.mission.id) + "/" + chemin, "POST", corps || {}), "");
  if (r && r.mission) { etat.missionVue = { mission: r.mission, orchestrateur: chemin === "repondre" || !!(corps && corps.consigne) }; majZoneMission(); }
  // the detail page of a mission just launched or cancelled: shown again with its lanes
  if (r && r.mission && etat.page === "mission" && !ACTIVE_MISSION.includes(r.mission.etat)) await rendrePage();
  return r;
}
document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-mis], [data-mis-choix]"), d = etat.missionVue; if (!b || !d) return;
  const m = d.mission, a = b.dataset.mis;
  if (b.dataset.misChoix !== undefined) { const q = m.questions.find((x) => !x.reponse); etat.missionReponse = ""; await actionMission("repondre", { question: q.id, choix: q.choix[+b.dataset.misChoix] }); return; }
  if (a === "modifier") { etat.missionEdition = { id: m.id, decoches: m.carte.acces.map((x, i) => x.decoche ? i : -1).filter((i) => i >= 0), politique: m.politique, consigne: "" }; majZoneMission(); $("#mission-zone input[type=checkbox], #mission-zone input[type=radio]")?.focus(); }
  else if (a === "fermer") { etat.missionEdition = null; majZoneMission(); }
  else if (a === "appliquer") {
    const ed = etat.missionEdition, consigne = ed.consigne.trim();
    const r = await actionMission("modifier", { acces_decoches: ed.decoches.map((i) => ({ entree: m.carte.acces[i].entree, pour: m.carte.acces[i].pour })), politique: ed.politique, ...(consigne ? { consigne } : {}) });
    if (r) { etat.missionEdition = null; majZoneMission(); }
  } else if (a === "lancer") { // C78: launching gives the card's keys, as the Access page does
    const p = await preuveMdp(t("Launching the mission gives its keys to the project: retype Athanor's password."), { ok: t("Launch") });
    if (p) await actionMission("lancer", { ...p, empreinte: m.empreinte }); // R3: the card read, never a newer one
  }
  else if (a === "annuler") { if (await choisir(t("Cancel this mission? Nothing has been built or deployed yet."), [{ id: "non", texte: t("Keep it") }, { id: "oui", texte: t("Cancel the mission"), danger: true }]) === "oui") await actionMission("annuler"); }
  else if (a === "oublier") { etat.missionVue = null; majZoneMission(); }
});
document.addEventListener("change", (ev) => {
  const ed = etat.missionEdition; if (!ed) return;
  if (ev.target.dataset.misAcces !== undefined) { const i = +ev.target.dataset.misAcces; ed.decoches = ev.target.checked ? ed.decoches.filter((x) => x !== i) : [...ed.decoches, i]; }
  else if (ev.target.name === "mis-politique") ed.politique = ev.target.value;
});
document.addEventListener("input", (ev) => { if (ev.target.id === "mis-consigne" && etat.missionEdition) etat.missionEdition.consigne = ev.target.value; else if (ev.target.id === "mis-q-libre") etat.missionReponse = ev.target.value; });
document.addEventListener("submit", async (ev) => {
  if (ev.target.id !== "mis-q-form") return;
  ev.preventDefault();
  const texte = ($("#mis-q-libre").value || "").trim(); if (!texte) return;
  if (await actionMission("repondre", { question: ev.target.dataset.question, texte })) etat.missionReponse = "";
});
async function envoyerAccueil() {
  const texte = ($("#accueil-texte").value || "").trim(); if (!texte || etat.envoiMission) return;
  etat.envoiMission = true; // B86: a double click sends once
  const r = await action(api("missions", "POST", { phrase: texte, mentions: (etat.accueilMentions || []).map((m) => ({ type: m.type, ref: m.ref })) }), "").finally(() => { etat.envoiMission = false; });
  if (!r) return;
  etat.accueilTexte = ""; etat.accueilMentions = []; $("#accueil-texte").value = ""; majMentions();
  etat.missionVue = { mission: r.mission, orchestrateur: r.mission.etat === "preparation" }; etat.missionEdition = null;
  majZoneMission();
}
document.addEventListener("submit", (ev) => { if (ev.target.id === "accueil-form") { ev.preventDefault(); envoyerHubChat(); } });

// M195: the home conversation (hub_chat.go), the project chat with the hub's scope. Its powers are the
// conversation's (kept by the server, off by default, gone with a new conversation), always shown under the field.
let minuterieHubChat;
const sansCartes = (m) => ({ ...m, texte: (m.texte || "").replace(/<(note|agent|mcp|action)>[\s\S]*?<\/\1>/g, "") }); // cards of the project chat: not here; its own card: carteActionHub
function filHub(d) {
  const msgs = (d.messages || []).map(sansCartes), cartes = d.actions || {};
  let html = msgs.length ? msgs.map((m, i) => messageChat(m, i) + (cartes[i] ? carteActionHub(cartes[i], i) : "")).join("") : `<div class="vide">${t("Ask anything about your projects, servers or missions. By default the assistant only reads and answers: it sees no file and no key until you give it more under “What the assistant can do”.")}</div>`;
  if (d.en_cours) {
    const p = d.partiel || {}, textes = p.textes || [], outils = p.outils || [];
    html += `<div class="chat-msg chat-claude"><div class="chat-bulle chat-attente">${textes.length ? mdChat(textes[textes.length - 1].replace(/<(note|agent|mcp|action)>[\s\S]*$/, "")) : ""}
      <div class="muet petit"><span class="chat-points"><i></i><i></i><i></i></span> ${outils.length ? h(t(outils[outils.length - 1])) : t("Claude is thinking…")}</div></div></div>`;
  }
  return html;
}
// M197: the card of an action the assistant proposes (computed by the Go: hub_chat_actions.go): what, on what, with
// which accesses; Launch, Change (its fields) or Refuse. Nothing runs before Launch.
const ICONES_ACTION = { projet: "🚀", pilote: "▶", demande: "📝", remarque: "💬", mission: "🧭", agent: "🧩", mcp: "🔌", attribuer: "📌" };
const MODELES_AGENT = [["", "Like the other sub-agents"], ["eco", "Cheap model (mechanical work)"], ["fort", "Most capable model (hard reasoning)"]];
// M198: a sub-agent or an MCP server (the <agent> and <mcp> cards of the project chat) added to the library and switched
// on in the projects ticked here; attribuer: one of the library switched on in projects. The Go checks them again.
function champsEquipeHub(c, projets) {
  const v = (x) => h(String(x || "")), ty = c.type;
  const choix = projets.map((n) => `<label class="interrupteur m0"><input type="checkbox" data-cact-projet value="${h(n)}" ${(c.projets || []).includes(n) ? "checked" : ""}><span>${h(n)}</span></label>`).join("")
    || `<span class="muet petit">${t("No project yet.")}</span>`;
  const surProjets = `<fieldset class="carte-action-projets m0"><legend class="petit">${ty === "attribuer" ? t("Projects") : t("Also switch it on in these projects (optional)")}</legend>${choix}</fieldset>`;
  if (ty === "attribuer") return { vue: surProjets, champs: "" };
  if (ty === "agent") return {
    vue: `${c.description ? `<div class="petit">${h(c.description)}</div>` : ""}
      <div class="petit muet">${t("{0}: {1}", [t("Model"), ""])}${t((MODELES_AGENT.find(([k]) => k === (c.modele || "")) || MODELES_AGENT[0])[1])}${c.lecture_seule ? " · " + t("Read only") : ""}</div>${surProjets}`,
    champs: `<label class="champ"><span>${t("Name")}</span><input type="text" data-cact="nom" value="${v(c.nom)}" spellcheck="false"></label>
      <label class="champ"><span>${t("When the lead should call it")}</span><textarea data-cact="description" rows="2">${v(c.description)}</textarea></label>
      <label class="champ"><span>${t("Its instructions")}</span><textarea data-cact="consignes" rows="8">${v(c.consignes)}</textarea></label>
      <label class="champ"><span>${t("Model")}</span><select data-cact="modele">${MODELES_AGENT.map(([k, n]) => `<option value="${k}" ${k === (c.modele || "") ? "selected" : ""}>${t(n)}</option>`).join("")}</select></label>
      <label class="interrupteur m0"><input type="checkbox" data-cact="lecture_seule" ${c.lecture_seule ? "checked" : ""}><span>${t("Read only: it reads and reports, never writes a file")}</span></label>`,
  };
  return {
    vue: `${c.description ? `<div class="petit">${h(c.description)}</div>` : ""}${c.source ? `<div class="petit muet">${t("Source: {0}", [`<code class="croit">${v(c.source)}</code>`])}</div>` : ""}
      ${(c.fichiers || []).length ? `<div class="petit muet">${t("{0}: {1}", [t("MCP server written by Claude"), ""])}${c.fichiers.map((f) => `<code>${h(f)}</code>`).join(", ")}</div>` : ""}${surProjets}
      <label class="interrupteur m0 attention" data-cact-confirme ${c.windows ? "" : "hidden"}><input type="checkbox" data-cact="confirme_windows" ${c.confirme_windows ? "checked" : ""}><span>${t("I understand: this server runs on Windows, outside the cage, with my rights; it can read and change my files and run programs.")}</span></label>`,
    champs: `<label class="champ"><span>${t("Name")}</span><input type="text" data-cact="nom" value="${v(c.nom)}" spellcheck="false"></label>
      <label class="champ"><span>${t("What it lets the pilot do")}</span><textarea data-cact="description" rows="2">${v(c.description)}</textarea></label>
      <label class="champ"><span>${t("Source")}</span><input type="text" data-cact="source" value="${v(c.source)}" spellcheck="false"></label>
      <label class="champ"><span>${t("Configuration")}</span><textarea data-cact="config" rows="8" class="code" spellcheck="false">${v(c.config)}</textarea></label>
      <label class="interrupteur m0"><input type="checkbox" data-cact="windows" ${c.windows ? "checked" : ""}><span>${t("Windows program: runs on Windows, outside the cage, with your rights")}</span></label>`,
  };
}
function carteActionHub(c, i) {
  const v = (x) => h(String(x || "")), ty = c.type;
  const projets = (etat.mentionsSugg || []).filter((m) => m.type === "projet" && !m.detail).map((m) => m.nom);
  if (c.projet && !projets.includes(c.projet)) projets.unshift(c.projet);
  const choixProjet = `<label class="champ"><span>${t("Project")}</span><select data-cact="projet">${projets.map((n) => `<option ${n === c.projet ? "selected" : ""}>${h(n)}</option>`).join("")}</select></label>`;
  const champs = ty === "projet" ? `<label class="champ"><span>${t("Name")}</span><input type="text" data-cact="projet" value="${v(c.projet)}"></label>
      <label class="champ"><span>${t("Folder")}</span><input type="text" data-cact="dossier" value="${v(c.dossier)}" spellcheck="false"></label>`
    : ty === "mission" ? "" : choixProjet;
  const equipe = ["agent", "mcp", "attribuer"].includes(ty) ? champsEquipeHub(c, [...new Set([...(c.projets || []), ...projets.filter((n) => n && n !== c.projet)])]) : null;
  const titre = ty === "demande" ? `<label class="champ"><span>${t("Title")}</span><input type="text" data-cact="titre" value="${v(c.titre)}"></label>` : "";
  const texte = ty === "pilote" || equipe ? "" : `<label class="champ"><span>${t("Text")}</span><textarea data-cact="texte" rows="5">${v(c.texte)}</textarea></label>`;
  const decide = c.etat ? `<div class="${c.etat === "lancee" ? "ok" : "muet"} petit" role="status">${c.etat === "lancee" ? "✔ " + t("Launched") : "✕ " + t("Refused")}${c.resultat ? " — " + h(t(c.resultat, c.args || [])) : ""}</div>`
    : `${c.erreur ? `<div class="attention petit" role="alert">${h(t(c.erreur))}</div>` : ""}
      ${equipe ? equipe.vue : ""}<div class="carte-action-champs" hidden>${equipe ? equipe.champs : champs + titre + texte}</div>
      <div class="ligne m0 mt-1"><button type="button" class="btn btn-petit btn-primaire" data-cact-decision="lancer" data-index="${i}">${t("Launch")}</button>
        ${ty === "attribuer" ? "" : `<button type="button" class="btn btn-petit" data-cact-modifier aria-expanded="false">${t("Edit")}</button>`}
        <button type="button" class="btn btn-petit" data-cact-decision="refuser" data-index="${i}">${t("Refuse")}</button></div>`;
  return `<div class="chat-msg chat-claude"><div class="chat-bulle chat-note carte-action" data-carte-action="${i}" data-type="${v(ty)}" ${ty === "attribuer" ? `data-element="${v(c.element)}" data-nom="${v(c.nom)}"` : ""}>
    <div class="muet petit">${t("Action proposed: nothing runs before you launch it")}</div>
    <div><b>${ICONES_ACTION[ty] || "•"} ${h(t(c.quoi || "Action"))}</b></div>
    <div class="petit">${t("{0}: {1}", [t("On"), `<code class="croit">${v(c.sur)}</code>`])}</div>
    ${c.titre ? `<div><b>${v(c.titre)}</b></div>` : ""}${c.texte ? `<div class="petit">${mdChat(c.texte)}</div>` : ""}
    <div class="petit muet">${t("{0}: {1}", [t("With which accesses"), ""])}</div><ul class="petit m0">${(c.acces || []).map((a) => `<li>${h(t(a))}</li>`).join("")}</ul>
    ${decide}</div></div>`;
}
document.addEventListener("click", async (ev) => {
  const m = ev.target.closest("[data-cact-modifier]");
  if (m) {
    const z = m.closest("[data-carte-action]").querySelector(".carte-action-champs");
    z.hidden = !z.hidden; m.setAttribute("aria-expanded", String(!z.hidden));
    if (!z.hidden) { const f = z.querySelector("input, select, textarea"); if (f) f.focus(); }
    return;
  }
  const b = ev.target.closest("[data-cact-decision]"); if (!b) return;
  const carte = b.closest("[data-carte-action]"), a = { type: carte.dataset.type };
  carte.querySelectorAll("[data-cact]").forEach((x) => { a[x.dataset.cact] = x.type === "checkbox" ? x.checked : x.value; });
  if (carte.querySelector(".carte-action-projets")) a.projets = [...carte.querySelectorAll("[data-cact-projet]:checked")].map((x) => x.value);
  if (a.type === "attribuer") { a.element = carte.dataset.element; a.nom = carte.dataset.nom; }
  b.disabled = true;
  const r = await action(api("hub/chat/action", "POST", { index: Number(b.dataset.index), decision: b.dataset.cactDecision, action: a }));
  b.disabled = false;
  if (r) majHubChat(true);
});
// a server set as a Windows program asks its own confirmation, shown as soon as the box is ticked
document.addEventListener("change", (ev) => {
  if (ev.target.dataset?.cact !== "windows") return;
  const z = ev.target.closest("[data-carte-action]")?.querySelector("[data-cact-confirme]");
  if (z) { z.hidden = !ev.target.checked; if (!ev.target.checked) z.querySelector("input").checked = false; }
});
const POUVOIRS_HUB = [["internet", "🌐", "Internet", "Searches and reads the web when a message needs it."],
  ["ecriture", "✏️", "Writing", "Changes files, only in the folders you point to it with read and write, never elsewhere."]];
const resumePouvoirs = (pv) => [t("Reads and answers"), ...POUVOIRS_HUB.filter(([k]) => pv[k]).map(([, i, n]) => i + " " + t(n)),
  ...((pv.dossiers || []).length ? ["📁 " + t("{0} folder(s)", [pv.dossiers.length])] : [])].join(" · ");
// M196: the folders pointed (checked by the server, hub_chat_dossiers.go), each read only or read and write
function listeDossiersHub(pv) {
  const l = pv.dossiers || [];
  return l.length ? l.map((d, i) => `<li><code class="croit">${h(d.chemin)}</code>
    <select data-dossier-acces="${i}" aria-label="${h(t("Access to {0}", [d.chemin]))}"><option value="">${t("Read only")}</option><option value="1" ${d.ecriture ? "selected" : ""}>${t("Read and write")}</option></select>
    <button type="button" class="btn-lien" data-dossier-retirer="${i}" title="${h(t("Remove"))}" aria-label="${h(t("Remove {0}", [d.chemin]))}">✕</button></li>`).join("")
    : `<li class="muet petit">${t("No folder: the assistant sees no file of your computer.")}</li>`;
}
function pouvoirsHub(pv) {
  return `<details class="hub-pouvoirs" id="hub-pouvoirs"><summary>${t("{0}: {1}", [t("What the assistant can do"), `<span id="hub-pouvoirs-resume">${h(resumePouvoirs(pv))}</span>`]).replace(" :", " :")}</summary>
    ${POUVOIRS_HUB.map(([k, i, n, aide]) => `<label class="interrupteur m0 local-seul"><input type="checkbox" data-pouvoir="${k}" ${pv[k] ? "checked" : ""} aria-describedby="pv-aide-${k}"><span>${i} ${t(n)}</span></label>
    <p class="petit muet m0 local-seul" id="pv-aide-${k}">${t(aide)}</p>`).join("")}
    <div class="hub-dossiers"><div class="ligne m0"><strong>📁 ${t("Folders")}</strong><span class="espace"></span><button type="button" class="btn btn-petit local-seul" id="hub-dossier-ajouter">${t("Add a folder…")}</button></div>
      <p class="petit muet m0" id="pv-aide-dossiers">${t("The assistant reads only the folders you point here; with Writing on, it changes files only in those set to “read and write”. Drop a folder here to add it.")}</p>
      <ul class="hub-dossiers-liste" id="hub-dossiers-liste" aria-describedby="pv-aide-dossiers">${listeDossiersHub(pv)}</ul></div></details>`;
}
async function changerPouvoirs(change) {
  await action(api("hub/chat/pouvoirs", "POST", { ...((etat.hubChat || {}).pouvoirs || {}), ...change }), "");
  majHubChat(true);
}
// a folder from the system's dialog (or Athanor's picker); a dropped folder: the page cannot see its path, the
// dialog opens to confirm it
async function ajouterDossierHub(nomGlisse) {
  const titre = nomGlisse ? t("Choose the folder {0} to confirm it", [nomGlisse]) : t("Choose a folder for the assistant");
  let r = null;
  if (!(etat.vue || {}).distant) r = await api("dossiers/choisir", "POST", { depart: "", titre }).catch((e) => { toast(e.message, "erreur"); return { annule: true }; });
  if (!r || r.indisponible) r = await selecteurDossier("", titre);
  if (!r || !r.chemin) return;
  changerPouvoirs({ dossiers: [...(((etat.hubChat || {}).pouvoirs || {}).dossiers || []), { chemin: r.chemin, ecriture: false }] });
}
// attached files of the home conversation: images, PDF and text, 20 MB at most each (hub_chat_dossiers.go)
const TYPES_PIECE_HUB = /\.(png|jpe?g|gif|webp|pdf|txt|md|csv|tsv|json|ya?ml|xml|toml|ini|log|html|css|js|ts|go|py|sql)$/i, TAILLE_PIECE_HUB = 20 * 1024 * 1024;
function ajouterPiecesHub(fichiers) {
  const l = etat.pieces.hc = etat.pieces.hc || [];
  for (const f of fichiers) {
    if (!TYPES_PIECE_HUB.test(f.name)) { toast(t("{0}: this type of file is not accepted (images, PDF and text files only).", [f.name]), "erreur"); continue; }
    if (f.size > TAILLE_PIECE_HUB) { toast(t("{0} is too large (20 MB at most).", [f.name]), "erreur"); continue; }
    if (!l.some((x) => x.name === f.name && x.size === f.size)) l.push(f);
  }
  const ul = $("#hc-liste"); if (ul) ul.innerHTML = listePieces("hc");
}
async function majHubChat(force) {
  if (etat.page !== "accueil") return;
  const d = await api("hub/chat").catch(() => null); if (!d || etat.page !== "accueil") return;
  etat.hubChat = d;
  clearTimeout(minuterieHubChat);
  if (d.en_cours) minuterieHubChat = setTimeout(() => majHubChat(false), 1500);
  const sig = JSON.stringify([d.messages.length, d.en_cours, d.partiel, d.pouvoirs, d.actions]);
  if (!force && sig === etat.hubChatSig) return;
  etat.hubChatSig = sig;
  const fil = $("#hub-chat-fil"); if (!fil) return;
  const enBas = fil.scrollHeight - fil.scrollTop - fil.clientHeight < 80;
  fil.innerHTML = filHub(d);
  if (enBas || force) fil.scrollTop = fil.scrollHeight;
  const pv = d.pouvoirs || {};
  document.querySelectorAll("[data-pouvoir]").forEach((c) => { c.checked = !!pv[c.dataset.pouvoir]; });
  const r = $("#hub-pouvoirs-resume"); if (r) r.textContent = resumePouvoirs(pv);
  const ld = $("#hub-dossiers-liste"); if (ld) ld.innerHTML = listeDossiersHub(pv);
  const b = $("#hub-chat-envoyer"), st = $("#hub-chat-stop");
  if (b) b.hidden = !!d.en_cours;
  if (st) st.hidden = !d.en_cours;
}
async function envoyerPieceHub(f) {
  const r = await fetch("/api/hub/chat/piece", { method: "POST", headers: { "X-Athanor": CSRF, "X-Nom-Fichier": encodeURIComponent(f.name) }, body: f });
  const d = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(d.erreur_key ? t(d.erreur_key, d.args || []) : t("Error {0}", [r.status]));
  return d;
}
async function envoyerHubChat() {
  const champ = $("#accueil-texte"); if (!champ || etat.hubChatEnvoi) return;
  const texte = champ.value.trim(), ments = etat.accueilMentions || [], fichiers = etat.pieces.hc || [];
  if (!texte && !fichiers.length) return;
  etat.hubChatEnvoi = true; // a double Enter sends once
  const message = ments.length ? texte + "\n\n" + ments.map((m) => "@" + m.nom).join(" ") : texte;
  const pieces = []; // the files first, then the message that names them; one that fails stops the sending
  for (const f of fichiers) {
    try { pieces.push((await envoyerPieceHub(f)).chemin); } catch (e) { toast(t("{0} could not be attached: {1}", [f.name, e.message]), "erreur"); etat.hubChatEnvoi = false; return; }
  }
  const r = await action(api("hub/chat", "POST", pieces.length ? { message, pieces } : { message }), "").finally(() => { etat.hubChatEnvoi = false; });
  if (!r) return;
  etat.accueilTexte = ""; etat.accueilMentions = []; champ.value = ""; majMentions();
  etat.pieces.hc = []; const ul = $("#hc-liste"); if (ul) ul.innerHTML = "";
  majHubChat(true);
}
document.addEventListener("keydown", (ev) => {
  // Enter sends (Shift+Enter: a new line; on a phone Enter is a new line); with the mentions list open, it chooses
  if (ev.target.id !== "accueil-texte" || ev.key !== "Enter" || ev.defaultPrevented || ev.shiftKey || ev.isComposing || etat.mentionListe || tactile()) return;
  ev.preventDefault(); envoyerHubChat();
});
document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("#hub-chat-aide, #accueil-mission, #hub-chat-stop, #hub-chat-nouveau, #hub-dossier-ajouter, [data-dossier-retirer]"); if (!b) return;
  if (b.id === "hub-chat-aide") return lancerTourChat();
  if (b.id === "accueil-mission") return envoyerAccueil();
  if (b.id === "hub-dossier-ajouter") return ajouterDossierHub();
  if (b.dataset.dossierRetirer) return changerPouvoirs({ dossiers: (etat.hubChat.pouvoirs.dossiers || []).filter((x, i) => i !== +b.dataset.dossierRetirer) });
  if (b.id === "hub-chat-stop") { await action(api("hub/chat/arreter", "POST")); return majHubChat(true); }
  if (await confirmer(t("Start a new conversation? The assistant forgets this one, and its powers are switched off."), { ok: t("New conversation") })) { await action(api("hub/chat/effacer", "POST")); majHubChat(true); }
});
document.addEventListener("change", async (ev) => {
  if (ev.target.matches("#hub-pouvoirs [data-dossier-acces]")) return changerPouvoirs({ dossiers: (etat.hubChat.pouvoirs.dossiers || []).map((x, i) => i === +ev.target.dataset.dossierAcces ? { ...x, ecriture: !!ev.target.value } : x) });
  if (ev.target.matches("#hub-pouvoirs [data-pouvoir]")) changerPouvoirs({ [ev.target.dataset.pouvoir]: ev.target.checked });
});
document.addEventListener("input", (ev) => { if (ev.target.id === "accueil-texte") { etat.accueilTexte = ev.target.value; chercherMentions(ev.target); } });

pages.suivi = async () => {
  const v = etat.vue; if (!v) return "";
  // a computer joining a team from a new folder: the switch is there before any request (the team's comes)
  if (!v.kit_installe || !v.projet || v.demande_vide) return rendreGuide(v) + (v.kit_installe && v.projet ? rendreBlocPCs(pcsDuProjet(v.projet.nom)) : "");
  const e = v.etat || {};
  const plan = v.plan || [];
  const faits = plan.filter((j) => j.fait).length;
  const stats = v.stats || [];
  const somme = (k) => stats.reduce((a, s) => a + (Number(s[k]) || 0), 0);
  const cache = somme("cache_lu"), plein = somme("entree_tokens") + somme("cache_ecrit");
  const ratio = cache + plein ? Math.round((cache / (cache + plein)) * 100) : 0;
  const g = v.gains;
  return `${bandeauRaccourci()}${v.pour_vous ? `<a class="bloc pv-bandeau" href="#pourvous">🙋 <strong>${t("Claude needs you")}</strong> · ${t("{0} task(s) on the For you page", [v.pour_vous])} →</a>` : ""}
  <div class="grille grille-2">
    <div class="colonne"><div class="bloc"><h2>${t("Right now")}</h2>
      ${v.actif && v.pilote_ancien ? `<p class="muet petit">ℹ ${v.version_pilote
        ? t("Athanor {0} is installed and the app already uses it. This pilot keeps running on {1}, the version it started with, until it is started again: it then moves to {0} by itself (to do it now: Stop after cycle, then Start). Nothing to do, nothing is lost.", [h(v.version), h(v.version_pilote)])
        : t("Athanor {0} is installed and the app already uses it. This pilot keeps running on the version it started with until it is started again: it then moves to {0} by itself (to do it now: Stop after cycle, then Start). Nothing to do, nothing is lost.", [h(v.version)])}</p>` : ""}
      <dl class="fiche">
        <dt>${t("Milestone")}</dt><dd>${e.jalon_en_cours ? `<b>${md(titreJalon(e.jalon_en_cours))}</b>${(() => { const j = plan.find((x) => x.titre === titreJalon(e.jalon_en_cours)); return j && j.detail ? `<div class="muet petit">${md(j.detail)}</div>` : ""; })()}` : "—"}</dd>
        <dt>${t("Last commit, this computer")}</dt><dd class="muet">${h(e.dernier_commit || "—")}</dd>
        ${ligneGitHubSuivi(v.github, e.dernier_commit)}
        ${v.relais ? `<dt>${t("Dev after milestones")}</dt><dd class="relais-suivi">${ligneRelais(v.relais)}</dd>` : ""}
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
    ${v.depense ? rendreDepense(v.depense) : rendreQuota(v.quota, v.quota_arret)}</div>
    <div class="bloc bloc-plan"><div class="bloc-tete"><h2>${t("Project plan")}</h2>${plan.length ? boutonDefil("plan") : ""}</div>
      ${plan.length ? `<div class="barre"><i style="width:${(faits / plan.length) * 100}%"></i></div><div class="muet petit">${texteEtape(v.projet)}${t("{0} / {1} milestones done", [faits, plan.length])}</div>
      <ul class="plan">${listePlan(plan, false)}</ul>`
        : `<div class="vide">${t("No plan yet: Claude writes it during the first cycle (framing).")}</div>`}
    </div>
  </div>
  ${rendreBlocPCs(pcsDuProjet(v.projet.nom))}
  ${(pcsDuProjet(v.projet.nom) || {}).actif ? blocDirect(v.projet.nom) : ""}
  ${rendreAvancement(stats, plan, faits, v.actif)}
  <div class="bloc"><h2>${t("Usage")}</h2>
    ${stats.length ? `<div class="chiffres">
      <div class="chiffre"><b>${stats.filter((s) => !String(s.cycle).includes("·")).length}</b><span>${t("cycles")}</span></div>
      <div class="chiffre"><b>${(() => { const m = Math.round(somme("minutes")); return `${Math.floor(m / 60)} h ${String(m % 60).padStart(2, "0")}`; })()}</b><span>${t("of work")}</span></div>
      <div class="chiffre"><b>${somme("tours")}</b><span>${t("turns")}</span></div>
      <div class="chiffre" title="${h(t("at list price (your subscription quota pays)"))}"><b>${stats.some((s) => s.estime) ? "≈ " : ""}${somme("cout_usd").toFixed(2)} $</b><span>${t("at list price")}</span></div>
      <div class="chiffre" title="${h(t("Claude's own prompt cache (Anthropic's, always on), over all the cycles: not a saving of an Athanor option. At list prices a token read again from the cache costs a tenth, one written to it 1.25 times: what Claude reads cost this much less than without the cache."))}"><b>${ratio} %</b><span>${t("read from Claude's own cache")}${g && g.cache_pct > 0 ? `<em class="cache-claude muet">${t("reading −{0} %", [g.cache_pct])}</em>` : ""}</span></div>
      ${g && g.rtk_entree > 0 ? `<div class="chiffre" title="${h(t("Measured by RTK: the tokens of the command outputs (git, ls, grep, tests…) it did not pass on to Claude."))}"><b>${fmtTokens(g.rtk_economise)}</b><span>${t("tokens spared by RTK")}<em class="gain">${t("−{0} % of the command outputs", [g.rtk_pct])}</em></span></div>` : ""}
      <div class="chiffre" title="${h(t("What Claude read at each turn: the whole conversation of the cycle again (the code, the plan, the tools' results), mostly from its cache. Most of the cost is there: what Claude writes is a small part."))}"><b>${fmtTokens(somme("entree_tokens") + somme("cache_lu") + somme("cache_ecrit"))}</b><span>${t("tokens read")}</span></div>
      <div class="chiffre" title="${h(t("What Claude wrote: its answers, the code, the tools it called. Each token written costs more than one read, but there are far fewer of them."))}"><b>${fmtTokens(somme("sortie_tokens"))}</b><span>${t("tokens produced")}</span></div>
      <div class="chiffre"><b>${fmtTokens(somme("contexte_max") / stats.length)}</b><span>${t("average peak context")}</span></div>
    </div>
    ${lignesGains(g)}
    <div class="tableau mt-2"><table class="tab"><thead><tr><th>${t("Cycle")}</th><th>${t("Time")}</th><th>${t("Model")}</th><th>${t("State")}</th><th>${t("Minutes")}</th><th>${t("Turns")}</th><th>${t("Cost $")}</th><th>${t("Peak context")}</th></tr></thead><tbody>
    ${stats.slice(etat.ouverts.has("stats:tous") ? -200 : -5).reverse().map((s) => `<tr><td>${s.cycle}</td><td>${h((s.horodatage || "").slice(11, 16))}</td><td>${h(s.template)}</td><td class="${h(s.etat)}">${h(s.etat)}</td><td>${Math.round(s.minutes)}</td><td>${s.tours}</td><td${s.estime ? ` title="${h(t("≈: estimated from the turns of the lead and its sub-agents (cycle closed by the pilot); ?: price of this model not learned yet"))}"` : ""}>${s.estime && !Number(s.cout_usd) && s.tours ? "?" : (s.estime ? "≈ " : "") + Number(s.cout_usd).toFixed(2)}</td><td>${fmtTokens(s.contexte_max)}</td></tr>`).join("")}
    </tbody></table></div>
    ${stats.length > 5 ? `<button type="button" class="btn btn-petit mt-1" data-stats-tous>${etat.ouverts.has("stats:tous") ? t("Last 5 cycles only") : t("All {0} cycles", [stats.length])}</button>` : ""}` : `<div class="vide">${t("Numbers appear at the end of the first cycle.")}</div>`}
  </div>`;
};

// M136: the dev deployment after the last milestone (relais_jalon.go)
function ligneRelais(r) {
  const [icone, etiquette] = r.etat === "attente" ? ["⏳", "Waiting for the milestone's commit on GitHub"] : r.etat === "lance" ? ETATS_DEPLOI.en_cours : ETATS_DEPLOI[r.etat] || ["", r.etat];
  return `<span class="badge">${icone} ${h(t(etiquette))}</span> <code>${h((r.commit || "").slice(0, 7))}</code> <span class="muet petit">${md(r.jalon)} · ${h(dateLisible(r.date))}</span>${r.erreur ? `<div class="petit attention">${h(t(r.erreur))}</div>` : ""} <a class="petit" href="#acces">${t("Environments")}</a>`;
}

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
    texte: "Claude Code and Homebrew's packages, installed in a Terminal window (it asks for your password once). Claude works in macOS' own sandbox (Seatbelt): it only writes in the project." }];
  return [{ id: "linux", nom: "Linux", actuel: true,
    texte: "Claude Code, the system sandbox (bubblewrap) and the tools, in a terminal; it asks for your password once." }];
}

// --- the shortcut to the installed Athanor (raccourci.go): the downloaded file installs it, the shortcut opens it
etat.raccourci = null; etat.raccourciLu = false; etat.raccourciPlusTard = false; etat.raccourciErreur = "";
function raccourciConnu() {
  if (etat.raccourci || etat.raccourciLu || etat.raccourciErreur) return etat.raccourci; // after an error: only "Try again" asks again
  etat.raccourciLu = true; etat.raccourciErreur = "";
  api("raccourci").then((r) => {
    etat.raccourci = r;
    if (etat.assistant >= 0) rendreAssistant();
    const l = r.lancement;
    if (l && l.ancien) toast(t("The file you opened is Athanor {0}, older than the installed one ({1}): nothing was replaced, the installed Athanor opened.", [l.ancien, (etat.vue || {}).version || ""]), "ok");
    if (l && l.mis_a_jour) toast(t("Athanor updated from {0} to {1}.", [l.mis_a_jour, (etat.vue || {}).version || ""]), "ok");
    if (l && (l.repares || []).length) toast(t("Same version opened again: {0} damaged file(s) of the installed Athanor put back.", [l.repares.length]), "ok");
    if (["suivi", "reglages"].includes(etat.page)) rendrePage();
  }).catch((e) => { // said, with a button to ask again (never "loading" for ever)
    etat.raccourciErreur = (e && e.message) || String(e); etat.raccourciLu = false;
    if (etat.assistant >= 0) rendreAssistant();
    if (etat.page === "reglages") rendrePage();
  });
  return null;
}
// attenteRaccourci: while the shortcut's details are being read, or why they could not be
function attenteRaccourci() {
  if (!etat.raccourciErreur) return `<p class="muet petit">${t("Loading…")}</p>`;
  return `<p class="erreur petit">${h(t("The shortcut's details could not be read: {0}", [etat.raccourciErreur]))}</p><button class="btn" data-raccourci="relire">${t("Try again")}</button>`;
}
function nomRaccourci(f) { return String(f).split(/[\\/]/).pop(); }
// blocRaccourci: the folder (proposed: the Desktop, the Mac's Applications) and the button
function blocRaccourci(r, titre, plus, champ = "rc-dossier") {
  if (!r) return "";
  const faits = r.raccourcis || [];
  const explication = r.systeme === "darwin" ? t("An Athanor.app that opens the installed Athanor: put it in Applications or on the Dock.")
    : r.systeme === "windows" ? t("An Athanor shortcut that opens the installed Athanor (the copy kept in Athanor's folder, which updates itself), also added to the Start menu: the downloaded file can then be deleted.")
    : t("A launcher that opens the installed Athanor (also added to the applications menu): the downloaded file can then be deleted.");
  return `${titre ? `<h2>${t("Shortcut")}</h2>` : ""}<p class="muet petit m0">${explication}</p>
    ${faits.map((f) => `<p class="ok petit m0">✔ <code title="${h(f)}">${h(nomRaccourci(f))}</code> <span class="muet">${h(f.replace(/[\\/][^\\/]*$/, ""))}</span></p>`).join("")}
    <label class="champ mt-1"><span>${t("Folder of the shortcut")}</span>${champDossier(champ, r.defaut || "", "", false, t("Choose the shortcut's folder"))}</label>
    <div class="ligne mt-1"><button class="btn ${faits.length ? "" : "btn-primaire"}" data-raccourci="creer" data-champ="${champ}">${faits.length ? t("Make another shortcut") : t("Create the shortcut")}</button>${plus || ""}</div>`;
}
document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-raccourci]"); if (!b) return;
  if (b.dataset.raccourci === "plus-tard") { etat.raccourciPlusTard = true; rendrePage(); return; }
  if (b.dataset.raccourci === "relire") { etat.raccourciErreur = ""; raccourciConnu(); if (etat.assistant >= 0) rendreAssistant(); else rendrePage(); return; }
  b.disabled = true;
  const r = await action(api("raccourci", "POST", { dossier: ($("#" + (b.dataset.champ || "rc-dossier")) || {}).value || "" }));
  b.disabled = false;
  if (r) { etat.raccourci = Object.assign({}, r.raccourci, { lancement: (etat.raccourci || {}).lancement }); toast(t("Shortcut made: {0}", [(r.crees || []).join(", ")]), "ok"); if (etat.assistant >= 0) rendreAssistant(); rendrePage(); }
});
// the downloaded file opened this window: a shortcut to the installed Athanor is offered, once
function bandeauRaccourci() {
  const r = raccourciConnu();
  if (!r || !r.lancement || !r.lancement.telechargement || (r.raccourcis || []).length || etat.raccourciPlusTard) return "";
  return `<div class="bloc bandeau-raccourci"><h2>📌 ${t("Open Athanor directly next time")}</h2>
    <p class="petit">${h(t("You opened Athanor from the file you downloaded ({0}). Athanor is installed in its own folder and updates itself there: create a shortcut to open it, and that file is no longer needed.", [nomRaccourci(r.lancement.fichier || "")]))}</p>
    ${blocRaccourci(r, false, `<button class="btn" data-raccourci="plus-tard">${t("Not now")}</button>`)}</div>`;
}

function rendreGuide(v) {
  const e1 = v.kit_installe, e2 = !!v.projet, e3 = e2 && !v.demande_vide;
  // the tooling of this system: on Windows, WSL (Claude in a sandbox) or native Windows, chosen here
  const outillage = v.systeme === "darwin" ? "Claude Code and Homebrew's packages, installed in a Terminal window (it asks for your password once). Claude works in macOS' own sandbox (Seatbelt): it only writes in the project."
    : !v.windows ? "Claude Code, the system sandbox (bubblewrap) and the tools, in a terminal; it asks for your password once."
    : v.natif ? "Native Windows: Git for Windows, jq, Python, Node.js and Claude Code for Windows, without WSL. Claude has no sandbox in this mode."
    : "WSL2 (the Linux built into Windows), Claude Code, the system sandbox. A window opens; Linux asks for a password once.";
  return `${bandeauRaccourci()}<div class="bloc"><h2>${t("First steps")}</h2><p class="intro">${t("Five steps, all from this window. Then you can let it run for days, on several projects at once.")}</p>
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
  </div>
  <h3 class="titre-plus-loin">${t("Going further")}</h3>
  <div class="cartes-guide">${["github", "distance", "pcs"].map((id) => { const g = GUIDE.find((x) => x.id === id); return `<a class="carte-guide" href="#guide" data-guide-sujet="${id}"><b>${t(g.titre)}</b><span class="muet petit">${mdGuide(t(g.intro))}</span><span class="lien-guide">${t("Read the guide")} →</span></a>`; }).join("")}</div>
  <p class="muet petit mt-2">${t("The Guide, behind the “?” at the top of the window, keeps every step of these tutorials.")}</p></div>`;
}

// --- the Guide: short step-by-step tutorials (starting, GitHub, the phone, several computers), always in
// the menu; the first steps, the Overview and the Remote page link to them
const GUIDE = [
  { id: "debut", titre: "Start a project", intro: "Athanor builds a whole project with Claude, milestone after milestone, while you do something else.",
    etapes: [
      ["Install and check", "*Setup & health*: *Install everything* once, then *Check everything*. Each problem has its own *Repair* or *Ask Claude* button.", "#sante", "Open the check"],
      ["Add the project", "In the project picker, *New project*: a name, its folder (new or existing, anywhere on the computer) and what you want.", "#nouveau", "Add a project"],
      ["Say what you want", "*Requests*: describe the result you expect and attach what helps (mock-ups, a specification, photos). *Improve with Claude* makes it clearer. Technologies are free unless you impose some.", "#demandes", "Requests"],
      ["Start", "*Start*, at the top of the window. Claude first writes the plan (the framing), then builds and checks one milestone per cycle."],
      ["Follow and answer", "*Overview* shows the milestone in progress, the plan and your quota. *For you* lists what only you can do (create an account, give a key, read a code received by SMS): answer there, Claude checks it.", "#pourvous", "For you"],
      ["Add along the way", "A new request waits in the queue and joins the plan at the next cycle. *Chat* answers your questions about the project at any time."],
    ],
    note: "*Stop after cycle* lets the current milestone finish, *Stop now* cuts it. Everything is kept in files: the pilot resumes where it stopped, even after the computer restarts." },
  { id: "hub", titre: "Finding your way", intro: "Athanor opens on its home page: what is going on and what needs you, at a glance. Each part of Athanor is a module with its own entry in the menu: for now *Studio*, the projects Claude builds, and the *Vault*, your keys.",
    etapes: [
      ["The menu", "On the left: *Home*, *Projects*, then one entry per module (*Studio*, *Vault*), and *Settings* at the bottom. A module's entry opens the page you last saw in it. On a phone, the ☰ button opens the menu, and a tap on an entry closes it.", "#accueil", "Home"],
      ["The home page", "*Home*: one tile per module, with its state (working, needs attention, idle) and its figures; a click on a figure opens its page, *Open* opens the module. Below, *In progress* shows what waits for you first, then what is working; *See all* opens *Projects*.", "#accueil", "Home"],
      ["The assistant", "The home page opens on a conversation: ask a question, or say what you want done. *Guide › Talk to the home assistant* explains what it reads, what it can do and its cards.", "#accueil", "Home"],
      ["Projects", "*Projects* lists everything under way in every module: its state, its progress, what it is doing now (“Milestone 5/14: …”) and what waits for you, with the reason. The filters show everything, only what needs you or only what is in progress; a click on a line opens its page. *New project* and *Join a project* are at the top.", "#global", "Projects"],
      ["The tabs", "At the top of a module, one line of tabs holds its pages. *Studio*: the project picker, then *Overview*, *Team*, *Requests*, *Chat*, *For you*, *Access*, *Log* and *Manage*. *Settings*: *Settings*, *Skills & MCP*, *Setup & health* and *Remote*. On a phone, the line scrolls sideways.", "#suivi", "Overview"],
      ["The bell", "The bell, at the top of the window, gathers what needs you in every module, one line each with its reason (“Studio, my-shop: …”). *View* opens the right page. *Ignore* hides a line and deletes nothing: it waits under *Ignored alerts*, where *Reopen* brings it back, and it comes back by itself when its reason changes."],
      ["The “?” and the tutorials", "The *?* beside the bell opens this Guide. Home, Studio and the Vault each have a short tutorial, shown on your first visit (the Vault's also opens from a project's *Access* tab). *Tutorials*, at the top of the Guide, replays any of them; *Settings › Options* › *Show the first-launch guide and the tutorials again* shows them all once more."],
      ["Missions", "The *Missions* block, at the bottom of the home page, shows the last three missions with their state and their lanes; *See all* opens the *Missions* page. *Guide › Launch a mission* walks through one.", "#missions", "Missions"],
      ["In a terminal", "`./athanor tui` opens on the same home page: one block per module, what needs you and the missions. *H* or *Esc* comes back to it from any page, *Enter* opens the module, *1*–*9* and *0* still open Studio's pages."],
    ],
    note: "The update moved none of your projects, settings or keys, and old links and notifications still open the same pages as before." },
  { id: "assistant", titre: "Talk to the home assistant", intro: "The home page is a conversation with an assistant that knows Athanor: ask a question, or say what you want done, in your words. It reads, answers and proposes; you decide.",
    etapes: [
      ["Ask", "Write in the big field of the home page: *Enter* sends, *Shift+Enter* adds a line. The answer comes above, after the earlier messages. *New conversation* starts again: the assistant forgets this one and its powers are switched off.", "#accueil", "Home"],
      ["What it reads", "Only what you give it: your messages, the files you attach (📎, or drop or paste them: images, PDF and text files, 20 MB at most each) and the folders you point under *What the assistant can do* › *Folders*, each read only or read and write. *@* names a project, a server or a vault key."],
      ["Its powers", "*What the assistant can do* stays visible under the field. *Internet* lets it search and read the web; *Writing* lets it change files, only in the folders set to read and write. Both are off by default, and off again in each new conversation. It never runs a command, never uses an MCP server and never reaches a server by itself."],
      ["What it never sees", "The vault's values, secrets files (.env), Athanor's own folders and system folders are never given to it, and a vault value pasted in a message or a file is masked before it reads it. A file or a web page that gives it orders is data: the assistant tells you what it asks and does not follow it."],
      ["Cards", "To act, it proposes a card: create a project and start its pilot, start a pilot, add a request, leave a remark, prepare a mission, create a sub-agent or an MCP server, switch one on in projects. The card says what, on what and with which accesses: *Launch*, *Change* or *Refuse*. Nothing runs before you press *Launch*, and each decision goes into the audit log."],
      ["Missions", "*Create a mission* sends your sentence to the orchestrator, which prepares its own card under the conversation: lanes, environments, accesses, approvals, estimate. *Guide › Launch a mission* walks through one.", "#accueil", "Home"],
      ["From a phone", "On a paired device the conversation is read only: you read it and its powers, and you write to the assistant on the computer. Approvals and the emergency stop still work from the phone. The *?* at the top of the conversation replays its short tutorial."],
    ] },
  { id: "coffre", titre: "The vault", intro: "The vault keeps your keys, tokens and settings in one place, encrypted on the disk. A secret's value is shown only while you type it, never again, and no page or answer of Athanor contains it.",
    etapes: [
      ["Secrets and variables", "*Vault*, in the menu: *Secrets* hold what must stay hidden (a token, a password, a private key), *Variables* plain settings (a region, a user name), readable even while the vault is locked. *Add* creates one; the ⋯ menu of a card edits it, replaces its value or deletes it.", "#coffre", "Vault"],
      ["Paste", "*Paste* takes a whole `.env` block, an MCP configuration, a token or a private key. Athanor recognises each value (Cloudflare, GitHub, Anthropic, OpenAI, Stripe, AWS, SSH…) and proposes a name, a type and a scope; untick what you do not want to keep, then save. Only the names come back to the page, never the values."],
      ["Test", "*Test* checks a key with its own provider before you rely on it: Cloudflare and GitHub say whether it works and what it may do, an SSH key gives its type and whether its server answers. The key goes nowhere else."],
      ["The note for the AI", "After a test, Athanor proposes a short note from the result (the account, the zones, the rights). Change it as you like: it tells Claude what the key is for without showing it the value."],
      ["Two scopes: code and infra", "*code*: what a project's code uses (an API key, a test token). *infra*: the keys of your servers and accounts (Cloudflare, SSH, GitHub, a certificate, AWS). Athanor proposes the scope from the type, and you can change it. *infra* keys never leave Athanor and need a password for Athanor: without one, the infra scope stays greyed out.", "#options@bloc-verrou", "Settings › Options › Lock"],
      ["Locked, unlocked", "The *code* keys reopen by themselves when Athanor starts on this PC. The *infra* keys open when you unlock Athanor (in the window, or from a paired phone) and close again when you lock it (🔒), when the session ends or when Athanor stops. Locked, the page says so and offers *Unlock*. A paired phone sees the list, never a value, and cannot change it."],
      ["Your old keys come along", "On start-up, Athanor copies into the vault the keys it already had: each project's `.athanor/secrets.env`, the keys of *Skills & MCP* and those of its own `.env`. It reads those files without changing them, and starting again imports nothing twice. A line at the top of *Vault* gives the count: *Undo the import* removes exactly those entries, *Import again* brings them back."],
    ],
    note: "Your project keys reopen alone on this PC; the keys of your servers open only with your password, and a copy of the disk opens nothing." },
  { id: "acces", titre: "Project access", intro: "Choose which keys and settings of the vault each project gets, and for which environment. Athanor writes them for the pilot itself before each start: Claude never holds the vault.",
    etapes: [
      ["The Access panel", "*Access*, a tab of *Studio*: the vault's entries on the left, the project's environments (*dev*, *staging*, *prod*) on the right. Drag an entry onto an environment, or tick its box: the pilot gets it under the variable name shown, which you can change.", "#acces", "Access"],
      ["The environment", "Next to *Start*, a list chooses the environment given to the pilot (*dev* at first). Changing it rewrites the pilot's values at once, and the panel follows. On a paired phone, the environment and the accesses are shown, never changed."],
      ["The badge", "The 🔑 badge next to *Start* counts the project's accesses and opens the panel. The sentence beside it says what the pilot will get (“2 test keys, 1 variable”). *vault locked*: the pilot's file waits until you unlock Athanor."],
      ["Code or infra", "A *code* entry (a test key, a setting) goes to the pilot: only the entries of the current environment are written into the project's `.athanor/secrets.env`. An *infra* entry (Cloudflare, SSH…) can be linked to the project, but Athanor keeps it for its own actions and never gives it to the pilot. Removing an access takes its value out of the file at once."],
      ["Keys given on “For you”", "When Claude asks you for a key on *For you*, your answer goes into the vault (scope *code*) with an access for the current environment, marked *added automatically*. The keys of your projects' old files came in the same way: visible, and removable like the others.", "#pourvous", "For you"],
      ["Native Windows mode", "Without WSL, nothing separates Claude from the files of this PC: any agent could read them. Linking an *infra* entry is then refused, with an offer to use WSL. *Force, knowing the risk* allows it for one project after a confirmation, and the choice is logged. *code* entries work as usual."],
      ["Test the vault", "*Setup & health* › *Test the vault (2 min)*: a short Claude session tries to read three sample entries. It must see the granted one, and never the ungranted one nor the infra one. The samples are removed afterwards, whatever happens.", "#sante", "Setup & health"],
    ],
    note: "The badge before *Start* tells you exactly what the pilot will receive; a value never appears on a page, in the log or on the phone." },
  { id: "approbations", titre: "Approvals and audit log", intro: "While it works, the pilot can ask Athanor for a key it lacks instead of stopping. You decide from the bell, from *For you* or from your phone, every request and decision is written to a log you can check, and one button stops everything.",
    etapes: [
      ["When an agent asks", "The pilot asks with its hub tools, giving a reason. The request appears in the bell and at the top of *For you*: the project, the environment, the agent's reason and the time left. It waits 24 hours; past that, it is refused for the agent. The pilot carries on with the rest meanwhile.", "#pourvous", "For you"],
      ["Accept", "*Accept* asks which project key (*code*) of the vault to give, or *Add a key*, for which environment and under which variable. Athanor writes the value into the project's `.athanor/secrets.env` at once: the pilot reads it without restarting, and never sees the vault."],
      ["Refuse", "*Refuse*: the agent is told and does without the key. Its instructions forbid asking again for a key you refused."],
      ["Never ask again", "*Never ask again for this project and environment*: the request is accepted, and the next ones of the same kind for this project in this environment are accepted without asking you. Production and destructive actions always wait for you otherwise."],
      ["From your phone", "A paired phone gets a notification for each new request (“Studio, my-shop: access requested”) and can accept or refuse it on *For you*. The rest of the hub stays closed to it, and so does the agents' connection."],
      ["The audit log", "*Audit log*, in the menu: every call of an agent to the hub, every decision, every access granted or forced and the emergency stop, one line each, newest first. Filter by month, project or type, or search. A key's value never appears: the vault's values and the known key formats are masked before writing.", "#audit", "Audit log"],
      ["Check the log", "Each line carries the fingerprint (SHA-256) of the line before it. *Check the log* recomputes the whole chain: green, or the first line that was changed, removed or inserted. A paired phone can read the log and check it too."],
      ["Emergency stop", "The red *Emergency stop* button at the foot of the menu (also on a paired phone, or `athanor arret-urgence` in a terminal), after a confirmation: every pilot stops, every agent loses its access to the hub, the pending requests are refused and the vault closes. To resume, unlock Athanor with its password (without a password: *Resume* in the bell), then *Start* the projects."],
      ["An agent of this PC", "An agent runs programs as you do and can reach Athanor's window. So, in WSL mode as in native mode, deciding an approval, giving a key to a project, forcing infra entries, moving a key from infra to code, taking the prod role away from a server or removing a server asks for Athanor's password (a paired phone unlocked with it does not ask again). Choosing the first password and *Resume* after an emergency stop wait until no pilot, chat or test runs. A line of the log changed by hand still breaks the chain."],
    ],
    note: "An agent never decides for you: every request waits for your answer in the bell, on *For you* or on your phone, and leaves a line in the audit log." },
  { id: "serveurs", titre: "Add a server", intro: "Add your VPS or your home server: Athanor checks its fingerprint, connects with its own key kept in the vault, and reads what runs on it, in about a minute. A server's password is never kept.",
    etapes: [
      ["Before you start", "A server's key is an *infra* entry of the vault: it opens only with Athanor's password. Choose one, then unlock Athanor; otherwise the Servers page tells you. A server is added on the computer, not from a paired phone.", "#options@bloc-verrou", "Settings › Options › Lock"],
      ["The address", "*Servers*, in the menu, then *+ Add a server*: the address, the port (22 by default), the user (`root` or a user with `sudo`), a name if you like, and its roles: *dev* for tests and previews, *prod* for your live site. Then *Continue*.", "#serveurs", "Servers"],
      ["The fingerprint", "Athanor reads the fingerprint the server presents and shows it. Compare it with the one the server itself gives (`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub`, typed on the server or in your host's console), then tick *It is my server's fingerprint*. From then on Athanor refuses any other: if it changes, the connection is refused with a clear message."],
      ["Three ways to connect", "*A key of the vault*: an SSH key you already use for this server. *A new key made by Athanor*: copy its public part into the server's `~/.ssh/authorized_keys`, or run the command shown on the server, then *Verify*. *The server's password, once*: Athanor puts its own key there for you. In native Windows mode, giving an existing key of the vault asks for Athanor's password."],
      ["The password, never kept", "With the password, Athanor connects once, adds its own key (ed25519, kept in the vault) to the server, then connects again with that key to check it. The password is neither saved nor written to any page or log."],
      ["Cut password logins", "Athanor then offers *Cut password logins*, never on its own: nobody can then try passwords on the server. It writes one small SSH setting (`/etc/ssh/sshd_config.d/00-athanor.conf`), checks it with `sshd -t`, reloads SSH and connects again with its key; if anything fails, it removes the setting again. *Later* leaves them open: the button stays on the server's sheet."],
      ["The server's sheet", "Click the server's card: system, processors, memory, disk, listening ports, software (Docker, Compose, k3s, nginx, Caddy, ufw…), firewall, running services and containers, read by one read-only script. *Refresh* reads it again; Athanor also does every 6 hours while the vault is open. The roles change there too; *Remove this server* forgets it without touching the server, and its key stays in the vault unless you tick the box. A paired phone sees the sheets, read only."],
    ],
    note: "To act on a server, give it a task: see *Give a task to the Servers agent*. The key never leaves Athanor: the agents get tools that use it, never the key itself." },
  { id: "fournisseurs", titre: "Connect Cloudflare (DNS and HTTPS)", intro: "A provider lets the Servers agent point your domains at your servers and set up HTTPS. Give it a Cloudflare token with only the rights Athanor uses, on your domain's zone only.",
    etapes: [
      ["Create the token", "On Cloudflare's dashboard: *My Profile › API Tokens › Create Token › Create Custom Token*. Permissions: *Zone › Zone › Read*, *Zone › DNS › Edit* (records) and *Zone › SSL and Certificates › Edit* (HTTPS mode and origin certificates). Add *Account › Access: Apps and Policies › Edit* only to protect an environment with Cloudflare Access. *Zone Resources*: *Include › Specific zone ›* your domain. Then *Continue to summary › Create Token*, and copy it."],
      ["Add it", "*Servers › Providers › + Add a provider*: paste the token (it goes straight to the vault, in the *infra* scope, which opens only with Athanor's password) or choose a Cloudflare entry already in the vault.", "#fournisseurs", "Providers"],
      ["Test", "*Test* reads, with the token, the zones it sees and what it may do on each: read or change DNS records, HTTPS settings, Cloudflare Access, origin certificates. Nothing is changed on Cloudflare. A refused, expired or disabled token is said plainly: create a new one and replace it in the Vault."],
      ["Use it", "On a project's *Access* page, an environment chooses its DNS provider. A provider used by an environment cannot be removed: take it out of those environments first. *Remove* forgets the provider; its token stays in the vault."],
    ],
    note: "The token never leaves Athanor: the Servers agent gets tools that use it on the zones of its environment, never the token itself. A paired phone sees the providers, read only." },
  { id: "taches-serveurs", titre: "Give a task to the Servers agent", intro: "Say in one sentence what should be done on a server. The Servers agent reads it, proposes a plan, waits for your agreement, backs up before changing, applies, then has the result checked from outside.",
    etapes: [
      ["Ask", "On the server's sheet, the *Tasks* block: type what should be done (“Install nginx and serve my site over HTTPS”), then *Launch*. The Servers agent gets tools for this server only, never its key nor its password, and loses them when the task ends. Tasks are launched from the computer; a paired phone follows them, read only.", "#serveurs", "Servers"],
      ["In native Windows mode", "Native mode has no barrier: an agent would run with all your rights on this PC. The task is refused, with *Switch to WSL*, or *Force this task* with Athanor's password if you accept the risk."],
      ["The plan", "The agent first reads the sheet, the RUNBOOK and the state of the server. To change anything it must propose a plan: a summary, then numbered steps, each with its tool, its arguments and why. A security critic, another session that can only read, reviews it first; its verdict (no risk found, or the risks one per line) is on the plan's card."],
      ["Three classes", "Athanor ranks every step itself, whatever the agent says: *read* runs without asking; *change* (installing, writing a file, a service, a firewall rule) and *destructive* (deleting, stopping SSH, resetting the firewall) wait for a plan. A command hidden behind `bash -c`, `sudo`, an interpreter, or one Athanor cannot analyse counts as destructive."],
      ["Your agreement", "The plan waits in the bell and on *For you* for 2 hours. *Accept* asks for Athanor's password again, or comes from a paired phone; *Refuse*, and nothing runs. Each accepted step runs once, exactly as shown: any other step needs a new plan. *Never ask again for this server* is offered only for changes on a *dev* server; destructive steps and *prod* always wait for you.", "#pourvous", "For you"],
      ["Backups first", "Before a file is replaced, its old version is kept away from it, in `/var/backups/athanor/prev/<path>`, readable by root only (the result gives its former mode and owner). A destructive step names what to keep: it is archived in `/var/backups/athanor/`, readable by root only, before the step runs; the step is refused if the archive fails or would take more than half the free space, and the 10 newest archives are kept."],
      ["Automatic undo", "A firewall rule or an SSH setting could lock Athanor out. Before one, the previous state is kept on the server and an undo is scheduled there. Athanor then connects again; *Keep this change*, on the server's sheet with its countdown, or the agent's confirmation cancels the undo. Without it, the server puts everything back by itself after 5 minutes."],
      ["Check and RUNBOOK", "After applying, an external verifier, another read-only session, checks the result from outside: HTTP status, certificate and its expiry, security headers, password asked. A failed check puts the task in failure and rings the bell. Each server has its `RUNBOOK.md` (*Runbook* on its sheet): access without secrets, role, what runs, the history of plans, the backups and how to go back."],
      ["Follow, stop, try", "Each task has its card on the sheet: state, dated steps, plans, check, the agent's answer. *Stop* ends it at once: the running command is cut and the agent loses its tools. To see it all on a server of no importance, *Test the Servers agent* plays a real task on Athanor's test container (Docker).", "#sante", "Setup & health"],
    ],
    note: "The agent never holds a server's key: it calls Athanor's tools, which check every step against your agreement and leave a line in the audit log." },
  { id: "environnements", titre: "Environments and Cloudflare", intro: "An environment is where a project runs: *dev* for tests, *staging* to check before going live, *prod* for your live site. Each has its address, its servers, its protection and its deployment policy.",
    etapes: [
      ["Describe an environment", "On the project's *Access* page, the *Environments* block has one card per environment: its address (URL), its servers, its protection, its deployment policy and its DNS provider, then *Save*. A server with the *prod* role goes only into *prod*, and *prod* takes only those. A paired phone sees the cards, read only.", "#acces", "Access"],
      ["The deployment policy", "*dev* and *staging* either *Ask for my approval* or deploy *Automatic when nothing is destroyed*. *prod* always asks: the card “Deploy example.com to production?”, with its commit and its server, comes in the bell, on *For you* and on your phone, and never offers *Never ask again*.", "#pourvous", "For you"],
      ["A token with minimum rights", "Cloudflare is reached with a token you create with only the rights Athanor uses (*Zone › Zone › Read*, *DNS › Edit*, *SSL and Certificates › Edit*, and *Access: Apps and Policies › Edit* only for Access), on your domain's zone only. Add it in *Servers › Providers*, *Test* it, then choose it as the environment's DNS provider. *Guide › Connect Cloudflare* gives every click.", "#fournisseurs", "Providers"],
      ["A task for an environment", "On the sheet of one of its servers, *Tasks*: choose it under *For the environment*, then say what should be done (“Point app.example.com at this server over HTTPS”). The agent may then read and change the DNS records, the HTTPS mode and the protection of the environment's address, and of that name only: another name or another zone is refused. Each change goes through a plan you accept; *prod* always waits for you.", "#serveurs", "Servers"],
      ["Protection", "*None (public)*; *Cloudflare Access (e-mail sign-in)*: only the e-mail addresses you give can open the site, with a code Cloudflare sends them; or *Password in front of the site*: a password of the vault (*infra*), of which the web server (Caddy) receives only a hash. The password itself is never written on the server nor shown to the agent."],
      ["The origin certificate", "With Cloudflare in front, Athanor makes the HTTPS certificate between Cloudflare and your server: its private key is made by Athanor, kept in the vault and placed on every server of the environment (`/etc/athanor/tls/`, readable by root and Caddy only), never handed to the agent; its expiry is kept with the environment. Without Cloudflare, Caddy gets its own certificate (Let's Encrypt), with the technical e-mail `ACME_EMAIL` of the vault."],
      ["The deployment key", "For a project linked to GitHub, *Create the deployment key*, under the cards, makes a key kept in the vault and adds it read-only to the repository: the servers will fetch the code with it and can never push. Unlinking or removing the project takes it off the repository and out of the vault."],
    ],
    note: "The token, the certificate's key, the proxy password and the deployment key stay in the vault: Athanor uses them itself, and the agent only sees zones, rights and results. *Guide › Put a site online* follows a deployment from the first one to production." },
  { id: "deployer", titre: "Put a site online", intro: "From a project on this PC to a site anyone can open: Athanor deploys a commit to an environment, checks it from outside, keeps the version before it, and watches the site afterwards.",
    etapes: [
      ["The environment", "On the project's *Access* page, give *dev* (then *staging* and *prod*) its address and its servers, as in *Guide › Environments and Cloudflare*. Each environment has its own folder on its servers, `/srv/athanor/<project>-<env>/`, with its versions side by side.", "#acces", "Access"],
      ["Harden a new server", "On a server you have just added, *Harden this server* shows a fixed plan written by Athanor, not by an agent: a user with sudo instead of root, SSH keys only, no root login, a firewall that refuses all but SSH, 80 and 443, security updates, the clock synchronised, Caddy. Each step already in place is skipped; you accept the plan first.", "#serveurs", "Servers"],
      ["The first deployment", "On the sheet of one of the environment's servers, *Tasks*: choose it under *For the environment* and say “Deploy my project to dev”. The agent picks the method in its plan: a static site served by Caddy, Docker Compose when the project has a compose file or a Dockerfile, or k3s only if you ask for it; the code comes from GitHub with the deployment key, or as an archive made on this PC. Once you accept, Athanor's own engine deploys and remembers the method for the next ones."],
      ["Dev after each milestone", "Tick *Deploy to dev after each milestone delivered* on the *dev* card: each time the pilot ticks a milestone, the project's last commit goes to dev and is checked, with no agent. The pilot may also ask for a deployment itself; *dev* and *staging* then follow their policy. Never production."],
      ["Production, on your agreement", "A deployment to *prod* always waits for you: the card “Deploy example.com to production?”, with its commit and its servers, comes in the bell, on *For you* and on your phone. *Accept* with Athanor's password retyped, or from a paired phone; one agreement is good for that commit only, and nothing touches the server before it.", "#pourvous", "For you"],
      ["Going back", "Each version goes into its own folder, then the site switches to it at once; the version before stays beside it, and the 5 newest are kept. The *Deployments* list of each environment shows who deployed which commit and what the check found. *Go back to the previous version* puts the one before back in a few seconds and checks it again; in *prod* it waits for your agreement too."],
      ["Health and incidents", "From this PC, every 15 minutes (*Health of the deployed sites* on the *Servers* page: 5 to 60 minutes, or off): the site's answer, its certificate, its security headers and protection, the disk and memory of its servers. A failed deployment, or a site found unwell twice in a row, rings the bell and your phone with what failed and what was tried, and three actions: *Retry*, *Go back to the previous version*, *I take over* (stops the automatic deployments of that environment and opens the server's sheet).", "#serveurs", "Servers"],
    ],
    note: "The agent never holds a key: the server's key, the deployment key and the values of the project's *.env* stay in the vault, and only Athanor's engine uses them. What a deployment does on a server, step by step: docs/DEPLOY.md in the repository." },
  { id: "email", titre: "Send e-mails from your apps", intro: "Your deployed apps send e-mails (sign-up, password reset, orders) through the SMTP account of your e-mail provider. Add it once on the *E-mail* page: Athanor keeps its password in the vault, tests it and gives each project the settings it needs.",
    etapes: [
      ["The account", "At your provider, create an *app password* (Gmail, Outlook and Yahoo ask for one when two-step verification is on) or an SMTP key (Brevo, Mailgun, SendGrid…). Then *E-mail › Add an email account*: choose the provider (it fills in the server, the port and the security), give the user name, the sender and the password. The password goes straight into the vault (type *E-mail account*) and is never shown again.", "#email", "E-mail"],
      ["Test", "*Send a test e-mail* sends a short message to the address you choose, within 20 seconds. A failure is said in plain words (password refused, server unreachable, wrong port or security, sender refused) and waits in the bell until a test passes. At most 5 tests a minute."],
      ["Configure sending", "*Configure sending* on an account: choose a project and an environment. The project receives *SMTP_HOST*, *SMTP_PORT*, *SMTP_SECURE*, *SMTP_USER*, *SMTP_FROM* and *SMTP_PASSWORD*: in *dev* through the pilot's secrets, in *staging* and *prod* in the deployed app's environment file. Athanor asks for its password first, if it has one."],
      ["In your app", "Read these variables like any other: for instance, with Nodemailer, *host: process.env.SMTP_HOST, port: Number(process.env.SMTP_PORT), secure: process.env.SMTP_SECURE === \"true\", auth: { user: process.env.SMTP_USER, pass: process.env.SMTP_PASSWORD }*, and *from: process.env.SMTP_FROM*. Ask the pilot in a request: “Send the order confirmation e-mail with the SMTP_* variables”."],
      ["Change or stop", "Changing the account (a new password, another server) updates every project that sends through it at its next start or deployment. The *×* next to a project stops its sending; deleting the account takes its settings out of every project, after a confirmation that names them."],
    ],
    note: "The password never appears on a page, in an answer of Athanor, in the audit log nor on a paired phone (it sees the accounts, read only). The home assistant knows the accounts' names and the projects they send for, never their settings." },
  { id: "missions", titre: "Launch a mission", intro: "One sentence on the home page, and several modules work together: Studio builds the project, Servers prepares where it will run. An orchestrator first turns your sentence into a mission card; nothing starts before you press *Launch*.",
    etapes: [
      ["The sentence and its mentions", "In the bar *What do you want to do?* of the home page, say what you want in one sentence, for example “Put @my-shop online at shop.example.com”. Type *@* to name a project, a server or a vault key (by its name, never its value): your choice becomes a chip, and its *×* takes it off. The chips under the bar add a recent project, a server or *New project*. *Send* creates the mission.", "#accueil", "Home"],
      ["The mission card", "The orchestrator reads the state of the modules, then proposes its card under the bar: what *Studio* will build (a new project with its folder, or one of yours), what *Servers* will prepare (DNS, certificates, proxy, protection, firewall), the environments with their address and protection, what will be touched (servers, DNS names, files) and an estimate drawn from the cycles already measured on this PC. When it needs to know something, its question comes at the top of the card, with its choices as buttons and a field for your own answer."],
      ["The access it uses", "*Access used* lists the vault entries the mission will give, each with a box: untick one and the project or the server does not get it. An entry the vault does not hold yet is marked *to fill*: add it in the *Vault*, the orchestrator never creates one. *I will ask for your approval for* is worked out by Athanor, not by the orchestrator: production always, hardening a server not hardened yet, unlocking the vault when an *infra* key is needed."],
      ["The policy", "Three choices on the card. *Ask me for production* (the default): production and its servers' plans wait for you, *dev* and *staging* deploy on their own. *Ask me for everything*: every Servers plan and every deployment waits for you. *Autonomous*: only putting in production waits for you. Whatever you choose, a deployment to *prod* always waits for your agreement."],
      ["Edit the card", "*Edit* opens the boxes, the policy and a field *Ask for a change* (“put the dev on the home server”). *Apply*: the boxes and the policy change the card at once, with no agent; a change you write goes back to the orchestrator, which proposes a new card. *Cancel the mission* drops it: nothing has been built or deployed yet."],
      ["Launch the mission", "*Launch* does it all, once: the new project is created (or your request joins the queue of the existing one) and its pilot starts, the access you ticked are given, the environments are set (*dev* deploys after each milestone), and a Servers task starts for each environment to prepare, its plans waiting as usual. If the mission uses an *infra* key, unlock the vault first. A step that fails stops the next ones and rings the bell; what is done stays done. *Missions*, in the menu, lists every mission with its state and its lanes.", "#missions", "Missions"],
      ["From your phone", "On a paired phone, unlocked, the same card can be read, answered, edited, launched or cancelled. When the orchestrator asks you to launch, “Launch the mission …?” comes in the bell, on *For you* and as a notification: *Accept* launches it as *Launch* does (on the computer, with Athanor's password retyped). Its questions come in the bell too, with their choices as buttons.", "#pourvous", "For you"],
      ["What the orchestrator cannot do", "The orchestrator is a Claude session with no tool of its own, in an empty folder, with no secret in its environment. It reads a summary of the modules (projects and their plan in one line, servers, environments, the names and types of the vault entries, never a value), proposes the card, asks you questions and asks you to launch. It cannot read a project's files, reach a server, call Cloudflare, build or deploy: those tools are refused to its token, and each of its calls is written to the audit log."],
    ],
    note: "Once launched, each lane follows its module's usual rules: the pilot's approvals, the Servers agent's plans and the deployments of *Guide › Put a site online*. What a mission does, step by step: docs/MISSIONS.md in the repository." },
  { id: "mission-direct", titre: "Follow a mission", intro: "Once launched, a mission has its own page: what Studio and Servers are doing side by side, the relay from a delivered milestone to production, and a journal in plain words. You pause it, stop it or change its policy there, and it carries on after Athanor restarts.",
    etapes: [
      ["The mission's page", "*Missions*, in the menu, lists every mission with its state and its lanes; click one to open its page: its sentence, its state, its policy and the *Pause* and *Stop* buttons at the top. The page reads the mission again every 2 seconds while you look at it: nothing to refresh.", "#missions", "Missions"],
      ["The lanes", "*Lanes* shows the modules side by side (one under the other on a phone): *Studio* with its project and the milestone being built (“Milestone 3/8: contact form”), *Servers* with its task and the step of its plan, or *See the plan* when it waits for your agreement. Each name opens its page."],
      ["The relay", "*Relay* follows the way to production in five steps: *Milestone delivered*, *Dev deployment*, *Verification*, *Your accord*, *Production* (the first three when the mission has no prod). The steps done are ticked, the current one moves gently, a failed one turns red. The address of dev, then of prod, shows below once it exists."],
      ["The journal", "One line per event, the newest first, with its time and a short sentence: a milestone delivered, a deployment checked from outside, production waiting for your accord. Warnings are amber, errors red; 50 lines show, *See more* opens the rest. An incident on the mission's site comes at the top of the page with *Retry*, *Go back to the previous version* and *I take over*."],
      ["Production, from your phone", "Once Studio's lane is done and dev is deployed and checked on the project's last commit, Athanor asks for your accord for production: the bell, *For you* and your phone get a card with the address, the commit, the version online now and what changes (the number of new commits and the subjects of the last ones). On a paired phone, unlocked, *Accept* is enough; on the computer, retype Athanor's password. The accord is good once, for that commit and that mission; refused or expired, *Ask again* on the mission's page asks once more. No policy lets production through without it.", "#pourvous", "For you"],
      ["Pause, stop, policy", "*Pause* stops the project's pilot and holds the mission: its Servers tasks and its deployments wait until *Resume*, which asks for Athanor's password. *Stop* ends it for good, after a confirmation: its pilot stops, its Servers tasks are cancelled and its pending accords refused, but nothing is undone: the environments and the access stay. The policy changes here too: a stricter one at once, a looser one with the password. The emergency stop pauses every mission."],
      ["After a restart", "Close Athanor or restart the PC: at the next start each launched mission is followed again where it was, never launched twice, and the journal says “Athanor restarted: the mission resumes”. A production accord asked before stays valid only if nothing changed since; otherwise it is refused, and *Ask again* asks once more. A mission that needs an *infra* key waits until you unlock the vault; a paused mission stays paused."],
      ["In a terminal", "On Linux and macOS, *E* in the terminal opens the same list; *Enter* shows a mission with its lanes, its relay as text and the 10 newest lines of its journal. *p* pauses it, *r* resumes it with Athanor's password, *a* stops it after a confirmation. The production accord is decided on page *6* (*For you*): *y* accepts, *r* refuses."],
    ],
    note: "The relay never deploys to production without your accord, whatever the policy. What a mission does once launched, step by step: docs/MISSIONS.md in the repository." },
  { id: "brancher-claude", titre: "Connect Claude Desktop", intro: "Claude Desktop or Claude Code on this PC can follow your projects through Athanor's MCP: read their plan and requests, add a request, start or pause a pilot, prepare a mission card. Only the projects and tools you allow, never more.",
    etapes: [
      ["Open a code", "*Settings* › *MCP clients* › *Connect a client* shows a 6-digit code. It works once, for 10 minutes; *Close* withdraws it.", "#options@bloc-clients-mcp", "MCP clients"],
      ["Pair in a terminal", "In a terminal on this PC, run the command shown under the code (*mcp-client appairer claude-desktop --sorte desktop*) and type the code. The terminal then waits for your yes."],
      ["Accept with a scope", "“claude-desktop wants to use Athanor's MCP” comes into *MCP clients*: tick the projects it may see (or *All projects*) and the tools it may use (reading is ticked; *ajouter_demande*, *pilote* and *proposer_mission* are not), then *Accept*. The terminal receives the token once and keeps it in your user folder, readable by you only."],
      ["Add it to Claude Desktop", "*Ready-to-copy configuration* gives the entry for *claude_desktop_config.json* (in Claude Desktop: *Settings* › *Developer* › *Edit Config*); restart Claude Desktop. For Claude Code: pair under the name *claude-code* with *--sorte code*, then *claude mcp add athanor -- Athanor mcp-client claude-code*."],
      ["What it can do", "Ask Claude “What are my projects doing?”: it reads the state of the modules, the projects of its scope, their plan, their requests, the missions and the approvals waiting. With the tools you ticked, it adds a request, starts a pilot, pauses it at the end of the cycle or stops it, or prepares a mission card that waits for your *Launch*. Each call goes to the audit log under *client:claude-desktop*."],
      ["What it can never do", "No client decides an approval, launches a mission, reads a value of the vault or touches a server: those tools do not exist for it, whatever you tick. Athanor checks the token, the tool and the project at each call; a project outside its scope does not exist for it. Athanor closed: Claude gets a clear error, never a hang."],
      ["Revoke", "*Revoke* on its line in *MCP clients*: its token stops working at once, and it must pair again to come back. The emergency stop suspends every client and closes pairing until *Resume*.", "#options@bloc-clients-mcp", "MCP clients"],
    ],
    note: "Everything stays on this PC: the relay talks to Athanor on 127.0.0.1 only, with no address open on the Internet. Step by step, with troubleshooting: docs/MCP.md in the repository." },
  { id: "athanor-distant", titre: "Add a remote Athanor", intro: "Link another Athanor to this one, on this PC, on your network or on a VPS: its projects and its Servers module come into your hub, and you drive its pilots from here. No link is made without the 6-digit code it shows and its owner's yes.",
    etapes: [
      ["There: open a code", "On the Athanor to add, *Settings* › *MCP clients* › *Connect a client* shows a 6-digit code (on a VPS with no window: *athanor module appairer* in its terminal). Its remote access must be on: its address is the one of its tunnel."],
      ["Here: add it", "*Settings* › *Remote Athanors* › *Add a remote Athanor*: its remote access address (https://…) and the code, then *Add*. The line says *Waiting for the other one's yes*.", "#options@bloc-instances-pc", "Remote Athanors"],
      ["There: accept", "Its owner accepts this Athanor on its screen (in its terminal for a module), choosing the projects and the tools it lends. The token comes once and is kept in this Athanor's vault: unlock the vault to reach the remote one."],
      ["On this PC or the network", "The Athanors running on this computer show under *On this PC*: type the code the other one shows, then *Pair*. *Discover the Athanors of the local network* (off by default) lists those that announce their remote access address: a proposal only, linked with the code alone."],
      ["Its projects in your hub", "A remote Athanor with Studio brings its projects into *Projects* and the home page as *name@instance*, greyed while it is offline (its last list stays). *Start*, *Stop after cycle* and *Stop now* drive its pilot from here if it lent the *pilote* tool; a mission can aim at one of its projects with *@name@instance*.", "#global", "Projects"],
      ["Servers on a VPS", "*athanor module servers* runs the Servers module alone, with no window, on a Linux VPS (a systemd unit is in docs/MODULE.md). Linked to your hub, its servers and tasks show here, and a production action still waits for your accord, checked on both sides."],
      ["Remove a link", "*Remove* on its line deletes the link and its token here; the other Athanor can revoke it in its *MCP clients*, and the line then says *Pairing revoked*. Each pairing, call and removal goes to the audit log of both Athanors."],
    ],
    note: "Nothing listens outside 127.0.0.1: the remote Athanor is reached through its own Cloudflare tunnel, and a client token works on the MCP only. Step by step, with troubleshooting: docs/REMOTE.md in the repository." },
  { id: "github", titre: "GitHub", intro: "Link a project to a GitHub repository: its commits go there after each milestone. Each computer connects to GitHub once, in the settings.",
    etapes: [
      ["Connect this computer to GitHub", "Once per computer: *Settings › Options › GitHub › Connect to GitHub*. A window opens: sign in to GitHub and authorize. Athanor puts a key “Athanor (this computer)” on your GitHub account and keeps it, out of Claude's reach; it then links any repository your account can push to, yours or one you are a collaborator of.", "#options@bloc-connexion-gh", "Connect to GitHub"],
      ["Link the project", "*Manage › GitHub*: *Use one of my repositories* (OWNER/NAME) links at once, with no window; *Create a private repository* opens one sign-in, only to create it. Athanor checks right away that your account can push there, and says why when it cannot (not found, read only, network).", "#projet@gh-bloc", "Manage › GitHub"],
      ["Share the workshop", "Tick *Share the workshop (.athanor/) on GitHub* when someone else goes on with the project in their own Athanor: the request, the plan, the log and the documents go into the commits (never secrets.env, the raw logs nor the pilot's state). Working with other computers switches it on."],
      ["The pushes", "After each milestone and at the delivery, or with *Push now*. Never forced: if GitHub has commits the project does not, Athanor stops and says so. Each push is checked for secrets first (keys, tokens, .env files)."],
      ["The author of the commits", "Your name and your GitHub address (the private *noreply* one is proposed). By default the commits are yours alone, with no “Co-Authored-By: Claude” line; untick it to show Claude as co-author. Set it on each computer: the history then shows who did what."],
      ["Working in turns", "Two people on one repository, one after the other: the owner adds the other as a collaborator on GitHub (the repository's *Settings › Collaborators*). Each one connects their computer, links the project and ticks *Share the workshop*. Before working: *Get GitHub's work* brings in what the other pushed; *Start* checks it too and offers to bring it in first. When you stop, the push after each milestone (or *Save and push the work in progress*) leaves your work on GitHub for the other.", "#projet@gh-bloc", "Manage › GitHub"],
    ],
    note: "*Settings › Options › GitHub › Disconnect* removes the key from this computer. A project linked by an older version with a key of its own shows *Use my GitHub connection* in its GitHub block." },
  { id: "distance", titre: "From your phone", intro: "Follow and drive Athanor from a phone or another computer, from anywhere, through a Cloudflare tunnel. Nothing to install on the phone.",
    etapes: [
      ["Switch it on", "*Remote* › *Remote access*. The first time, Athanor downloads Cloudflare's tunnel program.", "#distance", "Remote"],
      ["Pair the phone", "Scan the QR code with the phone's camera (or open the link on the other device), then type the 6 digits the computer shows when the mouse is over them. They change every 20 seconds, so a photo of the QR code is not enough."],
      ["Use it", "The phone gets the same pages, laid out for its screen: start, stop, chat, requests. *Add to Home Screen* makes it an app. The secrets, the remote settings and the computer's folders stay on the computer."],
      ["Remove a device", "*Remote* › *Trusted devices*: ✕ removes one at once. Switching remote access off closes the tunnel for every device."],
      ["A fixed address (optional)", "The temporary address changes when the computer restarts. With a domain whose DNS is at Cloudflare (a free account is enough): *Address › Use my domain*, then sign in to Cloudflare once. The page also shows how to add Cloudflare Access: an e-mail and a code are asked before anything reaches the computer."],
      ["Notifications on the phone", "On the phone, *Remote* › *This device* › *Notifications on this device*: a notification even with Athanor closed, when Claude needs you, a milestone is done, a project is delivered or a computer of a team is stuck. On an iPhone or iPad, add Athanor to the Home Screen first and open it from there."],
      ["Stop using your domain", "*Address › Back to a temporary address* deletes the tunnel at Cloudflare. Then, in Cloudflare's dashboard: delete the name's CNAME record (*DNS › Records*), and the Access application if you made one (*Zero Trust › Access › Applications*). If the computer was off at that moment, delete the tunnel there too (*Zero Trust › Networks › Tunnels*)."],
    ],
    note: "While remote access is on, the computer does not go to sleep (the screen can still turn off), and Athanor must stay open." },
  { id: "pcs", titre: "Several computers", intro: "Several computers build one project at the same time, from one GitHub repository: each one takes a milestone no other one is building, and they merge each other's work. No GitHub organization is needed.",
    etapes: [
      ["What it needs", "One GitHub repository, and for each person an account that can push to it: the owner's own, or a collaborator's. The owner adds each person in the repository's *Settings › Collaborators* on GitHub (write access). A GitHub organization is not needed."],
      ["Connect each computer", "On every computer, once: *Settings › Options › GitHub › Connect to GitHub*, with the account that can push to the repository.", "#options@bloc-connexion-gh", "Connect to GitHub"],
      ["On the first computer", "Link the project to the repository (*Manage › GitHub*), then *Overview › Other computers › Work with other computers*. Athanor then shares the workshop (.athanor/): the others get the request, the plan and the log with the next push.", "#suivi@bloc-pcs", "Overview"],
      ["On each other computer", "*New project › Join a project*: the repository (OWNER/NAME), the same branch and a new empty folder. With the computer connected, the project arrives at once, with its request and its plan, already working with the others.", "#rejoindre", "Join a project"],
      ["What each computer is good at", "*Remote* › *This computer, for the other computers*: a name, what it has (`RTX 4090`), a few words (`gpu`, `network`). Claude marks the milestones that need one (`Needs: gpu.`), and the pilot gives them to a computer that has the word. Without words, the milestones are simply shared out. A word may carry a level from 1 to 9 (`gpu:3`): a milestone `Needs: gpu:3.` goes first to a computer at that level or more, the others take the lighter ones at the same time.", "#distance", "What this computer is good at"],
      ["Start the pilots", "Start the pilot on each computer. One writes the plan, the others wait for it; then each one takes a milestone, and the Overview shows who builds what. The final report is written once, by one of them."],
      ["When a program does not work on a computer", "Claude first looks for the cause and repairs what it can on that computer, even if it takes 5 or 10 minutes. Only when the cause is out of its reach (the hardware, a driver, a licence) or its repair did not work does it say so: you get a notification, and the other computers try the milestone in its place, those that have the program's word first. The one that succeeds keeps the word, and such milestones go to it from then on. If none can, the computer that was meant to build it takes it back and asks you on *For you*, saying which computers tried. Working alone, Claude asks you at that point."],
      ["Notes, and work never lost", "The computers' Claude leave each other notes (an interface that changed, a trap to avoid): the Overview shows the last ones. A computer that goes away in the middle of a milestone has sent its unfinished work to GitHub (every 10 minutes, and when its pilot stops): the computer that takes the milestone next goes on from there."],
      ["When a computer waits", "Its Overview says why: everything left depends on a milestone in progress; files of its folder that GitHub has too (move them); a project with another history (start again from an empty folder). A real merge conflict is undone, then Claude merges it at its next cycle."],
    ],
    note: "A computer whose pilot runs without the switch works alone: its pushes are refused as soon as another computer has pushed. Parallel mode is off while several computers work on a project. Each computer leaves a small ref refs/athanor/pc/… on the repository, not a branch (never in the list of branches, no workflow runs for it); it goes away when that computer stops working with the others." },
  { id: "terminal", titre: "In a terminal", intro: "On Linux and macOS, Athanor also runs in a terminal, like btop: the same projects, overview, log, requests, chat and checks, with the keyboard and the mouse. Handy on a server reached through SSH.",
    etapes: [
      ["Open it", "`./athanor tui` in a terminal, in Athanor's folder (or the downloaded file: `./Athanor-Linux-x64 tui`). Through SSH, or on a Linux without a screen, `./athanor` alone opens it."],
      ["What stays in the window", "The terminal holds the home page, the missions and the Studio pages. The Vault, Servers, E-mail, Access and the home chat stay in the window: *w* opens it on a computer with a screen, and a paired phone reaches it through remote access."],
      ["The window instead", "`./athanor gui` opens the window whatever the guess (a computer reached through SSH whose screen you use, say)."],
      ["Alongside the window", "With Athanor already open, `./athanor tui` shows the same Athanor in the terminal: both stay in step. In the terminal, *w* opens the window as well."],
      ["The keys", "*1*–*9* the pages, *s* Start, *a* Stop after cycle, *x* Stop now, *p* the project, *n* a request, *i* writes in the chat, *?* every key, *q* quits. The mouse works too."],
      ["Quitting", "*q* (or Ctrl+C) asks, then quits: the pilots keep running. When the terminal started Athanor, Athanor stops with it: start it again to follow them."],
    ],
    note: "The colours follow the app's palette. Over SSH the terminal may not say it shows 24-bit colours: `ATHANOR_COULEURS=24 ./athanor tui` forces them. Windows: Athanor is its window." },
];
// mdGuide: the guide's texts, with **bold**, *emphasis* and `code`
const mdGuide = (s) => h(s).replace(/\*\*([^*]+)\*\*/g, "<b>$1</b>").replace(/\*([^*]+)\*/g, "<em>$1</em>").replace(/`([^`]+)`/g, "<code>$1</code>");
// lienGuide: a "How it works" link to one tutorial of the guide
const lienGuide = (sujet, texte) => `<a class="lien-guide" href="#guide" data-guide-sujet="${sujet}">${h(texte || t("How it works"))}</a>`;
pages.guide = async () => {
  const sujet = GUIDE.find((g) => g.id === etat.guideSujet) || GUIDE[0];
  const tours = (etat.hubModules || []).filter((m) => (m.tutoriel || []).length);
  return `${tours.length ? `<div class="bloc ligne tutoriels"><b>${t("Tutorials")}</b><span class="muet petit croit">${t("A short tutorial of each part, shown at your first visit. Replay one:")}</span>
    ${tours.map((m) => `<button type="button" class="btn btn-petit" data-tutoriel="${h(m.id)}">${h(t(m.nom))}</button>`).join("")}</div>` : ""}
  <div class="onglets onglets-guide">${GUIDE.map((g) => `<button class="${g === sujet ? "actif" : ""}" data-guide-sujet="${g.id}">${t(g.titre)}</button>`).join("")}</div>
  <div class="bloc guide"><h2>${t(sujet.titre)}</h2><p class="intro">${mdGuide(t(sujet.intro))}</p>
    <div class="etapes">${sujet.etapes.map(([titre, texte, lien, libelle], i) => `<div class="etape ${lien ? "" : "sans-lien"}"><div class="n">${i + 1}</div>
      <div><b>${t(titre)}</b><div class="d">${mdGuide(t(texte))}</div></div>
      ${lien ? `<a class="btn btn-petit" href="${lien}">${t(libelle)}</a>` : ""}</div>`).join("")}</div>
    ${sujet.note ? `<p class="muet petit mt-2">ℹ ${mdGuide(t(sujet.note))}</p>` : ""}
  </div>`;
};
document.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-tutoriel]"); if (!b) return;
  const m = (etat.hubModules || []).find((x) => x.id === b.dataset.tutoriel); if (m) lancerTutoriel(m);
});
// a tutorial chosen from anywhere (the guide's tabs, the "How it works" links)
document.addEventListener("click", (ev) => {
  const a = ev.target.closest("[data-guide-sujet]"); if (!a) return;
  etat.guideSujet = a.dataset.guideSujet;
  if (etat.page === "guide") { ev.preventDefault(); rendrePage(); $("#page").scrollTop = 0; }
}, true);

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
      <div class="ligne"><input type="text" id="gp-dossier" placeholder="${h(t("e.g. D:\\Dev\\my-site"))}"><button class="btn" type="button" data-parcourir="gp-dossier">${t("Browse…")}</button><button class="btn" data-gestion="dossier" ${dis}>${t("Use this folder")}</button></div></label>
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
    ${l.length ? `<table class="tab tab-pile"><tbody>${l.map((p) => `<tr><td><b>${h(p.nom)}</b></td><td class="muet">${h(p.dossier)}</td><td class="droite"><div class="ligne fin">
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

// --- GitHub: the project's repository (athanor github). The sign-in is never kept: the repository gets
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
    ${caseAtelier(`id="${pre}-gh-atelier"`, false)}
    ${(etat.ghConnexion || {}).connecte ? `<p class="muet petit">${etat.ghConnexion.acces_garde ? t("This computer is connected to GitHub as {0}: one of your repositories is linked at once, and a new one is created at once too (the access is kept).", [h(etat.ghConnexion.login)]) : t("This computer is connected to GitHub as {0}: one of your repositories is linked at once; creating a new one opens a sign-in.", [h(etat.ghConnexion.login)])}</p>`
      : pre === "np" ? `<p class="attention petit" data-avis-connexion>${t("To link a project, connect this computer to GitHub first, once (Settings › Options › GitHub).")} <button class="btn btn-petit" type="button" data-connexion-gh="connecter">${t("Connect to GitHub")}</button></p>` : ""}`;
}
// the workshop shared on GitHub: for someone who takes the project over with their Athanor (off by default)
function caseAtelier(attribut, coche) {
  return `<label class="interrupteur"><input type="checkbox" ${attribut} ${coche ? "checked" : ""}><span>${t("Share the workshop (.athanor/) on GitHub")}</span></label>
    <p class="muet petit">${t("For a repository shared with someone who goes on with the project in their own Athanor (git pull): the plan, the requests, the log and the documents go into the commits. Never secrets.env, the raw logs nor the pilot's state. To build it with several computers at once: Overview › Other computers (it switches this on). Unticked later, the workshop leaves the next push.")} ${lienGuide("pcs")}</p>`;
}
// depotSaisi: OWNER/NAME from what was typed (or the repository's address as GitHub shows it), "" if invalid
function depotSaisi(x) {
  const v = String(x || "").trim().replace(/^(https?:\/\/)?(www\.)?github\.com\//i, "").replace(/^git@github\.com:/i, "").replace(/\/+$/, "").replace(/\.git$/i, "");
  return /^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9._-]{1,100}$/.test(v) ? v : "";
}
// nomDepuisDepot: a project name from a repository ("octo/my_site.v2" → "my-site-v2")
function nomDepuisDepot(x) {
  const d = depotSaisi(x); if (!d) return "";
  return d.split("/")[1].replace(/[^A-Za-z0-9 -]+/g, "-").replace(/-+/g, "-").replace(/^-|-$/g, "").slice(0, 60);
}
// null: no GitHub; false: incomplete or invalid (said in a toast); else what to send to /api/github
function lireChoixGitHub(pre, nomProjet) {
  const mode = (document.querySelector(`input[name=${pre}-gh]:checked`) || {}).value || "";
  if (!mode) return null;
  const auto = !!($("#" + pre + "-gh-auto") || {}).checked;
  const atelier = !!($("#" + pre + "-gh-atelier") || {}).checked;
  if (mode === "creer") {
    const tape = $("#" + pre + "-gh-nom").value.trim();
    const valeur = tape ? nomDepot(tape) : nomDepot(nomProjet);
    if (tape && valeur !== tape) $("#" + pre + "-gh-nom").value = valeur; // spaces and accents become a valid name
    return { action: "lier", mode, valeur, auto, atelier };
  }
  // OWNER/NAME, or the repository's address as GitHub shows it
  const v = $("#" + pre + "-gh-depot").value.trim().replace(/^(https?:\/\/)?(www\.)?github\.com\//i, "").replace(/^git@github\.com:/i, "").replace(/\/+$/, "").replace(/\.git$/i, "");
  if (!/^[A-Za-z0-9][A-Za-z0-9-]{0,38}\/[A-Za-z0-9._-]{1,100}$/.test(v)) { toast(t("Write the repository as OWNER/NAME (e.g. octocat/my-site)."), "erreur"); return false; }
  return { action: "lier", mode, valeur: v, auto, atelier };
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
  const cx = g.connexion_pc || {};
  if (!g.lie) return `<h2>GitHub</h2>
    <p class="muet petit">${t("Send the project's commits to one of your GitHub repositories, by itself after each milestone.")} ${lienGuide("github")}</p>${manque}
    <div class="gh-etape ${cx.connecte ? "faite" : ""}"><b>1. ${t("This computer connected to GitHub")}</b>${cx.connecte ? `<p class="ok petit m0">✔ ${t("Connected as {0}", [h(cx.login)])}</p>`
      : `<p class="petit m0">${t("Once per computer: Athanor then links any repository your account can push to (yours, or one you are a collaborator of).")}</p><div class="ligne"><button class="btn btn-primaire" data-connexion-gh="connecter">${t("Connect to GitHub")}</button></div>`}</div>
    <div class="gh-etape"><b>2. ${t("Repository")}</b>${choixGitHub("gh", r.nom_propose || "")}</div>
    <div class="gh-etape"><b>3. ${t("Commits")}</b>${rendreAuteur(g.identite)}</div>
    <div class="gh-etape"><b>4. ${t("Link")}</b>
      <div class="ligne"><button class="btn ${cx.connecte ? "btn-primaire" : ""}" data-gh="lier" ${cx.connecte ? "" : "disabled"}>${t("Link to GitHub")}</button><button class="btn btn-petit" data-gh="actualiser">${t("Refresh")}</button></div>
      ${cx.connecte ? "" : `<p class="muet petit">${t("Connect this computer to GitHub first (step 1).")}</p>`}</div>`;
  const cls = !d ? "" : d.resultat === "ok" ? "ok" : d.resultat === "attente" ? "muet" : "bloque";
  const quand = d && d.quand ? String(d.quand).replace("T", " ").slice(0, 16) : "";
  const test = etat.ghTest && etat.ghTest.projet === etat.projet ? `<p class="petit ${etat.ghTest.ok ? "ok" : "bloque"}">${etat.ghTest.ok ? "✔" : "✖"} ${h(t(etat.ghTest.message_key || "", etat.ghTest.args || []))}</p>${!etat.ghTest.ok && etat.ghTest.detail ? `<p class="muet petit"><code>${h(etat.ghTest.detail)}</code></p>` : ""}` : "";
  return `<h2>GitHub</h2>${manque}
    <p class="gh-statut"><span class="ok">✔</span> ${t("Linked to")} <b>${h(g.depot)}</b> · ${t("branch")} <code>${h(g.branche)}</code>${g.compte ? ` · ${t("account")} ${h(g.compte)}` : ""} <button class="btn btn-petit" data-gh="ouvrir">${t("Open on GitHub")}</button></p>
    <div class="ligne"><button class="btn" data-gh="tester">${t("Test the connection")}</button><button class="btn" data-gh="pousser">${t("Push now")}</button><button class="btn btn-primaire" data-gh="wip" title="${h(t("Commits what is in the folder now (“M3: … (in progress)”) and pushes it, without Claude: someone else can take over, even with no quota left."))}">${t("Save and push the work in progress")}</button>${d && d.resultat === "bloque" && d.sha ? `<button class="btn btn-danger" data-gh="malgre" data-sha="${h(d.sha)}">${t("Push anyway")}</button>` : ""}</div>
    <p class="muet petit">${t("Commits what is in the folder now (“M3: … (in progress)”) and pushes it, without Claude: someone else can take over, even with no quota left.")}</p>
    ${g.equipe ? "" : `<div class="ligne mt-1"><button class="btn" data-gh="recuperer">${t("Get GitHub's work")}</button></div>
    <p class="muet petit">${t("Working on this repository in turns with someone else: brings in what they pushed (as git pull would), before you start the pilot. Athanor also checks it when you press Start.")}</p>`}
    ${test}
    ${(d && d.resultat === "rejete" && /has commits the project does not have/.test(d.modele || "")) || (etat.ghTest && etat.ghTest.projet === etat.projet && /has commits the project does not have/.test(etat.ghTest.message_key || "")) ? `<div class="ligne mt-1"><button class="btn btn-primaire" data-gh="resoudre">${t("Sort it out with GitHub's branch…")}</button></div>` : ""}
    ${d ? `<div class="gh-dernier"><b class="${cls}">${h(t(LIBELLES_GH[d.resultat] || d.resultat))}</b> <span class="muet petit">${h(quand)}${d.commit ? " · " + h(d.commit) : ""}</span>
      <div class="petit">${h(t(d.modele || d.message || "", d.args || []))}</div>
      ${(d.fichiers || []).length ? `<ul class="petit">${d.fichiers.map((x) => `<li><code>${h(x.f)}</code> — ${h(t(x.genre))}</li>`).join("")}</ul>` : ""}</div>`
      : `<p class="muet petit">${t("Nothing pushed yet.")}</p>`}
    <label class="interrupteur"><input type="checkbox" data-gh-auto ${g.auto ? "checked" : ""}><span>${t("Push automatically after each milestone")}</span></label>
    ${caseAtelier("data-gh-atelier", g.atelier)}
    ${rendreAuteur(g.identite)}
    <label class="champ mt-2"><span>${t("GitHub branch")}</span><div class="ligne"><input type="text" id="gh-branche" value="${h(g.branche)}"><button class="btn btn-petit" data-gh="branche">${t("Change")}</button></div></label>
    <p class="muet petit">${g.connexion ? t("Athanor pushes with this computer's connection to GitHub ({0}), out of Claude's reach (Settings › Options › GitHub). It never overwrites what is on GitHub and checks every push for secrets (keys, tokens, .env files).", [h((g.connexion_pc || {}).login || g.compte || "")]) : g.cle_compte ? t("The key Athanor pushes with is on your GitHub account (you are a collaborator of this repository): it stays out of Claude's reach, and you can remove it in GitHub's Settings › SSH and GPG keys. Athanor never overwrites what is on GitHub and checks every push for secrets (keys, tokens, .env files).") : t("The key Athanor pushes with works for this repository only and stays out of Claude's reach. Athanor never overwrites what is on GitHub and checks every push for secrets (keys, tokens, .env files).")}</p>
    ${g.connexion ? "" : `<div class="gh-ancienne-cle"><p class="attention petit m0">${t("This project still pushes with a key of its own, from an older version of Athanor. Move it to this computer's connection to GitHub: one key for every project, nothing to link again.")}</p>
      <div class="ligne">${cx.connecte ? `<button class="btn btn-petit btn-primaire" data-gh="connexion">${t("Use my GitHub connection")}</button>` : `<button class="btn btn-petit btn-primaire" data-connexion-gh="connecter">${t("Connect to GitHub")}</button>`}</div></div>`}
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
  if (!r.erreur) { etat.ghCache = etat.ghCache || {}; etat.ghCache[nom] = r; if (r.github && r.github.connexion_pc) etat.ghConnexion = r.github.connexion_pc; }
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
  if (act === "recuperer") {
    b.disabled = true; b.textContent = t("Getting GitHub's work…");
    const r = await recupererGitHub();
    if (r && r.resultat === "bloque" && r.sans_lien) { await majGitHub(); return resoudreBrancheGitHub(); }
    if (r) annoncerRecuperation(r);
    return majGitHub();
  }
  if (act === "resoudre") return resoudreBrancheGitHub();
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
  if (act === "lier" || act === "connexion") b.textContent = t("Linking…");
  const r = await action(api("github", "POST", corps));
  b.disabled = false;
  if (r && r.lie_compte) { await resultatLierCompte(r, corps); return majGitHub(); }
  if (r && r.cree) { toast(t("Private repository created and linked: {0}", [r.cree]), "ok"); return majGitHub(); }
  if (r && r.enregistre) toast(t("Saved: {0}", [r.enregistre]), "ok");
  if (r && r.github && r.github.dernier) { const d = r.github.dernier; toast(t(d.modele || d.message || "", d.args || []), d.resultat === "ok" ? "ok" : "erreur"); }
  if (act === "lier" && r) etat.ghSuivi = { projet: etat.projet, jusqua: Date.now() + 5 * 60000 };
  majGitHub();
}

// joining a project other computers already build (pcs.go): its repository, a new folder, one window
function formulaireRejoindre() {
  return `<div class="bloc"><h2>${t("Join a project")}</h2>
      <p class="intro petit">${t("This computer joins a project other computers already build through its GitHub repository: it gets its request, its plan and its work, then builds milestones with them.")} ${lienGuide("pcs")}</p>
      <label class="champ"><span>${t("GitHub repository")}</span><input type="text" id="rj-depot" placeholder="${h(t("OWNER/NAME, e.g. octocat/my-site"))}" autocomplete="off"></label>
      <label class="champ"><span>${t("Branch")}</span><input type="text" id="rj-branche" value="main" autocomplete="off"></label>
      <label class="champ"><span>${t("Name")}</span><input type="text" id="np-nom" placeholder="${h(t("the repository's name"))}"></label>
      <label class="champ"><span>${t("Folder of the project on this computer")}</span>${champDossier("np-dossier", "", t("e.g. D:\\Dev\\watch-store"), true)}</label>
      <p class="muet petit">${t("A new or empty folder: the team's files arrive in it.")}</p>
      ${(etat.ghConnexion || {}).connecte ? `<p class="ok petit">✔ ${t("This computer is connected to GitHub as {0}: the project arrives at once.", [h(etat.ghConnexion.login)])}</p>`
        : `<p class="attention petit" data-avis-connexion>${t("First, connect this computer to GitHub (once): with the account the repository's owner added as a collaborator.")} <button class="btn btn-petit" type="button" data-connexion-gh="connecter">${t("Connect to GitHub")}</button></p>`}
      <button class="btn btn-primaire" id="rj-rejoindre" ${(etat.ghConnexion || {}).connecte ? "" : "disabled"}>${t("Join the project")}</button>
    </div>`;
}
const ongletsNouveau = () => `<div class="onglets"><button class="${etat.npRejoindre ? "" : "actif"}" data-np-mode="nouveau">${t("New project")}</button><button class="${etat.npRejoindre ? "actif" : ""}" data-np-mode="rejoindre">${t("Join a project")}</button></div>`;
// the project's settings beside the form (the block of Manage › Working mode): chosen here, sent once the
// project exists
pages.nouveau = async () => {
  connexionConnueGH();
  if (!etat.npMode) { const m = await api("mode?defaut=1").catch(() => null); if (m) etat.npMode = { ...m, duree_max: -1 }; }
  return pageNouveau();
};
const pageNouveau = () => `${ongletsNouveau()}<div class="${etat.npRejoindre || !etat.npMode ? "page-etroite" : "np-colonnes"}">
    ${etat.npRejoindre ? formulaireRejoindre() : `<div class="bloc"><h2>${t("New project")}</h2>
      <p class="intro petit">${t("A project = a development folder, anywhere on the PC (existing or new). The tool only adds a .athanor subfolder (request, plan, log, checks): delete it at the end and the project is clean.")}</p>
      <label class="champ"><span>${t("Name")}</span><input type="text" id="np-nom" placeholder="${h(t("e.g. watch-store"))}"></label>
      <label class="champ"><span>${t("Development folder")}</span>${champDossier("np-dossier", "", t("e.g. D:\\Dev\\watch-store"))}</label>
      <label class="champ"><span>${t("What you want (you can complete it later)")}</span><textarea id="np-demande" rows="7" placeholder="${h(t("Describe the expected result, for whom, on which platform. Technologies are free unless you impose one."))}"></textarea></label>
      ${barreAmeliorer("np-demande")}
      ${zonePieces("np")}
      <div class="champ gh-nouveau"><span>GitHub</span>${choixGitHub("np", nomDepot(""))}</div>
      <button class="btn btn-primaire" id="np-creer">${t("Create the project")}</button>
    </div>${etat.npMode ? `<div class="np-reglages"><p class="muet petit m0">${t("This project's settings: you can change them later in Manage.")}</p><div id="np-mode">${blocMode(etat.npMode)}</div></div>` : ""}`}
  </div>`;

// champDossier: a folder field with its Browse… button (the system's dialog, or Athanor's picker)
function champDossier(id, valeur, exemple, rejoindre, titre) {
  return `<div class="ligne champ-dossier"><input type="text" id="${id}" value="${h(valeur || "")}" placeholder="${h(exemple || "")}"><button class="btn" type="button" data-parcourir="${id}" ${rejoindre ? "data-parcourir-rejoindre" : ""} ${titre ? `data-titre="${h(titre)}"` : ""}>${t("Browse…")}</button></div>`;
}
// parcourir: the folder chosen goes into the field; to join a project, a folder with files gets a new
// folder inside it, named after the project (the team's files arrive in an empty one)
async function parcourir(b) {
  const champ = $("#" + b.dataset.parcourir); if (!champ || b.disabled) return;
  b.disabled = true;
  let r = null;
  try {
    if (!(etat.vue || {}).distant) r = await api("dossiers/choisir", "POST", { depart: champ.value.trim(), titre: b.dataset.titre || t("Choose the project's folder") }).catch((e) => { toast(e.message, "erreur"); return { annule: true }; });
    if (!r || r.indisponible) r = await selecteurDossier(champ.value.trim(), b.dataset.titre);
  } finally { b.disabled = false; }
  if (!r || !r.chemin) return;
  let chemin = r.chemin;
  if (b.hasAttribute("data-parcourir-rejoindre") && !r.vide) {
    const nom = ($("#np-nom") && $("#np-nom").value.trim()) || nomDepuisDepot(($("#rj-depot") || {}).value) || "project";
    chemin = chemin.replace(/[\\/]+$/, "") + (chemin.includes("\\") ? "\\" : "/") + nom;
  }
  champ.value = chemin;
  champ.dispatchEvent(new Event("input", { bubbles: true }));
}
document.addEventListener("click", (ev) => { const b = ev.target.closest("[data-parcourir]"); if (b) { ev.preventDefault(); parcourir(b); } });
// selecteurDossier: Athanor's own folder picker, in a dialog (no system dialog, or from a phone)
function selecteurDossier(depart, titre) {
  const fond = $("#selecteur-dossier");
  $("#sd-titre").textContent = titre || t("Choose the project's folder");
  fond.querySelector('[data-sd="annuler"]').textContent = t("Cancel");
  fond.querySelector('[data-sd="ok"]').textContent = t("Use this folder");
  fond.hidden = false;
  let courant = null;
  const montrer = async (chemin) => {
    const r = await api("dossiers?chemin=" + encodeURIComponent(chemin || "")).catch(() => null); if (!r) return;
    courant = r;
    $("#sd-chemin").textContent = r.chemin;
    $("#sd-racines").innerHTML = r.racines.map((x) => `<button class="btn btn-petit" data-chemin="${h(x.chemin)}">${x.nom === "~" ? t("Home folder") : h(x.nom)}</button>`).join("");
    $("#sd-explorateur").innerHTML = (r.parent ? `<div class="parent" data-chemin="${h(r.parent)}">↑ ${t("parent folder")}</div>` : "") + (r.dossiers.map((d) => `<div data-chemin="${h(d.chemin)}">📁 ${h(d.nom)}</div>`).join("") || `<div class="muet">${t("No subfolder.")}</div>`);
  };
  montrer(depart);
  return new Promise((resoudre) => {
    const fin = (v) => { fond.hidden = true; fond.onclick = null; document.removeEventListener("keydown", clavier, true); resoudre(v); };
    const clavier = (e) => { if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); fin(null); } };
    fond.onclick = (e) => {
      const c = e.target.closest("[data-chemin]"); if (c) return montrer(c.dataset.chemin);
      const b = e.target.closest("[data-sd]");
      if (b) return fin(b.dataset.sd === "ok" && courant ? { chemin: courant.chemin, vide: !!courant.vide } : null);
      if (e.target === fond) fin(null);
    };
    document.addEventListener("keydown", clavier, true);
  });
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
        <input type="password" autocomplete="off" spellcheck="false" data-pv-secret="${h(x.id)}|${h(n)}" value="${h(b.secrets[n] || "")}" placeholder="${h(t("paste it here: it goes into Athanor's vault on this PC, never into git"))}"></label>`).join("")}
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

// the agents' pending approvals, every project, first on For you: what was asked, why, and what accepting gives
const ENVS_APPR = ["dev", "staging", "prod"];
const signatureAppr = (ap) => JSON.stringify(((ap || {}).approbations || []).filter((x) => x.etat === "attente").map((x) => x.id));
// a Servers plan (M91): its steps as the code classed them; every text comes from the agent or the server, shown as text
const CLASSES_ETAPE = () => ({ lecture: [t("read"), "pv-ok"], changement: [t("change"), "pv-claude"], destructrice: [t("destructive"), "pv-vous"] });
function argumentsLisibles(l) {
  let o; try { o = JSON.parse(l); } catch { return h(l || ""); } // cut at 4000 characters: shown as it came
  return Object.entries(o || {}).map(([k, v]) => `<span class="muet">${h(k)}</span> ${h(typeof v === "string" ? v : JSON.stringify(v))}`).join("<br>");
}
function blocPlan(x, p) {
  if (!p) return `<div class="pv-details">${h(x.raison)}</div>`;
  const etapes = (p.etapes || []).map((e) => {
    const [lib, cl] = CLASSES_ETAPE()[e.classe] || [e.classe, ""];
    // C52: a step marked run waits for its result, then says ✓ or ✗ (and why)
    const fait = !e.executee ? "" : e.etat === "ok" ? ` <span class="pv-etat pv-ok">✓ ${t("applied")}</span>` : e.etat === "echec" ? ` <span class="pv-etat pv-vous">✗ ${t("failed")}</span>` : ` <span class="pv-etat pv-claude">${t("running…")}</span>`;
    return `<li><div class="plan-etape-tete"><code>${h(e.outil)}</code> <span class="pv-etat ${cl}">${h(lib)}</span>${fait}</div>
      ${e.raison ? `<div class="petit muet">${t("Why this class:")} <span lang="en">${h(e.raison)}</span></div>` : ""}${e.etat === "echec" && e.message ? `<div class="petit echec">${h(e.message)}</div>` : ""}
      <div class="plan-args">${argumentsLisibles(e.lisible)}</div><div class="pv-details m0">${h(e.durcir ? t(e.pourquoi) : e.pourquoi)}</div>
      ${(e.sauvegarde || []).length ? `<div class="petit plan-args">${h(t("Backed up first: {0}", [e.sauvegarde.join(", ")]))}</div>` : ""}${e.sans_sauvegarde ? `<div class="petit plan-args">${h(t("No backup: {0}", [e.durcir ? t(e.sans_sauvegarde) : e.sans_sauvegarde]))}</div>` : ""}${e.archive ? `<div class="petit plan-args">${h(t("Archive made: {0}", [e.archive]))}</div>` : ""}</li>`;
  }).join("");
  // C73: only Athanor's key gets in after this hardening: said, its public key exported, a box to tick
  const pub = p.seule_cle ? p.seule_cle + " athanor" : "";
  const seule = pub && x.id ? `<p class="petit attention">⚠ ${h(t("Only Athanor's key will get into this server: passwords are cut and no other key lets root in. Put your own public key in root's ~/.ssh/authorized_keys first (hardening copies it to athanor), then refuse this plan and ask for it again; or keep a copy of Athanor's public key below."))}</p>
    <label class="champ"><span>${t("Public key")}</span><textarea readonly rows="2" class="code" data-seule-pub>${h(pub)}</textarea></label>
    <div class="ligne m0"><button type="button" class="btn btn-petit" data-seule-copier>${t("Copy")}</button><a class="btn btn-petit" download="athanor-${h(p.serveur)}.pub" href="data:text/plain;charset=utf-8,${encodeURIComponent(pub + "\n")}">${t("Download")}</a></div>
    <label class="ligne m0 mt-1"><input type="checkbox" data-appr-seule> <span>${h(t("I know that only Athanor's key will get in after this plan"))}</span></label>` : "";
  return `<div class="pv-details">${h(p.agent === "durcissement" ? t(p.resume) : p.resume)}</div><ol class="plan-etapes">${etapes}</ol>
    ${p.critique ? blocCritique(p.critique) : ""}${seule}`;
}
// the security critic's verdict (M98): reading, no risk found, or its risks one per line
function blocCritique(c) {
  const [lib, cl] = !c.verdict ? [t("Reading the plan…"), "pv-claude"] : c.verdict === "ok" ? [t("No risk found"), "pv-ok"] : [t("Risks found"), "pv-vous"];
  return `<div class="pv-note plan-critique"><strong>${t("Security critic")}</strong> <span class="pv-etat ${cl}">${h(lib)}</span>
    ${(c.lignes || []).length ? `<ul class="petit m0">${c.lignes.map((l) => `<li>${h(l)}</li>`).join("")}</ul>` : ""}</div>`;
}
function carteApprobation(x, ap, codes) {
  const d = x.details || {};
  const p = (ap.plans || {})[x.id];
  const durcir = x.agent === "durcissement"; // B80: Athanor's own plan, titled by the server (its project is internal)
  if (x.action === "plan_serveur") return carteDecision(x, ap, durcir ? `<strong>${h(t("Harden this server: {0}", [d.nom || d.serveur]))}</strong>`
    : `<span class="badge">${h(x.env)}</span> <strong>${h(t("Plan for {0}", [d.nom || d.serveur]))}</strong> <span>${h(x.projet)}</span>`,
    [t("Server role: {0}", [x.env]), t("{0} step(s)", [d.etapes || "?"])], blocPlan(x, p), d.regle === "oui" && t("Never ask again for this server"), p && p.critique && !p.critique.verdict);
  // a deployment (M120): prod is never decided by a rule
  if (x.action === "deploiement") return carteDecision(x, ap, `<span class="badge">${h(x.env)}</span> <strong>${h(x.env === "prod" ? t("Deploy {0} to production?", [d.hote]) : t("Deploy {0} to {1}?", [d.hote, x.env]))}</strong> <span>${h(x.projet)}</span>`,
    [t("Commit {0}", [d.commit || (d.sha || "").slice(0, 7)]), d.avant && t("Online now: version {0}", [d.avant]), d.serveurs && t("Servers: {0}", [d.serveurs]), d.fichiers_build && t("Build files changed: {0}", [d.fichiers_build])],
    // P7: a mission's prod accord says where it goes, what changes and opens the mission
    (d.mission ? `${/^https?:\/\//.test(d.url || "") ? `<div class="pv-details"><a href="${h(d.url)}" target="_blank" rel="noopener">${h(d.url)}</a></div>` : ""}${d.commits ? `<div class="pv-details"><strong>${h(t("What changes: {0} new commit(s)", [d.commits]))}</strong>${d.changements ? ` <span class="appr-changements">${h(d.changements)}</span>` : ""}</div>` : ""}` : "") +
    (x.env === "prod" ? `<div class="pv-details">${h(t("Production always asks you: one accord, one deployment."))}${d.mission ? ` <a href="#missions/${encodeURIComponent(d.mission)}">${h(t("See the mission"))}</a>` : ""}</div>` : d.fichiers_build ? `<div class="pv-details">${h(t("Its build command will run these changed files on this PC, inside the pilot's barrier: one accord, one deployment."))}</div>` : ""),
    x.env !== "prod" && !d.fichiers_build && t("Never ask again for this project and environment"));
  // M158: the orchestrator asks to launch its card; accepting launches it as Launch does (never a rule)
  // C77: the environments, prod and the ticked access of the very card it launches
  if (x.action === "mission") return carteDecision(x, ap, `${d.prod === "oui" ? `<span class="badge">prod</span> ` : ""}<strong>${h(t("Launch the mission {0}?", [d.titre]))}</strong>`,
    [t("Environments: {0}", [d.envs || "—"]), t("Access: {0}", [d.acces || "—"])], `<div class="pv-details">${h(t("Accepting launches the mission as its Launch button does."))} <a href="#missions/${encodeURIComponent(x.mission || "")}">${h(t("See the card"))}</a></div>`, false);
  const acces = x.action === "demander_acces", f = etat.apprForm[x.id] || {}, nom = (x.details || {}).nom || "";
  const entree = f.entree ?? (codes.find((e) => e.nom === nom) || {}).id ?? "", env = f.env || x.env, variable = f.variable ?? nom;
  const champs = acces ? `<div class="appr-champs">
      <label class="champ"><span>${t("Vault key (code)")}</span>${codes.length ? `<select data-appr-entree><option value="">${t("Choose a key…")}</option>${codes.map((e) => `<option value="${h(e.id)}"${e.id === entree ? " selected" : ""}>${h(e.nom)}</option>`).join("")}</select>` : `<span class="muet petit">${t("No project key (code) in the vault yet.")}</span>`}
        <a class="petit" href="#coffre">＋ ${t("Add a key")}</a></label>
      <label class="champ"><span>${t("Environment")}</span><select data-appr-env>${ENVS_APPR.map((e) => `<option${e === env ? " selected" : ""}>${e}</option>`).join("")}</select></label>
      <label class="champ"><span>${t("Variable name")}</span><input type="text" data-appr-var value="${h(variable)}" maxlength="100" spellcheck="false" placeholder="${h(t("the vault's name"))}"></label></div>` : "";
  return carteDecision(x, ap, `<span class="badge">${h(x.env)}</span> <strong>${h(x.projet)}</strong> <span>${h(acces ? t("access requested") : x.action)}</span>`,
    [acces && nom && t("proposed variable: {0}", [nom])], `<div class="pv-details">${h(x.raison)}</div>${champs}`, t("Never ask again for this project and environment"));
}
// an approval's card: its head, what it asks, the password when the window decides, and its decisions ("never ask again" only when allowed)
function carteDecision(x, ap, tete, meta, corps, toujours, sansAccord) {
  const natif = ap.mot_de_passe_requis ? `<label class="champ"><span>${t("Athanor's password")}</span><input type="password" data-appr-mdp autocomplete="current-password"></label>
      <p class="muet petit">${t("Any program of this PC can drive this window, so a decision asks for Athanor's password.")}</p>`
    : ap.appareil_requis ? `<p class="petit attention">⚠ ${t("An agent of this PC could decide in your place: choose a password for Athanor in Options first, then retype it here (or decide from a paired phone)")}</p>` : "";
  const bloque = ap.appareil_requis ? " disabled" : "";
  return `<div class="bloc pv-carte appr-carte" id="appr-${h(x.id)}" data-appr="${h(x.id)}">
    <div class="pv-tete">${tete} <span class="pv-etat pv-vous">${h(t("expires {0}", [dans(Date.parse(x.expire) / 1000)]))}</span></div>
    <div class="muet petit">${[t("Asked by {0}", [x.agent]), x.mission, ...meta].filter(Boolean).map(h).join(" · ")}</div>
    ${corps}${natif}
    <div class="ligne mt-1 appr-boutons"><button type="button" class="btn btn-primaire" data-appr-decision="accepter"${bloque || (sansAccord ? " disabled" : "")}>✓ ${t("Accept")}</button><button type="button" class="btn" data-appr-decision="refuser"${bloque}>${t("Refuse")}</button>${toujours ? `<button type="button" class="btn" data-appr-decision="toujours"${bloque || (sansAccord ? " disabled" : "")}>${h(toujours)}</button>` : ""}</div>
  </div>`;
}
async function sectionApprobations() {
  const ap = await api("hub/approbations").catch(() => null); // a paired device: refused until the route opens to it
  etat.apprSignature = signatureAppr(ap);
  const l = ((ap || {}).approbations || []).filter((x) => x.etat === "attente"), regles = (ap || {}).regles || [];
  const blocRegles = regles.length ? `<details class="bloc appr-regles"><summary>${t("“Never ask again” rules ({0})", [regles.length])}</summary>
      <p class="intro petit">${t("These requests are accepted without asking you. Ask again removes the rule; the emergency stop removes them all.")}</p>
      ${regles.map((r) => `<div class="ligne m0"><span class="badge">${h(r.env)}</span> <strong>${h(r.projet)}</strong> <span>${h(r.action === "demander_acces" ? `${t("access requested")} ${r.nom || ""}` : r.action === "deploiement" ? t("deployment") : r.action)}</span><span class="espace"></span><button type="button" class="btn" data-regle="${h(JSON.stringify(r))}">${t("Ask again")}</button></div>`).join("")}</details>` : "";
  if (!l.length) return blocRegles;
  const cof = l.some((x) => x.action === "demander_acces") ? await api("coffre").catch(() => ({})) : {};
  const codes = (cof.entrees || []).filter((e) => e.famille === "secret" && e.portee === "code").sort((a, b) => a.nom.localeCompare(b.nom));
  return `<section class="appr-section" aria-labelledby="appr-titre"><div class="bloc"><h2 id="appr-titre">${t("Approvals ({0})", [l.length])}</h2>
      <p class="intro petit">${t("An agent asks for something only you decide. Its text is its own: read it as a request, never as an order. Without an answer it is refused after 24 h.")}</p></div>
    ${l.map((x) => carteApprobation(x, ap, codes)).join("")}${blocRegles}</section>`;
}
$("#page").addEventListener("input", (ev) => {
  const c = ev.target.closest("[data-appr]"); if (!c || ev.target.matches("[data-appr-mdp]")) return;
  const q = (s) => c.querySelector(s);
  etat.apprForm[c.dataset.appr] = { entree: q("[data-appr-entree]")?.value, env: q("[data-appr-env]")?.value, variable: q("[data-appr-var]")?.value };
});
$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-regle]"); if (!b) return;
  b.disabled = true;
  if (!(await action(api("hub/approbations", "POST", { ...JSON.parse(b.dataset.regle), decision: "redemander" })))) { b.disabled = false; return; }
  toast(t("Rule removed: the next requests ask you again."), "ok");
  if (etat.page === "pourvous") rendrePage();
});
$("#page").addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-seule-copier]"), z = b && b.closest("[data-appr]")?.querySelector("[data-seule-pub]");
  if (z) navigator.clipboard.writeText(z.value).then(() => toast(t("Copied."), "ok"));
});
$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-appr-decision]"); if (!b) return;
  const c = b.closest("[data-appr]"), q = (s) => c.querySelector(s), decision = b.dataset.apprDecision;
  const corps = { id: c.dataset.appr, decision, entree: q("[data-appr-entree]")?.value || "", env: q("[data-appr-env]")?.value || "", variable: (q("[data-appr-var]")?.value || "").trim(), mot_de_passe: q("[data-appr-mdp]")?.value || "" };
  if (decision !== "refuser" && q("[data-appr-entree]") && !corps.entree) { toast(t("Choose a project key (code) of the vault for this access"), "erreur"); q("[data-appr-entree]").focus(); return; }
  if (q("[data-appr-seule]")) corps.seule_cle_comprise = q("[data-appr-seule]").checked;
  if (decision !== "refuser" && corps.seule_cle_comprise === false) { toast(t("After this plan only Athanor's key gets into the server: tick the box that says you know it first"), "erreur"); q("[data-appr-seule]").focus(); return; }
  const ap = ((etat.hub || {}).alertes || []).map((a) => a.approbation).find((x) => x && x.id === corps.id);
  if (decision === "toujours" && ap && !(await confirmerToujours(ap))) return;
  b.disabled = true;
  if (await deciderApprobation(corps)) delete etat.apprForm[corps.id]; else b.disabled = false;
});

pages.pourvous = async () => {
  pvProjet();
  const [r, appr] = await Promise.all([api("pourvous"), sectionApprobations()]);
  const l = r.taches || [];
  const actives = l.filter((x) => x.etat !== "close"), closes = l.filter((x) => x.etat === "close");
  etat.pvSignature = JSON.stringify(l);
  return `${appr}<div class="bloc"><h2>${t("For you")}</h2>
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
    const corps = { id, action: act, texte: br.texte, fichiers, secrets: act === "fait" ? br.secrets : {} };
    // an infrastructure key answered here goes to the pilot: only once the user confirms it
    const r = await action(api("pourvous", "POST", corps).catch(async (e) => {
      if (!e.data || !e.data.confirmer_infra) throw e;
      if (!(await confirmer(e.message, { titre: t("Infrastructure key"), ok: t("Give it to the pilot"), annuler: t("Cancel") }))) return null;
      return api("pourvous", "POST", { ...corps, confirme: true });
    }));
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
  const membres = equipe.map((m) => `<div class="membre ${appelsCycle[m.nom] ? "utilise" : ""} ${m.perso ? "perso" : ""}">
      <div class="membre-nom">${h(m.nom)} <span class="muet">· ${m.modele ? h(modeleLisible(m.modele)) : t("lead's model")}</span>${m.perso ? ` <span class="badge">${m.perso.origine === "chef" ? t("proposed by the lead") : t("created by you")}${m.perso.lecture_seule ? " · " + t("read only") : ""}</span>` : ""}</div>
      <div class="membre-role">${h(ROLES[m.nom] ? t(ROLES[m.nom]) : m.mission)}</div>
      <div class="muet petit">${m.appele_par === "chef_ouvriers" ? t("called by the lead and the workers") : t("called by the lead")}${appelsCycle[m.nom] ? " · " + t("called {0}× this cycle", [appelsCycle[m.nom]]) : ""}</div></div>`).join("");
  return `<p class="intro">${t("Who works on this cycle and who called whom. Click a sub-agent to read the brief it was given and the report it handed back.")}</p>
  ${rendreSession(chef, appelsDe("chef"), t("Project lead"), t("Leads the current milestone: reads the plan, codes, checks, commits."), ((w) => (w[0][0] + (w.length > 1 ? w[w.length - 1][0] : w[0][1] || "")).toUpperCase())(t("Project lead").split(/\s+/)))}
  ${ouvriers.length ? `<h3 class="equipe-section">${t("Workers in parallel ({0})", [ouvriers.length])}</h3>
    <div class="equipe">${ouvriers.map((o) => rendreSession(o, appelsDe(o.id), t("Worker " + o.jalon), o.mission || t("Builds milestone " + o.jalon), o.jalon.replace(/_/g, "."))).join("")}</div>` : ""}
  ${membres ? `<h3 class="equipe-section">${t("Who can be called")}</h3><div class="roster">${membres}</div>` : ""}
  ${(r.refuses || []).length ? `<p class="muet petit">${t("Removed proposals of the lead (never proposed again): {0}", [r.refuses.map(h).join(", ")])}</p>` : ""}
  ${blocBibliothequeAgents(r.bibliotheque || [])}`;
};

// The library of sub-agents (extensions/agents.json): kept in Athanor, switched on per project like the
// skills. The lead sees its project's ones in .athanor/TEAM.md (athanor equipe).
function blocBibliothequeAgents(biblio) {
  etat.agPerso = {};
  const lignes = biblio.map((a) => { etat.agPerso[a.nom] = a; return `<div class="ext-ligne ${a.actif ? "actif" : ""}" data-ag-ligne="${h(a.nom)}" ${agentCorrespond(a, etat.agRecherche) ? "" : "hidden"}>
      <label class="interrupteur" title="${h(t("Used by this project"))}"><input type="checkbox" data-ag-actif="${h(a.nom)}" ${a.actif ? "checked" : ""}></label>
      <div class="ext-corps"><div class="ext-nom">${h(a.nom)} <span class="etiquette">${a.origine === "chef" ? t("proposed by the lead") : t("created by you")}</span>${a.lecture_seule ? ` <span class="etiquette">${t("read only")}</span>` : ""}</div>
        <div class="ext-desc">${h(a.description)}</div>
        <div class="muet petit">${t("Used by {0} project(s)", [(a.projets || []).length])}</div></div>
      <div class="ext-actions"><button class="btn btn-petit" data-ag="modifier" data-nom="${h(a.nom)}">${t("Edit")}</button><button class="btn btn-petit btn-danger" data-ag="supprimer" data-nom="${h(a.nom)}" title="${h(t("Delete"))}">✕</button></div></div>`; }).join("");
  const vus = biblio.filter((a) => agentCorrespond(a, etat.agRecherche)).length;
  return `<div class="bloc mt-2"><h2>${t("Sub-agent library")} <span class="muet">${biblio.length}</span></h2>
    <p class="muet petit">${t("Kept in Athanor for all your projects: switch on the ones this project uses. It applies from the next cycle.")}</p>
    ${biblio.length ? `<input type="text" id="ag-recherche" value="${h(etat.agRecherche || "")}" placeholder="${h(t("Search a sub-agent: name or what it does"))}" autocomplete="off">
    <div class="ag-biblio">${lignes}<div class="vide" id="ag-aucun" ${vus ? "hidden" : ""}>${t("No sub-agent matches this search.")}</div></div>` : ""}
    ${formulaireAgent()}</div>`;
}
// The search of the library, as it is typed: name and description, without case or accents.
const sansAccents = (s) => String(s || "").normalize("NFD").replace(/[̀-ͯ]/g, "").toLowerCase();
function agentCorrespond(a, q) { q = sansAccents(q).trim(); return !q || sansAccents(a.nom + " " + a.description).includes(q); }
function filtrerAgents() {
  let n = 0;
  document.querySelectorAll("[data-ag-ligne]").forEach((l) => { const ok = agentCorrespond(etat.agPerso[l.dataset.agLigne] || {}, etat.agRecherche); l.hidden = !ok; n += ok; });
  const v = $("#ag-aucun"); if (v) v.hidden = n > 0;
}

// A sub-agent of the library: the lead of each project that uses it sees it in its team and calls it when
// its description fits or when a request names it.
function formulaireAgent() {
  const f = etat.agForm;
  if (!f) return `<div class="ligne mt-2"><button class="btn" data-ag="nouveau">＋ ${t("New sub-agent")}</button>
    <span class="muet petit">${t("A specialist of your own (a translator, a researcher, a level designer…): the lead calls it when its description fits, or when your request names it. The lead can also propose some.")} ${t("Or describe it to Claude in Chat: it asks what it needs and writes it for you.")}</span></div>`;
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
    <p class="muet petit">${f.ancien ? (f.n > 1 ? t("Used by {0} projects: the change applies to all of them.", [f.n]) : "") : t("Saved in the library and switched on in this project.")} ${t("Like the other sub-agents, it works inside the project's sandbox and has its skills, MCP tools and attached files.")}</p>
    <div class="ligne"><button class="btn btn-primaire" data-ag="enregistrer">${t("Save the sub-agent")}</button><button class="btn" data-ag="annuler">${t("Cancel")}</button></div></div>`;
}
async function actionAgent(act, b) {
  if (act === "nouveau") { etat.agForm = { nom: "", description: "", consignes: "", modele: "", lecture_seule: false }; return rendrePage(); }
  if (act === "annuler") { etat.agForm = null; return rendrePage(); }
  if (act === "modifier") {
    const a = (etat.agPerso || {})[b.dataset.nom]; if (!a) return;
    etat.agForm = { ancien: a.nom, nom: a.nom, description: a.description, consignes: a.consignes, modele: a.modele, lecture_seule: a.lecture_seule, n: (a.projets || []).length };
    await rendrePage(); const z = $("#ag-nom"); if (z) z.scrollIntoView({ block: "center" }); return;
  }
  if (act === "supprimer") {
    const a = (etat.agPerso || {})[b.dataset.nom]; if (!a) return;
    if (!(await confirmer(t("Delete “{0}” from the library? It leaves the team of every project that uses it ({1}).", [a.nom, (a.projets || []).length]), { ok: t("Delete"), danger: true }))) return;
    if (await action(api("equipe/agents", "POST", { action: "supprimer", ancien: a.nom }))) rendrePage();
    return;
  }
  if (act === "enregistrer") {
    const f = etat.agForm || {};
    const agent = { nom: $("#ag-nom").value.trim().toLowerCase(), description: $("#ag-description").value, consignes: $("#ag-consignes").value, modele: $("#ag-modele").value, lecture_seule: $("#ag-lecture").checked };
    b.disabled = true;
    const r = await action(api("equipe/agents", "POST", { action: "enregistrer", ancien: f.ancien || "", agent }));
    b.disabled = false;
    if (r) { etat.agForm = null; rendrePage(); }
  }
}

// --- skills & MCP
// A library kept in Athanor (verified catalog, GitHub link, written by hand, pasted configuration) and,
// for the current project, a switch per skill or server. The pilot applies the choice at the next cycle.
const ext = { onglet: "skills", lien: "", trouves: null, voir: null, tests: {}, occupe: "", recherche: { skills: { q: "", res: null }, mcp: { q: "", res: null } }, apercu: null, corr: {}, rapports: [], code: "" };
pages.extensions = async () => {
  const d = await api("extensions");
  ext.rapports = ((await api("extensions/corrections").catch(() => null)) || {}).rapports || [];
  const o = ext.onglet, type = o === "skills" ? "skill" : "mcp";
  const intro = `<p class="intro">${t("Skills give Claude expertise it reads only when it needs it; MCP servers give it new tools (up-to-date documentation, code navigation, a browser…). Install them once in Athanor, then switch on what each project uses: it applies from the next cycle, for the lead and for the parallel workers.")}</p>
  <div class="onglets"><button class="${o === "skills" ? "actif" : ""}" data-ext-onglet="skills">Skills <span class="muet">${d.skills.length}</span></button><button class="${o === "mcp" ? "actif" : ""}" data-ext-onglet="mcp">MCP <span class="muet">${d.mcp.length}</span></button><button class="${o === "plugins" ? "actif" : ""}" data-ext-onglet="plugins">Plugins <span class="muet">${d.plugins || 0}</span></button></div>`;
  if (o === "plugins") return intro + await pagePlugins(d);
  const liste = o === "skills" ? d.skills : d.mcp;
  const cat = (o === "skills" ? d.catalogue.skills : d.catalogue.mcp) || [];
  const occupe = (cle) => ext.occupe === cle ? "disabled" : "";
  const secrets = (x) => champsServeur(x);
  const test = (x) => type === "skill" ? testSkill(x) : testExt(x);
  const voir = (x) => ext.voir && ext.voir.id === x.id && ext.voir.type === type ? `<div class="ext-voir">
      ${type === "skill" ? `<textarea class="code" rows="14" data-ext-texte="${h(x.id)}">${h(ext.voir.texte)}</textarea>
        ${ext.voir.fichiers && ext.voir.fichiers.length > 1 ? `<div class="muet petit">${t("{0}: {1}", [t("Files"), ""])}${ext.voir.fichiers.map((f) => `<code>${h(f)}</code>`).join(" ")}</div>` : ""}
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
        ${secrets(x)}${x.mode_blender ? choixModeBlender(x) : ""}${correctionFaite(x, type)}${test(x)}${voir(x)}
      </div>
      <div class="ext-actions"><button class="btn btn-petit" data-ext-voir="${type}|${h(x.id)}">${ext.voir && ext.voir.id === x.id && ext.voir.type === type ? t("Close") : type === "skill" || !x.plugin ? t("View / edit") : t("View")}</button>
        ${type === "mcp" && (x.type === "http" || x.type === "sse") && !/^https?:\/\/(localhost|127\.|0\.0\.0\.0|\[::1\]|10\.|192\.168\.|172\.(1[6-9]|2\d|3[01])\.)/.test(x.resume || "") ? `<button class="btn btn-petit" data-ext-connexion="${h(x.id)}" title="${h(t("For a server that asks you to sign in (OAuth): once, valid for every project."))}">${t("Sign in")}</button>` : ""}
        ${type === "mcp" ? `<button class="btn btn-petit" data-ext-tester="${h(x.id)}" ${d.projet ? "" : "disabled"} ${ext.tests[x.id] === "…" ? "disabled" : ""}>${t("Test")}</button>`
          : `<button class="btn btn-petit" data-ext-tester-skill="${h(x.id)}" ${ext.tests["skill|" + x.id] === "…" ? "disabled" : ""}>${t("Test")}</button>`}
        <button class="btn btn-petit btn-danger" data-ext-retirer="${type}|${h(x.id)}" title="${h(t("Remove"))}">✕</button></div>
    </div>`).join("");
  const cartes = cat.map((c) => `<div class="ext-carte">
      <div class="ext-nom">${h(t(c.nom))} <span class="muet petit">${h(c.licence)}</span></div>
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
  ${blocRapports()}${blocRecherche}
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
  const r = ext.tests[x.id], c = ext.corr["mcp|" + x.id];
  if (c) return etatCorrection(c, "mcp", x);
  if (!r) return "";
  if (r === "…") return `<div class="muet petit">${x.type === "windows" ? t("Starting the Windows program…") : t("Starting the server in its cage… (the first time, it downloads it: up to two minutes)")}</div>`;
  const cle = /^missing key/.test(r.erreur || "");
  return r.ok ? `<div class="ext-test ok">✓ ${r.outils && r.outils.length ? t("Works: {0} tools ({1})", [r.outils.length, h(r.outils.slice(0, 8).join(", ")) + (r.outils.length > 8 ? "…" : "")]) : t(r.note || "Works.")}</div>`
    : `<div class="ext-test echec">✕ ${h(t(r.erreur || "Failed"))}${r.details && r.details.length ? `<pre class="texte-agent">${h(r.details.join("\n"))}</pre>` : ""}
      ${cle ? "" : boutonCorriger("mcp", x.id)}</div>`;
}
// Blender's server: how Claude works with it (blender_apercu.go)
function choixModeBlender(x) {
  const modes = [["apercu", t("Background + preview window"), t("Claude works in the background (no MCP tools: the cheapest); each run is shown in a Blender window that reloads by itself, one per folder Claude works in (on Claude's desktop when that option is on).")],
    ["arriere-plan", t("Background only"), t("Claude works in the background, no window opens.")],
    ["direct", t("In Blender's window (MCP)"), t("The pilot opens Blender before each cycle and Claude drives it through the MCP server: you see each step, it costs more.")]];
  if (!x.apercu_possible) modes.shift(); // a Mac: Claude runs Blender itself, no preview window
  const m = x.mode_blender || "apercu", choisi = modes.find((y) => y[0] === m) || modes[0];
  return `<div class="ext-mode-blender"><span class="petit">${t("How Claude works with Blender:")}</span>
    <div class="segments">${modes.map((y) => `<button class="btn btn-petit ${y[0] === m ? "btn-primaire" : ""}" data-ext-mode-blender="${h(x.id)}|${y[0]}">${y[1]}</button>`).join("")}</div>
    <div class="muet petit">${choisi[2]}</div></div>`;
}
// --- "Fix with Claude" (ext_corriger.go, ext_skill.go): an extension whose test fails here
function boutonCorriger(type, id) {
  return `<div class="ligne mt-1"><button class="btn btn-petit btn-primaire" data-ext-corriger="${type}|${h(id)}">✨ ${t("Fix with Claude")}</button>
    <span class="muet petit">${type === "mcp" ? t("Claude looks for a configuration that works on this system; it is tested before it is kept, and written for everyone.") : t("Claude rewrites a copy of the skill so that it runs on every system; you see the changes before they replace anything.")}</span></div>`;
}
function testSkill(x) {
  const c = ext.corr["skill|" + x.id]; if (c) return etatCorrection(c, "skill", x);
  const r = ext.tests["skill|" + x.id]; if (!r) return "";
  if (r === "…") return `<div class="muet petit">${t("Checking the skill and its scripts…")}</div>`;
  if (r.erreur) return `<div class="ext-test echec">✕ ${h(t(r.erreur))}</div>`;
  const pb = r.problemes || [];
  const liste = pb.length ? `<ul class="m0 petit">${pb.map((p) => `<li>${p.grave ? "✕" : "⚠"} <code>${h(p.fichier)}</code> ${h(t(p.cle || p.message, p.args || []))}</li>`).join("")}</ul>` : "";
  if (r.ok && !pb.length) return `<div class="ext-test ok">✓ ${t("Runs here: {0} script(s) checked", [r.scripts || 0])}</div>`;
  return `<div class="ext-test ${r.ok ? "ok" : "echec"}">${r.ok ? "✓ " + t("Runs here; may fail on another system:") : "✕ " + t("Will not run as it is on this computer:")}${liste}${boutonCorriger("skill", x.id)}</div>`;
}
function etatCorrection(c, type, x) {
  const etapes = { test: c.essai ? t("testing Claude's fix ({0}/3)…", [c.essai]) : t("testing it as it is…"), claude: type === "mcp" ? t("Claude looks for the cause and a fix ({0}/3)…", [c.essai || 1]) : t("Claude rewrites a copy of the skill…") };
  if (c.encours) return `<div class="ext-test"><span class="chat-points"><i></i><i></i><i></i></span> ${etapes[c.etape] || t("Working…")} <span class="muet petit">${t("It takes a few minutes: you can go on meanwhile.")}</span></div>`;
  const r = c.resultat || {};
  const fermer = `<button class="btn btn-petit" data-ext-corr-fermer="${type}|${h(x.id)}">${t("Close")}</button>`;
  const athanor = r.athanor ? `<div class="petit mt-1">🛠 ${t("Athanor's own way of running it is at fault: {0}", [h(r.athanor)])} <span class="muet">${t("(written for the next version of Athanor)")}</span></div>` : "";
  if (r.deja) return `<div class="ext-test ok">✓ ${t("It works already: nothing to fix.")} ${fermer}</div>`;
  if (r.manque) return `<div class="ext-test echec">✕ ${t("A program is missing on this computer: {0}", [h(r.manque)])}${r.explication ? `<div class="petit">${h(r.explication)}</div>` : ""}
    <div class="ligne mt-1"><a class="btn btn-petit" href="#sante">${t("Setup & health")}</a>${fermer}</div></div>`;
  if (type === "mcp" && r.ok) return `<div class="ext-test ok">✓ ${t("Fixed by Claude: it works now ({0} tools).", [(r.outils || []).length])}${r.explication ? `<div class="petit">${h(r.explication)}</div>` : ""}
    <div class="muet petit">${t("Kept in this computer's library (Undo puts back the previous one) and written for everyone in logs/corrections.")}</div>${athanor}<div class="ligne mt-1">${fermer}</div></div>`;
  if (type === "skill" && r.fichiers && r.fichiers.length) {
    const reste = (r.apres || []).filter((p) => p.grave);
    return `<div class="ext-test ${r.ok ? "ok" : "echec"}">${r.ok ? "✓ " + t("Claude's fix passes the test. Look at the changes, then apply them.") : "⚠ " + t("Claude's fix still fails the test: {0}", [h(reste.map((p) => p.fichier + ": " + t(p.cle || p.message, p.args || [])).join("; "))])}
      ${r.explication ? `<div class="petit">${h(r.explication)}</div>` : ""}
      <div class="muet petit">${t("Files changed: {0}", [r.fichiers.map((f) => h(f)).join(", ")])}</div>
      <pre class="texte-agent diff">${diffColore(r.diff || "")}</pre>${athanor}
      <div class="ligne mt-1"><button class="btn btn-petit btn-primaire" data-ext-corr-appliquer="${h(x.id)}">${t("Apply the fix")}</button>${fermer}</div></div>`;
  }
  const essais = (r.essais || []).map((e, i) => `<li>${h(e.explication || t("Try {0}", [i + 1]))} → <span class="erreur">${h(t(e.erreur || ""))}</span></li>`).join("");
  return `<div class="ext-test echec">✕ ${h(t(r.erreur || "No fix found"))}${r.explication ? `<div class="petit">${h(r.explication)}</div>` : ""}${essais ? `<ul class="m0 petit">${essais}</ul>` : ""}${athanor}
    <div class="ligne mt-1"><button class="btn btn-petit" data-ext-corriger="${type}|${h(x.id)}">${t("Try again")}</button>${fermer}</div></div>`;
}
function diffColore(d) {
  return h(d).split("\n").map((l) => l.replace(/\r$/, "␍")).map((l) => /^\+(?!\+\+)/.test(l) ? `<span class="ajout">${l}</span>` : /^-(?!--)/.test(l) ? `<span class="retrait">${l}</span>` : /^@@/.test(l) ? `<span class="muet">${l}</span>` : l).join("\n");
}
function correctionFaite(x, type) {
  if (!x.correction) return "";
  return `<div class="muet petit ext-corrige">✨ ${x.correction.le ? t("Fixed by Claude on {0}", [h(x.correction.le)]) : t("Fixed by Claude")}${x.correction.explication ? ": " + h(x.correction.explication) : ""}
    <button class="btn btn-petit" data-ext-corr-annuler="${type}|${h(x.id)}">${t("Undo the fix")}</button></div>`;
}
// the fixes written for everyone (logs/corrections): into a copy of Athanor's code, then commit and push
function blocRapports() {
  const l = ext.rapports || []; if (!l.length) return "";
  const lignes = l.slice(0, 12).map((r) => `<li><b>${h(r.nom || r.id)}</b> <span class="etiquette">${h(r.genre === "skill" ? "skill" : "MCP")}</span> <span class="muet">${h(r.systeme)} · ${h(r.le)}</span>
      ${r.catalogue ? ` <span class="etiquette">${t("verified catalog")}</span>` : ""}${r.apres || (r.fichiers || []).length ? "" : ` <span class="muet">${t("no fix found")}</span>`}
      ${r.explication ? `<div class="petit">${h(r.explication)}</div>` : ""}${r.athanor ? `<div class="petit">🛠 ${h(r.athanor)}</div>` : ""}</li>`).join("");
  return `<div class="bloc"><h2>${t("Fixes for everyone")} <span class="muet">${l.length}</span></h2>
    <p class="muet petit">${t("What Claude fixed or found on this computer, kept in logs/corrections of Athanor's folder. The verified catalog's fixes can be written into your copy of Athanor's code: commit and push it, and every Athanor gets them.")}</p>
    <ul class="ext-rapports">${lignes}</ul>
    <label class="champ mt-1"><span>${t("Your copy of Athanor's code")}</span>${champDossier("ext-code", ext.code || "", "", false, t("Choose the folder of Athanor's code"))}</label>
    <div class="ligne mt-1"><button class="btn" data-ext-rapports-appliquer>${t("Write the catalog's fixes into it")}</button></div></div>`;
}
async function suivreCorrection(cle) {
  const [type, id] = cle.split("|");
  for (;;) {
    await new Promise((ok) => setTimeout(ok, 2500));
    const c = await api("extensions/corriger?type=" + encodeURIComponent(type) + "&id=" + encodeURIComponent(id)).catch(() => null);
    if (!c) continue;
    ext.corr[cle] = c;
    if (etat.page === "extensions") rendrePage();
    if (!c.encours) {
      if (type === "mcp" && c.resultat && c.resultat.ok && !c.resultat.deja) { delete ext.tests[id]; toast(t("Fixed by Claude: it works now ({0} tools).", [(c.resultat.outils || []).length]), "ok"); }
      return;
    }
  }
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
  const b = ev.target.closest("[data-ext-rechercher], [data-ext-apercu], [data-ext-installer-res], [data-ext-onglet], [data-ext-installer], [data-ext-chercher], [data-ext-installer-trouves], [data-ext-creer], [data-ext-mcp-ajouter], [data-ext-voir], [data-ext-enregistrer], [data-ext-mcp-enregistrer], [data-ext-tester], [data-ext-retirer], [data-ext-cle-ok], [data-ext-corriger], [data-ext-corr-fermer], [data-ext-corr-appliquer], [data-ext-corr-annuler], [data-ext-tester-skill], [data-ext-rapports-appliquer], [data-ext-mode-blender]");
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
  if (d.extCorriger) {
    const [type, id] = d.extCorriger.split("|");
    const r = await action(api("extensions/corriger", "POST", { type, id }));
    if (r) { ext.corr[d.extCorriger] = { encours: true, etape: "test", essai: 0 }; rendrePage(); suivreCorrection(d.extCorriger); }
    return;
  }
  if (d.extModeBlender) {
    const [id, mode] = d.extModeBlender.split("|");
    const r = await action(api("extensions/mode-blender", "POST", { id, mode }));
    if (r) rendrePage();
    return;
  }
  if (d.extCorrFermer) { delete ext.corr[d.extCorrFermer]; return rendrePage(); }
  if (d.extCorrAppliquer) {
    b.disabled = true;
    const r = await action(api("extensions/correction/appliquer", "POST", { id: d.extCorrAppliquer }));
    if (r && r.ok) { delete ext.corr["skill|" + d.extCorrAppliquer]; delete ext.tests["skill|" + d.extCorrAppliquer]; toast(t("The fix is applied."), "ok"); }
    else if (r) toast(t(r.erreur || "Failed"), "erreur");
    return rendrePage();
  }
  if (d.extCorrAnnuler) {
    const [type, id] = d.extCorrAnnuler.split("|");
    if (!(await confirmer(t("Undo Claude's fix of “{0}”? What it replaced comes back.", [id]), { ok: t("Undo the fix") }))) return;
    const r = await action(api("extensions/correction/annuler", "POST", { type, id }));
    if (r && r.ok === false) toast(t(r.erreur || "Failed"), "erreur");
    delete ext.tests[id]; delete ext.tests["skill|" + id];
    return rendrePage();
  }
  if (d.extTesterSkill) {
    const cle = "skill|" + d.extTesterSkill; ext.tests[cle] = "…"; delete ext.corr[cle]; await rendrePage();
    ext.tests[cle] = await api("extensions/tester-skill", "POST", { id: d.extTesterSkill }).catch((e) => ({ ok: false, erreur: e.message, problemes: [] }));
    return rendrePage();
  }
  if (d.extRapportsAppliquer !== undefined) {
    ext.code = ($("#ext-code") || {}).value || "";
    b.disabled = true;
    const r = await action(api("extensions/corrections/appliquer", "POST", { dossier: ext.code }));
    b.disabled = false;
    if (r) await confirmer(r.texte || "", { titre: t("Fixes for everyone"), ok: t("OK"), annuler: "" });
    return rendrePage();
  }
  if (d.extTester) {
    const id = d.extTester; ext.tests[id] = "…"; delete ext.corr["mcp|" + id]; await rendrePage();
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
  const ag = ev.target.closest("[data-ag-actif]");
  if (ag) { const r = await action(api("equipe/agents", "POST", { action: "activer", ancien: ag.dataset.agActif, actif: ag.checked })); if (!r) ag.checked = !ag.checked; else rendrePage(); return; }
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
  if (id === "hc") return ajouterPiecesHub(fichiers); // the home conversation: its own types and size
  const l = etat.pieces[id] = etat.pieces[id] || [];
  for (const f of fichiers) {
    if (f.size > TAILLE_PIECE_MAX) { toast(t("{0} is too large (50 MB at most).", [f.name]), "erreur"); continue; }
    if (!l.some((x) => x.name === f.name && x.size === f.size)) l.push(f);
  }
  const ul = $("#" + id + "-liste"); if (ul) ul.innerHTML = listePieces(id);
}
// one file into the workshop ({nom, chemin, taille}); cible: "demande", a queue file or "chat" (api_pieces.go)
async function envoyerPiece(f, cible, projet) {
  const r = await fetch("/api/pieces?p=" + encodeURIComponent(projet) + "&cible=" + encodeURIComponent(cible), {
    method: "POST", headers: { "X-Athanor": CSRF, "X-Nom-Fichier": encodeURIComponent(f.name) }, body: f });
  const d = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(d.erreur_key ? t(d.erreur_key, d.args || []) : t("Error {0}", [r.status]));
  return d;
}
// cible: "demande" (REQUEST.md) or a queue file; projet: the project's name
async function envoyerPieces(id, cible, projet) {
  const fichiers = etat.pieces[id] || [];
  let ok = 0;
  for (const f of fichiers) {
    try { await envoyerPiece(f, cible, projet); ok++; } catch (e) { toast(t("{0} could not be attached: {1}", [f.name, e.message]), "erreur"); }
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
    ${boutonDefil("journal")}
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
  const haut = el.scrollTop;
  etat.rendJournal = true; setTimeout(() => { etat.rendJournal = false; }, 0);
  el.innerHTML = items.slice(-1500).map((e) => f.brut
    ? `<div class="ev g-${h(e.genre)}"><span class="h">${h(e.heure)}</span><span class="m"></span><span class="t">${h(e.brut)}</span></div>`
    : `<div class="ev g-${h(e.genre)} ${e.sous_agent ? "sous" : ""} ${e.detail ? "cliquable" : ""}" data-n="${e.n}"><span class="h">${h(e.heure)}</span><span class="m"></span><span class="t">${e.sous_agent ? "↳ " : ""}${h(t(e.titre))}</span></div>${etat.ouverts.has(e.n) && e.detail ? `<div class="ev-detail">${h(e.detail)}</div>` : ""}`
  ).join("");
  if (etat.defilAuto.journal) el.scrollTop = el.scrollHeight;
  else el.scrollTop = Math.min(haut, el.scrollHeight);
  if (!el.dataset.suivi) {
    // scrolling up pauses the auto-scroll, back at the end resumes it (the switch shows it)
    el.dataset.suivi = "1";
    el.addEventListener("scroll", () => {
      if (etat.rendJournal) return;
      const auBout = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
      if (auBout !== etat.defilAuto.journal) { etat.defilAuto.journal = auBout; ecrireDefilAuto(); majBoutonDefil("journal"); }
    }, { passive: true });
  }
}
function majBoutonDefil(quoi) { const b = document.querySelector(`[data-defil="${quoi}"]`); if (b) b.outerHTML = boutonDefil(quoi); }

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
    <button class="btn" id="st-coffre" title="${h(t(v.windows && v.natif ? "Native Windows mode, no sandbox: the granted test value must be seen, the code key can be opened by DPAPI, the infra value must stay unreadable." : "A short Claude session tries to read three test values of the vault: only the one granted to its project may reach it."))}">${t("Test the vault (2 min)")}</button>
    <button class="btn" id="st-mcp-hub" title="${h(t("A short Claude session uses the hub's tools like a pilot: it asks for a test key, Athanor accepts it as you would, and the session must read it without restarting."))}">${t("Test the hub's MCP (2 min)")}</button>
    <button class="btn" id="st-agent-serveurs" title="${h(t("On Athanor's test container (Docker), a real Claude session gets a Servers task: it reads the sheet, its forbidden attempts must be refused, its plan is accepted for you, the change is made and the RUNBOOK completed."))}">${t("Test the Servers agent (5 min)")}</button>
    <button class="btn" id="st-recompiler" title="${h(t("After a repair of its Go code (app/): shows the change, checks it, rebuilds, runs the autotest, and only then replaces the executable, keeping the previous one."))}">${t("Rebuild Athanor")}</button>
  </div>
  ${v.windows && v.natif ? `<p class="petit attention mt-1" id="st-exception-natif">${t("Native Windows mode: no sandbox keeps Claude away from Athanor's data (settings, vault, modules). The chained audit journal shows any change (Verify), and approvals ask for your password.")}</p>` : ""}
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
  CLAUDE_AUTH: ["connexion", "Connect with"], CLAUDE_CODE_OAUTH_TOKEN: ["connexion", "Connection token"],
  ANTHROPIC_API_KEY: ["connexion", "API key"], API_BUDGET_USD: ["connexion", "API budget"], API_SPENT_USD: ["connexion", "Already spent"],
  MODEL: ["modeles", "Model of the lead"], FALLBACK_MODEL: ["modeles", "Fallback model"],
  FRAMING_MODEL: ["modeles", "Model of the framing"], WORKER_MODEL: ["modeles", "Model of the parallel workers"],
  SUBAGENT_MODEL: ["modeles", "Model of the sub-agents"], CHEAP_MODEL: ["modeles", "Cheap model"], EFFORT: ["modeles", "Thinking effort"],
  CLOSE_AFTER_MILESTONE: ["marche", "One milestone per cycle"], RELAY_CONTEXT_K: ["marche", "Fresh session beyond"],
  MAX_HOURS: ["marche", "Work time limit"], CYCLE_MAX_MIN: ["marche", "Longest cycle"], IDLE_MAX_MIN: ["marche", "Silent cycle stopped after"],
  FRAMING_MAX_MIN: ["marche", "Longest framing"], RTK: ["marche", "Compressed command outputs"], PONYTAIL: ["marche", "Simplest code (Ponytail)"],
  KEEP_AWAKE: ["marche", "Keep the PC awake"],
  LANGUAGE: ["avance", "Language of Claude's documents"], TZ: ["avance", "Time zone of the logs"],
  QUOTA_WAIT_MIN: ["avance", "Wait when the quota's end is unknown"], ERROR_WAIT_MIN: ["avance", "Wait after an error"],
};
const GROUPES_REGLAGES = [["connexion", "Connection"], ["modeles", "Models"], ["marche", "How the pilot works"]];
// the "i" of a setting: what a lower or a higher value changes (the line under its name stays short)
const DETAILS_REGLAGES = {
  MODEL: ["Stronger (opus): better plans, decisions and fixes; each turn uses more quota.", "Lighter (sonnet): more cycles for the same quota, enough for simpler or well-planned projects."],
  FALLBACK_MODEL: ["Set: when the main model's quota runs out, the pilot carries on with this model until the quota reopens.", "Empty: the pilot pauses until the quota reopens."],
  FRAMING_MODEL: ["The framing writes the plan that every milestone follows: a strong model here pays off for the whole project, for the quota of a single cycle.", "Empty: the lead's model."],
  WORKER_MODEL: ["Each parallel worker carries out one well-written milestone sheet: execution work, where sonnet is usually enough.", "Stronger: fewer mistakes on hard milestones, but every worker uses more quota, and several run at once."],
  SUBAGENT_MODEL: ["Empty: the lead's model. auto: the lead picks one for each call (a light model to read, a strong one to review).", "A lighter model here saves a lot: a sub-agent starts from zero and rereads the project every time."],
  CHEAP_MODEL: ["Used by the executor sub-agent for mechanical work (long outputs, searches, edits repeated over many files): the cheapest model is enough.", "Empty: the same model as the other sub-agents, which uses more quota for the same work."],
  EFFORT: ["Higher (high, xhigh, max): Claude thinks longer before acting; better on hard problems, slower, and it uses more quota.", "Lower (low, medium): faster and cheaper, enough for simple or well-planned work. Empty: the model's normal effort."],
  CLOSE_AFTER_MILESTONE: ["On: the pilot closes the cycle a few minutes after a milestone's commit, and the next milestone starts with a fresh, light context (cheaper turns).", "Off: the session ends when Claude stops on its own, which costs more if it lingers."],
  RELAY_CONTEXT_K: ["Lower: a fresh session takes over more often; cheaper turns, but each new session rereads its notes.", "Higher: longer sessions that keep more in mind, but every turn costs more as the context grows. 0: never; Claude Code may then compact the context by itself and blur the thread."],
  MAX_HOURS: ["Only the time the pilot works counts: a pause, a quota wait, a stopped pilot or Athanor closed do not count. Past the limit, the pilot stops, whatever the project's state.", "Unlimited (0, the default): the pilot works until the project is delivered, which stops it by itself, or until you stop it.", "A limit only caps what a project can use (720 = 30 days of work). A project can have its own: Manage › Working mode."],
  CYCLE_MAX_MIN: ["Lower: a cycle that drags is cut sooner (the work done stays, the milestone resumes), but a long milestone (a big build, a game engine) may be cut before its end.", "Higher: more patience for long milestones; a stuck cycle wastes more time."],
  IDLE_MAX_MIN: ["A cycle with no activity for this long is stopped, then resumed.", "Lower: a frozen session is caught sooner, but a long silent command (a build, tests) may be cut. Higher: the opposite."],
  FRAMING_MAX_MIN: ["Lower: the plan is ready sooner, but less precise on a large project (code skimmed, vaguer milestone sheets, a quick review).", "Higher: a more precise plan. It is a ceiling, not a duration: a framing that ends sooner stops sooner. Past it, one short cycle (15 min at most) finishes the plan.", "Advice: 25 for a small project, 45 to 60 for a large code base or large attached files. A project can have its own: Manage › Working mode."],
  RTK: ["On: command outputs are compressed before Claude reads them; fewer tokens, the signal is kept.", "Off: full outputs, only to investigate a problem; it uses more quota."],
  PONYTAIL: ["On: each milestone is built with the smallest complete change, never less than the request; less code, fewer bugs, less quota.", "Off: Claude may add structure and options nobody asked for."],
  QUOTA_WAIT_MIN: ["Only when Claude does not say when the quota reopens: the pilot waits this long, then tries again.", "Lower: resumes sooner, but may try several times for nothing. Higher: fewer tries, maybe a longer idle time."],
  ERROR_WAIT_MIN: ["After an error or a stopped cycle, the pilot waits this long, then twice as long at each new error in a row, up to a ceiling.", "Lower: retries sooner. Higher: lets a passing outage (network, service) end before the next try."],
};
function detailReglage(cle) {
  const nom = (REGLAGES[cle] || [])[1];
  return choisir((DETAILS_REGLAGES[cle] || []).map((l) => "• " + t(l)).join("\n"), [{ id: "ok", texte: t("OK"), primaire: true }], { titre: nom ? t(nom) : cle });
}
// a work time limit (MAX_HOURS, in hours of work) for its button: 0 is unlimited
const libelleDuree = (n) => n === 0 ? t("Unlimited") : { 72: t("3 days"), 168: t("1 week"), 336: t("2 weeks"), 720: t("30 days") }[n] || t("{0} h", [n]);
const boutonDetail = (cle) => DETAILS_REGLAGES[cle] ? `<button type="button" class="chat-info" data-detail-reglage="${h(cle)}" title="${h(t("What a lower or a higher value changes"))}" aria-label="${h(t("What a lower or a higher value changes"))}">i</button>` : "";
const EFFORTS = ["", "low", "medium", "high", "xhigh", "max"];
// the connection (connexion_claude.go): the subscription's token or an API key; each one's settings shown
// with it only
const ORDRE_CONNEXION = ["CLAUDE_AUTH", "CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "API_BUDGET_USD", "API_SPENT_USD"];
const MODE_CONNEXION = { CLAUDE_CODE_OAUTH_TOKEN: "subscription", ANTHROPIC_API_KEY: "api", API_BUDGET_USD: "api", API_SPENT_USD: "api" };
function modeConnexion(v) { return String(v || "").toLowerCase() === "api" ? "api" : "subscription"; }
function majModeConnexion(mode) {
  document.querySelectorAll("[data-connexion]").forEach((x) => { x.hidden = x.dataset.connexion !== mode; });
}

// modeles: the proposals, from the server (the aliases, then the models this PC has used: a new model
// appears by itself once a cycle has run on it); any other name can be typed in the field
function champReglage(l, modeles) {
  const id = "env-" + l.cle;
  const choix = (valeurs, nom) => `<input type="text" id="${id}" data-cle="${h(l.cle)}" value="${h(l.valeur)}"><div class="choix">${valeurs.map((m) => `<button type="button" data-pour="${id}" data-val="${h(m)}" class="${l.valeur === m ? "actif" : ""}">${h(nom(m))}</button>`).join("")}</div>`;
  const nomModele = (m) => m === "auto" ? t("chosen by the lead") : m || t("account default");
  if (["MODEL", "FALLBACK_MODEL", "FRAMING_MODEL", "WORKER_MODEL", "CHEAP_MODEL"].includes(l.cle)) return choix(["", ...modeles], nomModele);
  if (l.cle === "SUBAGENT_MODEL") return choix(["", ...modeles, "auto"], nomModele);
  if (l.cle === "EFFORT") return choix(EFFORTS, (m) => m || t("normal"));
  if (l.cle === "MAX_HOURS") return choix(["72", "168", "336", "720", "0"], (m) => libelleDuree(Number(m)));
  if (l.cle === "CLAUDE_AUTH") return choix(["subscription", "api"], (m) => t(m === "api" ? "API key (paid per use)" : "Claude subscription (token)"));
  if (l.cle === "KEEP_AWAKE") return choix(["1", "screen", "0"], (m) => t({ "1": "PC awake", screen: "PC and screen awake", "0": "no" }[m]));
  if (["RTK", "PONYTAIL", "UNREAL_MCP", "CLOSE_AFTER_MILESTONE"].includes(l.cle) && (l.valeur === "0" || l.valeur === "1")) return `<label class="interrupteur m0"><input type="checkbox" id="${id}" data-cle="${h(l.cle)}" ${l.valeur === "1" ? "checked" : ""}> <span>${l.valeur === "1" ? t("enabled") : t("disabled")}</span></label>`;
  return `<input type="${l.secret && !etat.envSecrets ? "password" : "text"}" id="${id}" data-cle="${h(l.cle)}" value="${h(l.valeur)}" ${l.secret ? 'autocomplete="off"' : ""}>`;
}

pages.reglages = async () => {
  const onglet = etat.ongletReglages === "claude" ? "claude" : "options";
  const tete = `<div class="onglets"><button class="${onglet === "options" ? "actif" : ""}" data-onglet-reglages="options">${t("Options")}</button><button class="${onglet === "claude" ? "actif" : ""}" data-onglet-reglages="claude">${t("Claude")}</button></div>`;
  if (onglet === "options") return tete + await rendreOptions();
  const [lignes, mod] = await Promise.all([api("env" + (etat.envSecrets ? "?secrets=1" : "")), api("modeles").catch(() => ({ modeles: [] }))]);
  const modeles = mod.modeles || [];
  const mode = modeConnexion((lignes.find((l) => l.cle === "CLAUDE_AUTH") || {}).valeur);
  const ligne = (l) => {
    const nom = (REGLAGES[l.cle] || [])[1];
    const m = MODE_CONNEXION[l.cle];
    return `<div class="reglage" ${m ? `data-connexion="${m}" ${m !== mode ? "hidden" : ""}` : ""}><div>${nom ? `<div class="nom-reglage">${h(t(nom))} <code class="cle-env">${h(l.cle)}</code>${boutonDetail(l.cle)}</div>` : `<div class="cle">${h(l.cle)}</div>`}<div class="aide">${h(t(l.aide))}</div></div><div>${champReglage(l, modeles)}</div></div>`;
  };
  const rang = (c) => { const i = ORDRE_CONNEXION.indexOf(c); return i < 0 ? 99 : i; };
  const groupe = (g) => lignes.filter((l) => (REGLAGES[l.cle] || ["avance"])[0] === g).sort((a, b) => g === "connexion" ? rang(a.cle) - rang(b.cle) : 0);
  const avertAPI = `<div class="avert-connexion petit" data-connexion="api" ${mode !== "api" ? "hidden" : ""}><b>⚠ ${t("With an API key")}</b> ${t("the plugins and connectors of your claude.ai account (Gmail, Drive… added in the Claude app) do not work. The Skills, the MCP servers and every extension of Athanor work the same. Each run of Claude is billed to the API account: Overview shows the spend, and the pilot waits once the budget is spent.")}</div>`;
  const actif = etat.vue && etat.vue.actif;
  const avance = groupe("avance");
  return `${tete}
  <div class="ligne reglages-tete">
    <p class="muet m0 croit">${t("The pilot reads these settings when it starts (a new token or API key: at once). The token and the API key give access to your Claude account: never share them.")}</p>
    <label class="interrupteur m0"><input type="checkbox" id="rg-secrets" ${etat.envSecrets ? "checked" : ""}> <span>${t("Show secrets")}</span></label>
    <button class="btn btn-primaire" id="rg-enregistrer">${t("Save")}</button>
    ${actif ? `<button class="btn" id="rg-redemarrer" title="${h(t("Saves, lets the current milestone finish, then restarts the pilot with the new settings."))}">${t("Save and restart after cycle")}</button>` : ""}
  </div>
  ${GROUPES_REGLAGES.map(([g, titre]) => { const l = groupe(g); return l.length ? `<div class="bloc"><h2>${t(titre)}</h2>${l.map(ligne).join("")}${g === "connexion" ? avertAPI : ""}</div>` : ""; }).join("")}
  ${avance.length ? `<div class="bloc"><details><summary>${t("Advanced")} <span class="muet petit">${t("{0} setting(s)", [avance.length])}</span></summary>${avance.map(ligne).join("")}</details></div>` : ""}`;
};

function lireEnvFormulaire() {
  const v = {};
  document.querySelectorAll("[data-cle]").forEach((i) => { v[i.dataset.cle] = i.type === "checkbox" ? (i.checked ? "1" : "0") : i.value; });
  return v;
}

// --- options (saved as soon as a switch or a field changes): the "Options" tab of the settings
// --- several computers on a project (pcs.go, api_pcs.go): who works with this one, on what
etat.pcsVues = {}; etat.pcsLus = {}; etat.pcsBascule = {}; etat.pcsGen = {};
// pcsDuProjet: what the page knows (read again in the background every 30 s; the page follows)
function pcsDuProjet(nom) {
  const lu = etat.pcsLus[nom] || 0;
  if (Date.now() - lu > 30000) {
    etat.pcsLus[nom] = Date.now();
    const gen = etat.pcsGen[nom] || 0;
    api("pcs").then((v) => {
      // the switch moved meanwhile: this reading is from before it, its answer is the switch's own
      if (etat.pcsBascule[nom] || gen !== (etat.pcsGen[nom] || 0)) return;
      const avant = JSON.stringify(etat.pcsVues[nom] || null);
      etat.pcsVues[nom] = v;
      if (JSON.stringify(v) !== avant && etat.page === "suivi" && etat.projet === nom) rendrePage();
    }).catch(() => { /* GitHub unreachable: shown at the next reading */ });
  }
  return etat.pcsVues[nom];
}
// --- the direct link between the computers (equipe_directe.go): read every 10 s
etat.directVues = {}; etat.directLus = {};
function directDuProjet(nom) {
  const vite = etat.directVues[nom] && (etat.directVues[nom].etat === "attente" || etat.directVues[nom].code || (etat.directVues[nom].demandes || []).length || etat.directVues[nom].passation);
  if (Date.now() - (etat.directLus[nom] || 0) > (vite ? 3000 : 10000)) {
    etat.directLus[nom] = Date.now();
    api("pcs-direct").then((v) => {
      const avant = JSON.stringify(etat.directVues[nom] || null);
      etat.directVues[nom] = v.direct || null;
      if (JSON.stringify(etat.directVues[nom]) !== avant && etat.page === "suivi" && etat.projet === nom) rendrePage();
    }).catch(() => { /* shown at the next reading */ });
  }
  return etat.directVues[nom];
}
function pctQuota(x) { return x === undefined || x === null ? "—" : Math.round(x * 100) + " %"; }
function blocDirect(nom) {
  const d = directDuProjet(nom);
  const intro = `<p class="intro petit">${t("Direct link: the computers of the team see each other live (what each one builds, its quota, its cost) through a link of their own, separate from the remote access: a team member can never reach this computer's panel. The code still goes through GitHub.")}</p>`;
  if (!d || d.etat === "fini") {
    return `<div class="bloc" id="bloc-direct"><h2>${t("Team, live")}</h2>${intro}
      ${d && d.erreur ? `<p class="petit"><span class="attention">✖ ${h(t(d.erreur))}</span></p>` : ""}
      <div class="ligne"><button class="btn" type="button" data-direct="maitre">${t("Be the master of the team")}</button></div>
      <div class="ligne mt-1"><input type="text" id="direct-invitation" placeholder="https://….trycloudflare.com#123456"><button class="btn" type="button" data-direct="rejoindre">${t("Join with an invitation")}</button></div>
    </div>`;
  }
  const lignes = (d.noeuds || []).map((n) => {
    const p = n.presence || {}, st = n.stats || {};
    const marques = [n.maitre ? `<span class="badge">${t("master")}</span>` : "", st.ponytail ? `<span class="badge">Ponytail</span>` : "", st.eco ? `<span class="badge">${t("eco mode")}</span>` : ""].join(" ");
    const etatN = n.vu < 0 ? `<span class="muet">${t("Paired, not seen yet")}</span>` : !n.en_direct ? `<span class="muet">${h(t("last seen {0} s ago", [n.vu]))}</span>` : etatPC({ ...p, en_ligne: true });
    return `<tr><td class="etroit"><span class="point-appareil ${n.en_direct ? "en-ligne" : ""}"></span></td>
      <td><b>${h(p.nom || "")}</b> ${marques}</td>
      <td>${etatN}</td>
      <td class="petit">${n.vu < 0 ? "" : `${t("quota {0} · week {1}", [pctQuota(st.session), pctQuota(st.semaine)])}<div class="muet">${t("{0} cycles · {1} $ · {2} tokens", [st.cycles || 0, (st.cout || 0).toFixed(2), fmtTokens(st.sortie || 0)])}</div>`}</td>
      ${d.role === "maitre" && !n.maitre ? `<td class="etroit">${n.en_direct && !d.passation ? `<button class="btn btn-petit" type="button" data-direct="transmettre" data-id="${h(p.id || "")}" data-nom="${h(p.nom || "")}" title="${h(t("This computer hands the team over: that one becomes the master, this one one of its nodes."))}">${t("Make it the master")}</button> ` : ""}<button class="btn btn-petit" type="button" data-direct="retirer" data-id="${h(p.id || "")}">${t("Remove")}</button></td>` : "<td></td>"}</tr>`;
  }).join("");
  let tete = "";
  if (d.role === "maitre") {
    const etatM = d.etat === "pret" ? `<span class="ok">✔ ${t("This computer is the master: the others join it with an invitation.")}</span>`
      : d.etat === "erreur" ? `<span class="attention">✖ ${h(t(d.erreur || ""))}</span>` : `<span class="muet">⏳ ${t("Opening the team's link…")}</span>`;
    const invitation = d.code && d.adresse ? `<div class="bloc-code mt-1"><code id="direct-invit">${h(d.adresse)}#${h(d.code)}</code> <button class="btn btn-petit" type="button" data-direct="copier">${t("Copy")}</button>
      <div class="muet petit">${t("Paste it on the other computer (Overview › Team, live › Join), then accept it here. It works once, for 10 minutes.")} ${t("Only a computer you know: anyone with the invitation must still be accepted here.")}</div></div>` : "";
    const demandes = (d.demandes || []).map((x) => `<div class="ligne mt-1 demande-direct"><span>⚠ <b>${h(t("{0} wants to join the team", [x.nom || "?"]))}</b>${x.ip ? ` <span class="muet petit">${h(x.ip)}</span>` : ""}</span><span class="espace"></span>
      <button class="btn btn-primaire" type="button" data-direct="accepter" data-id="${h(x.ref)}">${t("Accept")}</button> <button class="btn btn-petit" type="button" data-direct="refuser" data-id="${h(x.ref)}">${t("Refuse")}</button></div>`).join("");
    const at = d.atelier;
    const atelier = `<div class="mt-1"><label class="interrupteur m0" title="${h(t("The plan, the request, the log and the notes go from computer to computer through this link, never through GitHub (for a public repository): .athanor/ then stays out of the commits on every computer. GitHub still carries the code."))}"><input type="checkbox" data-direct-bascule="atelier" ${d.atelier_lien ? "checked" : ""}><span><b>${t("Workshop through the direct link")}</b> <span class="muet petit">${t("not on GitHub")}</span></span></label>
      ${d.atelier_lien && at ? `<div class="muet petit">${at.version ? h(t("Team workshop: version {0}, {1} files, last change by {2} ({3})", [at.version, at.fichiers, at.par || "?", at.quand ? dateLisible(at.quand) : "—"])) : t("Team workshop: empty for now (the request comes with it)")}</div>` : ""}</div>`;
    const ordre = d.ordre ? `<div class="muet petit">${h(t(d.ordre.action === "demarrer" ? "Last order: start, {0}" : "Last order: stop after the cycle, {0}", [dateLisible(new Date(d.ordre.quand * 1000).toISOString())]))}</div>` : "";
    const ordres = `<div class="ligne mt-1"><button class="btn btn-primaire" type="button" data-direct="demarrer-equipe" title="${h(t("Starts this computer's pilot, then every computer of the team (those whose user allows it)."))}">▶ ${t("Start the team")}</button><button class="btn" type="button" data-direct="arreter-equipe">${t("Stop the team after the cycle")}</button></div>${ordre}`;
    const pa = d.passation;
    const passation = !pa ? "" : pa.etat === "en_cours" ? `<p class="petit"><span class="muet">⏳ ${h(t("Handing the team over to {0}: it takes it at its next beat…", [pa.nom]))}</span></p>` : `<p class="petit"><span class="attention">✖ ${h(t(pa.erreur || ""))}</span></p>`;
    tete = `<p class="petit">${etatM}</p>${passation}${invitation}${demandes}${atelier}${ordres}<div class="ligne mt-1"><button class="btn" type="button" data-direct="inviter" ${d.etat !== "pret" ? "disabled" : ""}>${t("Invite a computer")}</button><span class="espace"></span><button class="btn btn-petit" type="button" data-direct="quitter">${t("End the team's link")}</button></div>`;
  } else if (d.role === "attente") {
    tete = `<p class="petit"><span class="muet">⏳ ${t("Waiting for the master to accept this computer…")}</span></p><div class="ligne mt-1"><span class="espace"></span><button class="btn btn-petit" type="button" data-direct="quitter">${t("Cancel")}</button></div>`;
  } else {
    const etatN = d.etat === "connecte" ? `<span class="ok">✔ ${h(t("Connected to {0}, the master", [d.maitre || ""]))}</span>` : `<span class="attention">✖ ${h(t("The master ({0}) does not answer: the team is seen through GitHub meanwhile.", [d.maitre || ""]))}</span>${d.erreur ? ` <span class="muet petit">${h(t(d.erreur))}</span>` : ""}`;
    const at = d.atelier;
    const atelierN = d.atelier_lien ? `<div class="muet petit">${at && at.version ? h(t("Team workshop through the master's link: version {0}, {1} files.", [at.version, at.fichiers])) : t("Team workshop through the master's link.")}</div>` : "";
    const ordre = d.ordre ? `<div class="muet petit">${h(t(d.ordre.action === "demarrer" ? "The master started the team, {0}" : "The master stopped the team after the cycle, {0}", [dateLisible(new Date(d.ordre.quand * 1000).toISOString())]))}</div>` : "";
    const relais = d.passation && d.passation.etat === "relais" ? `<p class="petit"><span class="muet">⏳ ${t("This computer takes the team over: the team's workshop comes, then its own link opens…")}</span></p>` : "";
    tete = `<p class="petit">${etatN}</p>${relais}${atelierN}
      <div class="mt-1"><label class="interrupteur m0"><input type="checkbox" data-direct-bascule="ordres" ${d.accepte_ordres ? "checked" : ""}><span>${t("The master may start and stop this computer's pilot")}</span></label>${ordre}</div>
      <div class="ligne mt-1"><span class="espace"></span><button class="btn btn-petit" type="button" data-direct="quitter">${t("Leave the team's link")}</button></div>`;
  }
  return `<div class="bloc" id="bloc-direct"><h2>${t("Team, live")}</h2>${tete}${lignes ? `<table class="tab appareils pcs mt-1">${lignes}</table>` : ""}</div>`;
}
// Overview › API spend › Auto stop: the dollars at which the pilot stops after the cycle (empty: none)
$("#page").addEventListener("change", async (ev) => {
  const i = ev.target; if (i.id !== "depense-arret") return;
  const v = Number(String(i.value).replace(",", "."));
  if (i.value !== "" && !(v >= 0)) { toast(t("An amount in dollars, or empty for no automatic stop."), "erreur"); return; }
  if (await action(api("depense", "POST", { action: "arret", seuil: i.value === "" ? 0 : v }))) rafraichir();
});
// Overview › API spend › Count again from now (credit added to the API account)
$("#page").addEventListener("click", async (ev) => {
  if (!ev.target.closest("#depense-zero")) return;
  if (!await confirmer(t("Count the API spend again from now (0 $)? Do it when you added credit to the API account; the budget then counts from now."), { ok: t("Count again from now") })) return;
  if (await action(api("depense", "POST", { action: "zero" }))) rafraichir();
});
$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-direct]"); if (!b) return;
  const nom = etat.projet || ((etat.vue || {}).projet || {}).nom;
  const a = b.dataset.direct;
  if (a === "copier") { const c = $("#direct-invit"); if (c) navigator.clipboard.writeText(c.textContent).then(() => toast(t("Copied."), "ok")); return; }
  if (a === "quitter" && (etat.directVues[nom] || {}).role !== "attente") {
    const d = etat.directVues[nom] || {};
    const c = await choisir(d.role === "maitre" ? t("End the team's link? The other computers lose it (they go on through GitHub) and will need a new invitation.") : t("Leave the team's link? This computer goes on through GitHub."), [{ id: "", texte: t("Cancel") }, { id: "oui", texte: d.role === "maitre" ? t("End") : t("Leave"), danger: true }]);
    if (c !== "oui") return;
  }
  const corps = { action: a };
  if (a === "rejoindre") corps.invitation = ($("#direct-invitation") || {}).value || "";
  if (a === "transmettre") {
    const c = await choisir(t("Make {0} the team's master? It takes the team (its computers, its workshop, its orders) and opens its own link; this computer becomes one of its nodes. The others follow by themselves.", [b.dataset.nom || ""]), [{ id: "", texte: t("Cancel") }, { id: "oui", texte: t("Make it the master") }]);
    if (c !== "oui") return;
  }
  if (a === "retirer" || a === "accepter" || a === "refuser" || a === "transmettre") corps.id = b.dataset.id;
  const v = await action(api("pcs-direct", "POST", corps), "");
  if (v) { etat.directVues[nom] = v.direct || null; etat.directLus[nom] = Date.now(); rendrePage(); if (a === "rejoindre") toast(t("The code is right: the master now accepts this computer on its screen."), "ok"); if (a === "demarrer-equipe") toast(t("The team starts: every computer whose user allows it starts within a few seconds."), "ok"); }
});
$("#page").addEventListener("change", async (ev) => {
  const i = ev.target.closest("[data-direct-bascule]"); if (!i) return;
  const nom = etat.projet || ((etat.vue || {}).projet || {}).nom;
  const valeur = i.checked;
  if (i.dataset.directBascule === "atelier" && valeur) {
    const c = await choisir(t("The team's workshop through the direct link? The plan, the request, the log and the notes then go through this link only: .athanor/ leaves the commits on every computer (the next push takes it out of GitHub; the files stay in each folder)."), [{ id: "", texte: t("Cancel") }, { id: "oui", texte: t("Through the link") }]);
    if (c !== "oui") { i.checked = false; return; }
  }
  const v = await action(api("pcs-direct", "POST", { action: i.dataset.directBascule, valeur }), "");
  if (v) { etat.directVues[nom] = v.direct || null; etat.directLus[nom] = Date.now(); rendrePage(); } else i.checked = !valeur;
});

function etatPC(p) {
  if (!p.en_ligne) return `<span class="muet">${t("Offline")}${p.maj ? " · " + h(t("last seen {0}", [dateLisible(p.maj)])) : ""}</span>`;
  if (p.pilote && p.jalon) {
    const nom = p.jalon === "cadrage" ? t("the plan (framing)") : p.jalon === "livraison" ? t("the final report") : p.jalon_titre || p.jalon;
    return `<b class="ok">${h(t("Builds {0}", [nom]))}</b>${p.depuis ? ` <span class="muet">· ${h(t("since {0}", [dateLisible(p.depuis)]))}</span>` : ""}`;
  }
  if (p.pilote) return `<span class="ok">${t("Pilot running")}</span>`;
  return `<span class="muet">${t("Online, pilot stopped")}</span>`;
}
// erreurPCs: the reasons athanor github pcs gives (in English), in the interface's language when known
function erreurPCs(e) {
  return String(e).split(" · ").map((x) => {
    let m = x.match(/^The project folder already has files that GitHub's repository has too \((.*)\): Athanor does not overwrite them\./);
    if (m) return t("The project folder already has files that GitHub's repository has too ({0}): Athanor does not overwrite them. Move them out of the folder, then it starts from the other computers' work.", [m[1]]);
    m = x.match(/^This project's history and the one on GitHub \((.*)\) have nothing in common/);
    if (m) return t("This project's history and the one on GitHub ({0}) have nothing in common: the other computers' work is not merged into it.", [m[1]]);
    return x;
  }).join(" · ");
}
// pcsOuvert: Overview › Other computers unfolded (this device's choice; folded by default)
function pcsOuvert() {
  if (etat.pcsOuvert === undefined) { try { etat.pcsOuvert = localStorage.getItem("pcsOuvert") === "1"; } catch (e) { etat.pcsOuvert = false; } }
  return etat.pcsOuvert;
}
document.addEventListener("toggle", (ev) => {
  if (!ev.target || ev.target.id !== "pcs-repli") return;
  etat.pcsOuvert = ev.target.open;
  try { localStorage.setItem("pcsOuvert", etat.pcsOuvert ? "1" : "0"); } catch (e) { /* this device only */ }
}, true);
function rendreBlocPCs(vp) {
  if (!vp || !vp.lie) return "";
  const nomP = etat.projet || ((etat.vue || {}).projet || {}).nom;
  const bascule = etat.pcsBascule[nomP];
  if (bascule) {
    // the switch is being turned on or off: it stays as the user put it until GitHub answers
    return `<div class="bloc" id="bloc-pcs"><h2>${t("Other computers")}</h2>
      <label class="interrupteur"><input type="checkbox" data-pcs-actif ${bascule === "on" ? "checked" : ""} disabled><span>${t("Work with other computers on {0}", [h(vp.depot || "")])}</span></label>
      <p class="muet petit">⏳ ${bascule === "on" ? t("Turning on: Athanor reads GitHub and brings in the other computers' work…") : t("Turning off…")}</p>
    </div>`;
  }
  if (!vp.actif) {
    // step by step: what working at the same time needs, and where this computer stands; folded while it is
    // not used (opened again by a click, remembered on this device; always open once switched on)
    const cx = connexionConnueGH() || {};
    return `<div class="bloc bloc-repliable" id="bloc-pcs"><details id="pcs-repli" ${pcsOuvert() ? "open" : ""}><summary><h2>${t("Other computers")}</h2><span class="muet petit">${t("not used on this project")}</span></summary>
      <p class="intro petit">${t("Build this project with other computers at the same time, from the same GitHub repository: each one takes a milestone no other one is building, the ones it is best at first, and they merge each other's work.")} ${lienGuide("pcs")}</p>
      <ol class="pas-pcs petit">
        <li>${t("Each person can push to {0}: its owner adds them as collaborators on GitHub (the repository's Settings › Collaborators). No organization needed.", [h(vp.depot || "")])}</li>
        <li>${t("Each computer is connected to GitHub with that account (Settings › Options › GitHub).")}
          ${cx.connecte ? `<span class="ok">✔ ${t("This computer: {0}", [h(cx.login || "")])}</span>` : `<button class="btn btn-petit" type="button" data-connexion-gh="connecter">${t("Connect to GitHub")}</button>`}</li>
        <li>${t("Here, switch on Work with other computers: the workshop (.athanor/) is shared, so the others get the request and the plan.")}</li>
        <li>${t("On each other computer: New project › Join a project, with {0}, the same branch and an empty folder.", [h(vp.depot || "")])}</li>
      </ol>
      <label class="interrupteur"><input type="checkbox" data-pcs-actif><span>${t("Work with other computers on {0}", [h(vp.depot || "")])}</span></label>
    </details></div>`;
  }
  const tous = [vp.moi, ...(vp.pcs || [])].filter(Boolean);
  // a milestone another computer could not build (a program that did not work there): who was meant to build it
  const principal = (jalon, p) => {
    const r = tous.flatMap((x) => (x.refus || []).filter((y) => y.jalon === jalon).map((y) => [y.quand, x.nom])).sort();
    return r.length && r[0][1] !== p.nom ? r[0][1] : "";
  };
  const refus = (p) => (p.refus || []).map((r) => `<div class="petit attention">✖ ${h(t("{0} does not work here", [r.outil ? r.outil[0].toUpperCase() + r.outil.slice(1) : ""]))} · ${h(r.jalon)}${r.pourquoi ? ` <span class="muet">— ${h(r.pourquoi)}</span>` : ""}</div>`).join("");
  const lignes = tous.map((p) => `<tr>
      <td class="etroit"><span class="point-appareil ${p.en_ligne ? "en-ligne" : ""}"></span></td>
      <td><b>${h(p.nom)}</b>${p.moi ? ` <span class="badge">${t("this computer")}</span>` : ""}
        ${p.capacites || (p.mots || []).length ? `<div class="muet petit">${h(p.capacites || "")}${(p.mots || []).length ? ` <span class="mots-pc">${p.mots.map((m) => `<code>${h(m)}</code>`).join(" ")}</span>` : ""}</div>` : ""}</td>
      <td>${etatPC(p)}${p.en_ligne && p.pilote && p.jalon && principal(p.jalon, p) ? `<div class="muet petit">${h(t("in place of {0}", [principal(p.jalon, p)]))}</div>` : ""}${p.pousse ? `<div class="muet petit">${h(t("last push: {0}", [p.pousse]))}</div>` : ""}${refus(p)}</td></tr>`).join("");
  const enLigne = (vp.pcs || []).filter((p) => p.en_ligne).length;
  return `<div class="bloc" id="bloc-pcs"><div class="bloc-tete"><h2>${t("Computers on this project")}</h2>${(vp.pcs || []).length ? `<span class="muet petit">${h(t("{0} other(s) online", [enLigne]))}</span>` : ""}</div>
    <table class="tab appareils pcs">${lignes}</table>
    ${vp.erreur ? `<p class="petit attention">✖ ${h(erreurPCs(vp.erreur))}</p>` : ""}
    ${(vp.notes || []).length ? `<h3 class="mt-2">${t("Notes between the computers")}</h3><ul class="notes-pcs muet petit">${vp.notes.map((n) => `<li>${md(n.replace(/^-\s*/, ""))}</li>`).join("")}</ul>` : ""}
    ${!(vp.pcs || []).length ? `<p class="muet petit">${t("No other computer yet. On the other computer: Join a project (in the project picker), with this same repository and branch.")}</p>` : ""}
    <div class="ligne mt-2"><a class="btn btn-petit" href="#distance">${t("What this computer is good at")}</a>${lienGuide("pcs")}<span class="espace"></span>
      <label class="interrupteur m0"><input type="checkbox" data-pcs-actif checked><span>${t("Work with other computers")}</span></label></div>
  </div>`;
}
$("#page").addEventListener("change", async (ev) => {
  const i = ev.target; if (!i.matches("[data-pcs-actif]")) return;
  const nom = etat.projet || ((etat.vue || {}).projet || {}).nom;
  const actif = i.checked, vp = etat.pcsVues[nom] || {};
  if (!actif) {
    const c = await choisir(t("Stop working with the other computers on this project? This computer stops taking part: its pilot then builds the milestones alone again."), [{ id: "non", texte: t("Cancel") }, { id: "oui", texte: t("Stop"), danger: true }]);
    if (c !== "oui") { i.checked = true; return; }
  } else if (!vp.atelier) {
    // the other computers build from the plan, the requests and the log: the workshop goes to GitHub too
    const c = await choisir(t("Working with other computers needs Share the workshop (.athanor/) on GitHub, which is off for this project: the request, the plan, the log and the documents then go into the commits, so that the other computers build from them (never secrets.env, the raw logs nor the pilot's state). Turn on both?"),
      [{ id: "oui", texte: t("Share the workshop and turn on"), primaire: true }, { id: "", texte: t("Cancel") }], { titre: t("Work with other computers") });
    if (c !== "oui") { i.checked = false; return; }
  }
  etat.pcsBascule[nom] = actif ? "on" : "off"; etat.pcsGen[nom] = (etat.pcsGen[nom] || 0) + 1;
  rendrePage();
  const v = await action(api("pcs", "POST", { actif }), "");
  delete etat.pcsBascule[nom];
  if (v) {
    etat.pcsVues[nom] = v; etat.pcsLus[nom] = Date.now();
    if (v.rejoint) toast(t("This computer starts from the other computers' work: their plan, their requests and their commits."), "ok");
    else if (actif && v.actif) toast(t("This computer now works with the other computers on {0}.", [v.depot || ""]), "ok");
    else if (!actif) toast(t("This computer works alone on this project again."), "ok");
  }
  await rafraichir(); // joined: the team's plan and requests are the project's now
  rendrePage();
});

// --- on a paired device, its Remote page: its own notifications (distance_push.go, static/sw.js)
const pushPossible = () => "serviceWorker" in navigator && "PushManager" in window && "Notification" in window && window.isSecureContext;
function cleOctets(b) { const s = atob(b.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((b.length + 3) % 4)); return Uint8Array.from(s, (c) => c.charCodeAt(0)); }
function memeClePush(sub, cle) {
  const k = sub.options && sub.options.applicationServerKey; if (!k) return true;
  const a = new Uint8Array(k), b = cleOctets(cle);
  return a.length === b.length && a.every((x, i) => x === b[i]);
}
async function abonnementLocal() { if (!pushPossible()) return null; const r = await navigator.serviceWorker.getRegistration("/"); return r ? r.pushManager.getSubscription() : null; }
async function rendreCetAppareil() {
  const iOS = /iPhone|iPad|iPod/.test(navigator.userAgent), installe = matchMedia("(display-mode: standalone)").matches || navigator.standalone;
  let corps;
  if (!pushPossible()) {
    corps = `<p class="attention petit">${iOS && !installe ? t("On an iPhone or iPad, notifications need Athanor on the Home Screen: Share › Add to Home Screen, then open Athanor from its icon and come back here.") : t("This browser cannot receive notifications from Athanor.")}</p>`;
  } else {
    const [v, sub] = await Promise.all([api("push").catch(() => null), abonnementLocal().catch(() => null)]);
    const actif = !!(v && v.abonne && sub && memeClePush(sub, v.cle));
    const refuse = Notification.permission === "denied";
    corps = `<label class="interrupteur"><input type="checkbox" id="push-actif" ${actif ? "checked" : ""} ${refuse ? "disabled" : ""}><span>${t("Notifications on this device")}</span></label>
      ${refuse ? `<p class="attention petit">${t("Notifications are blocked for this site in the browser's settings: allow them there, then come back.")}</p>` : ""}
      ${actif ? `<button class="btn btn-petit" id="push-essai">${t("Send a test notification")}</button>` : ""}`;
  }
  return `<div class="bloc"><h2>${t("This device")}</h2>
    <p class="intro petit">${t("Get a notification here, even with Athanor closed, when Claude needs you, when a milestone is done, when a project is delivered and when a computer of a team is stuck. Encrypted for this device alone: the push service of its browser only relays it.")}</p>
    ${corps}</div>`;
}
$("#page").addEventListener("change", async (ev) => {
  const i = ev.target; if (i.id !== "push-actif") return;
  i.disabled = true;
  try {
    if (i.checked) {
      const v = await api("push");
      const reg = await navigator.serviceWorker.register("/sw.js", { scope: "/" });
      await navigator.serviceWorker.ready;
      if (await Notification.requestPermission() !== "granted") throw new Error(t("Notifications were not allowed."));
      let sub = await reg.pushManager.getSubscription();
      if (sub && !memeClePush(sub, v.cle)) { await sub.unsubscribe().catch(() => {}); sub = null; } // made with an older key of Athanor's
      if (!sub) sub = await reg.pushManager.subscribe({ userVisibleOnly: true, applicationServerKey: cleOctets(v.cle) });
      const j = sub.toJSON();
      await api("push", "POST", { action: "abonner", endpoint: j.endpoint, p256dh: j.keys.p256dh, auth: j.keys.auth });
      toast(t("Notifications are on: a test one is on its way."), "ok");
    } else {
      const sub = await abonnementLocal(); if (sub) await sub.unsubscribe().catch(() => {});
      await api("push", "POST", { action: "desabonner" });
    }
  } catch (e) { toast(e.message || String(e), "erreur"); }
  rendrePage();
});
$("#page").addEventListener("click", async (ev) => {
  if (!ev.target.closest("#push-essai")) return;
  const r = await action(api("push", "POST", { action: "essai" }));
  if (r) toast(t("Sent: it shows in a few seconds."), "ok");
});

// --- remote access (distance.go): the switch, the pairing link and its QR code, the trusted devices
pages.distance = async () => {
  // on a paired device: its notifications, the state, the trusted devices and this computer; never the
  // switch, the address nor the pairing (the server gives a device none of it)
  const distant = !!(etat.vue || {}).distant;
  const d = await api("distance");
  etat.distVue = d; etat.distSig = signatureDistance(d);
  if (!etat.pcMachine) etat.pcMachine = await api("pcs/machine").catch(() => null);
  const m = etat.pcMachine;
  const statut = {
    arret: ["muet", t("Off: no device can reach Athanor.")],
    installation: ["muet", t("Downloading Cloudflare's tunnel program (once)…")],
    demarrage: ["muet", t("Opening the tunnel…")],
    pret: ["ok", "✔ " + t("Reachable at {0}", [d.adresse || ""])],
    erreur: ["attention", "✖ " + (d.message || t("The tunnel did not start."))],
  }[d.etat] || ["muet", ""];
  const appareils = d.appareils.length ? `<table class="tab appareils"><tbody>${d.appareils.map((a) => `<tr>
      <td class="etroit"><span class="point-appareil ${a.en_ligne ? "en-ligne" : ""}" title="${h(a.en_ligne ? t("Connected now") : t("Not connected"))}"></span></td>
      <td><input class="nom-appareil" data-renommer="${h(a.id)}" value="${h(a.nom)}" maxlength="40" aria-label="${h(t("Name"))}">
        <div class="muet petit">${a.moi ? `<span class="badge">${t("this device")}</span> ` : ""}${a.en_ligne ? `<b class="ok">${t("Connected now")}</b>` : a.vu_le ? h(t("Last seen {0}", [dateLisible(a.vu_le)])) : ""}${a.lieu ? " · " + h(a.lieu) : ""} · ${h(t("paired {0}", [dateLisible(a.ajoute)]))}${a.notifs ? ` · 🔔 ${t("notifications on")}` : ""}</div></td>
      <td class="droite"><button class="btn btn-petit btn-danger" data-retirer-appareil="${h(a.id)}" data-nom="${h(a.nom)}" ${a.moi ? "data-moi" : ""} title="${h(t("Remove from the trusted devices"))}" aria-label="${h(t("Remove from the trusted devices"))}">✕</button></td></tr>`).join("")}</tbody></table>`
    : `<div class="vide">${t("No device yet.")}</div>`;
  const gauche = distant ? `${await rendreCetAppareil()}
      <div class="bloc"><h2>${t("Remote access")}</h2>
        <p class="petit ${statut[0]}">${h(statut[1])}</p>
        <p class="muet petit">${t("This device is paired: it follows and drives Athanor from anywhere. The switch, the address and the pairing of new devices are on the computer only.")} ${lienGuide("distance")}</p>
      </div>` : `
      <div class="bloc"><h2>${t("Remote access")}</h2>
        <p class="intro petit">${t("Follow and drive Athanor from your phone or another computer, from anywhere: through a Cloudflare tunnel (https, nothing to set up). Only the devices you pair here get in.")} ${lienGuide("distance")}</p>
        <label class="interrupteur"><input type="checkbox" id="dist-actif" ${d.actif ? "checked" : ""}><span>${t("Remote access")}</span></label>
        <p class="petit ${statut[0]}" id="dist-statut">${h(statut[1])}</p>
        <p class="muet petit">${d.domaine ? t("While remote access is on, the computer does not go to sleep (the screen can still turn off); Athanor must stay open.") : t("The address changes when the computer restarts: scan the code again then. While remote access is on, the computer does not go to sleep (the screen can still turn off); Athanor must stay open.")}</p>
      </div>
      ${blocDomaine(d)}
      ${d.etat === "pret" && d.lien ? `<div class="bloc"><h2>${t("Pair a device")}</h2>
        <div class="appairage"><img class="qr-distance" src="${h(d.qr)}" alt="${h(t("QR code of the pairing link"))}">
          <div><p class="m0">${t("Scan it with the phone's camera, or open this link on the device:")}</p>
            <div class="ligne mt-1"><input type="text" class="code" id="dist-lien" readonly value="${h(d.lien)}"><button class="btn btn-petit" id="dist-copier">${t("Copy")}</button></div>
            <p class="muet petit mt-1">${t("Single-use link, renewed by itself")} (<span id="dist-expire">${h(dureeCourte(d.expire))}</span>). ${t("Never send it to anyone: whoever opens it gets in.")}</p></div></div>
        <h3>${t("Then, the code to type on the device")}</h3>
        <div class="code-pc" id="dist-chiffres" tabindex="0" role="button" aria-label="${h(t("Show the code"))}">
          <span class="masque" aria-hidden="true">••• •••</span><span class="vrai">${h(chiffresLisibles(d.chiffres))}</span>
          <span class="indice">${t("Move the mouse over it")}</span></div>
        <div class="barre-chiffres"><i id="dist-chiffres-barre" style="width:${Math.round(100 * (d.chiffres_expire || 0) / 20000)}%"></i></div>
        <p class="muet petit mt-1">${t("Shown only here, renewed every 20 seconds: a photo of the QR code is not enough, the device must see this screen. Three wrong codes and it must scan again.")}</p>
      </div>` : ""}`;
  return `<div class="grille grille-2">
    <div class="colonne">${gauche}
    </div>
    <div class="colonne">
      <div class="bloc"><h2>${t("Trusted devices")}</h2>${appareils}
        <p class="muet petit mt-2">${t("✕ removes a device: its next click is refused, and it must scan the code again to come back.")}</p>
      </div>
      ${m ? `<div class="bloc"><h2>${t("This computer, for the other computers")}</h2>
        <p class="intro petit">${t("When several computers build a project (Overview › Other computers), each one says what it is good at: the pilot gives it the milestones that need it, and leaves the others to the computers that have what they need.")} ${lienGuide("pcs")}</p>
        <label class="champ"><span>${t("Name")}</span><input type="text" id="pc-nom" maxlength="60" value="${h(m.nom || "")}"></label>
        <label class="champ"><span>${t("What it is good at")}</span><input type="text" id="pc-capacites" maxlength="300" value="${h(m.capacites || "")}" placeholder="${h(t("e.g. RTX 4090, 24 GB: 3D models and renders"))}"></label>
        <label class="champ"><span>${t("Words for the plan")}</span><input type="text" id="pc-mots" value="${h((m.mots || []).join(", "))}" placeholder="gpu:3, network, blender"></label>
        <p class="muet petit">${t("A milestone that needs one of these words gets “Needs: gpu.” in the plan: a computer that has the word builds it. Two computers with the same word build such milestones at the same time. A level from 1 to 9 says how capable it is (gpu:3): the heaviest milestones go to the most capable computer. Optional: with no words anywhere, Athanor shares the milestones out by itself.")}</p>
        <button class="btn" id="pc-enregistrer">${t("Save")}</button>
      </div>` : ""}
    </div>
  </div>`;
};
// your own domain (distance_domaine.go): a fixed address, and Cloudflare Access in front of it
function blocDomaine(d) {
  const e = d.domaine_etape || "";
  const etape = e === "connexion" ? `<p class="petit">⏳ ${t("Sign in to Cloudflare in the browser window that opened, then choose your domain.")}${d.domaine_lien ? ` <a href="${h(d.domaine_lien)}" target="_blank" rel="noopener noreferrer">${t("Open the sign-in page")}</a>` : ""}</p><button class="btn btn-petit" data-domaine="annuler">${t("Cancel")}</button>`
    : e === "creation" ? `<p class="petit">⏳ ${t("Creating the tunnel and the address…")}</p>`
    : e === "erreur" ? `<p class="petit attention">✖ ${h(d.domaine_message || "")}</p>` : "";
  if (d.domaine) {
    return `<div class="bloc"><h2>${t("Address")}</h2>
      <p class="ok m0">✔ ${t("Your domain: {0}", [h("https://" + d.domaine)])}</p>
      <p class="muet petit">${t("A fixed address: the devices keep their access when the computer restarts.")}</p>
      ${etape}
      <details class="mt-2"><summary>${t("Let Cloudflare check who comes in (Cloudflare Access)")}</summary>
        <p class="petit">${t("With it, Cloudflare asks for your e-mail address and sends a code before anything reaches the computer: a stranger who finds the address does not even see Athanor. Free up to 50 people. Once, in Cloudflare's dashboard:")}</p>
        <ol class="petit guide-access">
          <li>${t("Open {0} (Zero Trust) and choose a team name if asked, with the free plan.", ['<a href="https://one.dash.cloudflare.com/" target="_blank" rel="noopener noreferrer">one.dash.cloudflare.com</a>'])}</li>
          <li>${t("Access › Applications › Add an application › Self-hosted.")}</li>
          <li>${t("Application domain: {0}.", [`<code>${h(d.domaine)}</code>`])}</li>
          <li>${t("Add a policy: action Allow, Include › Emails › your e-mail address (and those of the people you trust).")}</li>
          <li>${t("Save. On the phone, open the address: Cloudflare asks for your e-mail and its code, then pair the device as usual.")}</li>
        </ol>
      </details>
      <div class="ligne mt-2"><button class="btn btn-petit" data-domaine="oublier">${t("Back to a temporary address")}</button></div>
    </div>`;
  }
  return `<div class="bloc"><h2>${t("Address")}</h2>
    <p class="muet petit m0">${t("Now a temporary address (trycloudflare.com), which changes when the computer restarts. With a domain of yours at Cloudflare, the address never changes, and Cloudflare can check who comes in before anything reaches the computer.")}</p>
    <details class="mt-2" ${e ? "open" : ""}><summary>${t("Use my domain")}</summary>
      <p class="muet petit">${t("Needs a free Cloudflare account and a domain whose DNS is at Cloudflare (a domain costs about 10 € a year). Athanor opens Cloudflare's sign-in once, creates a tunnel of this computer and the address; an existing record of that name is never replaced.")}</p>
      <label class="champ"><span>${t("Address on your domain")}</span><input type="text" id="dom-hote" placeholder="athanor.example.com" autocomplete="off" spellcheck="false" ${e === "connexion" || e === "creation" ? "disabled" : ""}></label>
      <div class="ligne"><button class="btn" id="dom-connecter" ${e === "connexion" || e === "creation" ? "disabled" : ""}>${t("Connect to Cloudflare")}</button></div>
      ${etape}
    </details>
  </div>`;
}
// (a device's "last seen" moves at each of its requests: the list is rebuilt only when one comes or goes)
function signatureDistance(d) { return JSON.stringify({ ...d, expire: 0, chiffres: "", chiffres_expire: 0, appareils: (d.appareils || []).map((a) => ({ ...a, vu_le: "" })) }); }
function chiffresLisibles(c) { return c ? c.slice(0, 3) + " " + c.slice(3) : ""; }
function dureeCourte(s) { s = Math.max(0, Math.round(s || 0)); return s >= 60 ? `${Math.floor(s / 60)} min ${String(s % 60).padStart(2, "0")}` : `${s} s`; }
function dateLisible(iso) {
  const d = new Date(iso); if (isNaN(d)) return "";
  const auj = new Date().toDateString() === d.toDateString();
  return auj ? d.toLocaleTimeString(etat.langue, { hour: "2-digit", minute: "2-digit" }) : d.toLocaleDateString(etat.langue, { day: "numeric", month: "short" }) + " " + d.toLocaleTimeString(etat.langue, { hour: "2-digit", minute: "2-digit" });
}
// live: the page is rebuilt only when something changed (a device, the tunnel, a new code); the countdown in place
async function majDistance() {
  if (document.activeElement && document.activeElement.matches(".nom-appareil, #dom-hote, #pc-nom, #pc-capacites, #pc-mots")) return;
  const d = await api("distance").catch(() => null); if (!d) return;
  if (signatureDistance(d) !== etat.distSig) return rendrePage();
  const e = document.getElementById("dist-expire"); if (e) e.textContent = dureeCourte(d.expire);
  const c = document.querySelector("#dist-chiffres .vrai"); if (c) c.textContent = chiffresLisibles(d.chiffres);
  const b = document.getElementById("dist-chiffres-barre"); if (b) b.style.width = Math.round(100 * (d.chiffres_expire || 0) / 20000) + "%";
}
$("#page").addEventListener("click", async (ev) => {
  const cp = ev.target.closest("#dist-chiffres");
  if (cp) { cp.classList.add("montre"); clearTimeout(etat.chiffresMontre); etat.chiffresMontre = setTimeout(() => { const x = document.getElementById("dist-chiffres"); if (x) x.classList.remove("montre"); }, 10000); return; }
  if (ev.target.closest("#pc-enregistrer")) {
    const m = await action(api("pcs/machine", "POST", { nom: $("#pc-nom").value, capacites: $("#pc-capacites").value, mots: $("#pc-mots").value }));
    if (m) { etat.pcMachine = m; etat.pcsLus = {}; toast(t("Saved."), "ok"); rendrePage(); }
    return;
  }
  const dm = ev.target.closest("#dom-connecter, [data-domaine]");
  if (dm) {
    if (dm.id === "dom-connecter") { const v = ($("#dom-hote") || {}).value || ""; if (!v.trim()) return $("#dom-hote").focus(); dm.disabled = true; await action(api("distance/domaine", "POST", { action: "connecter", hote: v.trim() }), ""); return rendrePage(); }
    if (dm.dataset.domaine === "oublier") {
      const c = await choisir(t("Back to a temporary address? The tunnel of your domain is deleted at Cloudflare, and the devices must scan the code again on the new address."), [{ id: "non", texte: t("Cancel") }, { id: "oui", texte: t("Back to a temporary address"), danger: true }]);
      if (c !== "oui") return;
    }
    await action(api("distance/domaine", "POST", { action: dm.dataset.domaine }), ""); return rendrePage();
  }
  const b = ev.target.closest("#dist-copier, [data-retirer-appareil]"); if (!b) return;
  if (b.id === "dist-copier") { const l = $("#dist-lien"); if (l) navigator.clipboard.writeText(l.value).then(() => toast(t("Copied."), "ok")); return; }
  const c = await choisir(b.hasAttribute("data-moi") ? t("Remove this device from the trusted devices? It loses access at once: to come back, it must scan the code on the computer.")
    : t("Remove “{0}” from the trusted devices? It must scan the code again to come back.", [b.dataset.nom || ""]), [{ id: "non", texte: t("Cancel") }, { id: "oui", texte: t("Remove"), danger: true }]);
  if (c !== "oui") return;
  await action(api("distance/appareils/" + encodeURIComponent(b.dataset.retirerAppareil), "DELETE"));
  rendrePage();
});
$("#page").addEventListener("change", async (ev) => {
  const i = ev.target;
  if (i.id === "dist-actif") { i.disabled = true; await action(api("distance", "POST", { actif: i.checked })); await rafraichir(); return rendrePage(); }
  if (i.matches(".nom-appareil") && i.value.trim()) { await action(api("distance/appareils/" + encodeURIComponent(i.dataset.renommer), "POST", { nom: i.value.trim() })); i.blur(); rendrePage(); }
});
$("#page").addEventListener("keydown", (ev) => { if (ev.key === "Enter" && ev.target.matches(".nom-appareil")) ev.target.blur(); });
// on a device the computer removed: the page says so, nothing else works
function afficherNonAppaire() {
  if (etat.nonAppaire) return; etat.nonAppaire = true;
  document.body.innerHTML = `<div class="non-appaire"><div style="font-size:40px">🔒</div><h1>Athanor</h1><p>${h(t("This device is no longer paired with Athanor. On your computer, open Athanor › Remote and scan the QR code again."))}</p></div>`;
}

async function rendreOptions() {
  setTimeout(majConnexionGH, 0); // the GitHub block fills in once the page is there (WSL may be slow)
  const [o, cl, inst] = await Promise.all([api("options"), api("clients").catch(() => null), api("instances").catch(() => null)]);
  etat.optionsVue = o; etat.clientsVue = cl;
  const b = o.bureau || {};
  return `<div class="grille grille-2">
    <div class="colonne">
      <div class="bloc"><h2>${t("Appearance")}</h2>
        <label class="champ"><span>${t("Language")}</span></label>${rendreLangues()}
        <p class="muet petit mt-1">${t("Also sets the language Claude uses for its plan, log and documents in new projects.")}</p>
        <label class="champ mt-3"><span>${t("Palette and accent")}</span></label>${rendreApparence()}
      </div>
      <div class="bloc" id="bloc-connexion-gh"><h2>GitHub</h2>${rendreConnexionGH(etat.ghConnexion)}</div>
      <div class="bloc" id="bloc-raccourci">${blocRaccourci(raccourciConnu(), true) || `<h2>${t("Shortcut")}</h2>${attenteRaccourci()}`}</div>
      <div class="bloc"><h2>${t("Startup")}</h2>
        <label class="interrupteur"><input type="checkbox" id="op-lancer" data-option ${o.lancer_pilote_au_demarrage ? "checked" : ""}><span>${t("Restart the pilots that were running, when the app opens")}</span></label>
        <label class="interrupteur"><input type="checkbox" id="op-windows" data-option ${o.demarrage_windows ? "checked" : ""}><span>${t(o.windows ? "Open Athanor when Windows starts" : (etat.vue || {}).systeme === "darwin" ? "Open Athanor when you log in to the Mac" : "Open Athanor when your desktop session starts")}</span></label>
      </div>
      <div class="bloc local-seul" id="bloc-verrou"><h2>${t("Lock")}</h2>
        <p class="intro petit">${t("With a password, the padlock locks the app: Claude keeps working, but nothing can be seen, changed or read (keys included) without the password. The app starts locked.")}</p>
        ${o.mot_de_passe ? `<label class="champ"><span>${t("Current password")}</span><input type="password" id="mdp-ancien" autocomplete="current-password"></label>` : ""}
        <label class="champ"><span>${o.mot_de_passe ? t("New password (empty to remove it)") : t("Password")}</span><input type="password" id="mdp-nouveau" autocomplete="new-password"></label>
        <button class="btn" id="mdp-enregistrer">${o.mot_de_passe ? t("Change") : t("Enable the lock")}</button>
      </div>
    </div>
    <div class="colonne">
      <div class="bloc"><h2>${t("Projects")}</h2>
        ${(o.projets || []).length ? `<table class="tab"><tbody>${o.projets.map((p) => `<tr><td><b>${h(p.nom)}</b>${p.archive ? ` <span class="badge">${t("archived")}</span>` : ""}</td><td class="muet chemin-projet">${h(p.dossier)}</td><td class="droite"><button class="btn btn-petit" data-gerer="${h(p.nom)}" data-archive="${p.archive ? 1 : ""}">${t("Manage")}</button></td></tr>`).join("")}</tbody></table>` : `<div class="vide">${t("No project.")}</div>`}
        <p class="muet petit mt-2">${t("Rename a project, move its folder, archive or delete it: “Manage”.")}</p>
      </div>
      ${rendreClientsMCP(cl)}
      ${rendreInstancesPC(inst)}
      <div class="bloc"${b.message === "Virtual desktop: Windows only." ? " hidden" : ""}><h2>${t("“Claude” desktop")}</h2>
        <label class="interrupteur"><input type="checkbox" id="op-bureau" data-option ${o.bureau_virtuel ? "checked" : ""}><span>${t("Open the programs Claude starts (Unreal, Blender, a game under test) on a desktop of their own")}</span></label>
        <p class="petit ${b.disponible ? "ok" : "attention"}">${h(t(b.message || ""))}</p>
        ${b.ecran ? `<p class="muet petit">${t("The Linux windows Claude opens (a game under test, Godot, a browser) go to a private screen: never on yours, never taking the focus.")}</p>
        <div class="ligne"><button class="btn btn-petit" data-ecran>${t("View Claude's screen")}</button></div>`
        : b.isole ? `<p class="muet petit">${t("They never show on your screen and never take the focus: a full-screen game is not interrupted. On Claude's desktop, the bar at the top (or Ctrl + Alt + B) brings you back.")}</p>
        <label class="interrupteur mt-1"><input type="checkbox" id="op-son" data-option ${o.entendre_bureau ? "checked" : ""}><span>${t("Hear Claude's desktop")}</span></label>
        <p class="muet petit">${t("Off: while a pilot runs, the programs on Claude's desktop make no sound (you hear them while you look at it).")}</p>
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
        <p class="petit ${o.git_bash ? "ok" : "attention"}">${o.git_bash ? h(t("Git for Windows: {0}", [o.git_bash.replace(/[\\/](usr[\\/])?bin[\\/]bash\.exe$/i, "")])) : t("Git for Windows is missing: Setup & health → Install everything.")}</p>
        <p class="muet petit">${t("Applies to the pilots started afterwards. When you close Athanor while native pilots run, it asks whether they go on in the background.")}</p>
        <label class="interrupteur mt-1"><input type="checkbox" id="op-terminal" data-option ${o.terminal_pilote ? "checked" : ""}><span>${t("Show the pilot's terminal")}</span></label>
        <p class="muet petit">${t("Off: the programs a native pilot starts (bash, git, go, the checks) run in a hidden console, and no window opens. On: one minimised window in the taskbar shows the pilot live, without ever taking the focus. For the pilots started afterwards.")}</p>
      </div>
      <div class="bloc local-seul"><h2>${t("Athanor's folder")}</h2>
        <p class="m0"><code class="chemin-kit">${h(o.kit)}</code></p><div class="muet petit mt-1">${o.kit_valide ? "✔ " + t("Athanor's own files: settings, token, logs, the tools Claude installs. Your projects are elsewhere.") : "✖ " + t("this folder is not the tool")}</div>
        <div class="ligne mt-1"><button class="btn btn-petit" data-ouvrir="kit">${t("Open the folder")}</button><button class="btn btn-petit" data-ouvrir="logs">${t("Logs folder")}</button><span class="espace"></span><button class="btn btn-petit" id="op-guide">${t("Show the first-launch guide and the tutorials again")}</button></div>
        <details class="mt-1" ${o.kit_valide ? "" : "open"}><summary class="petit muet">${t("Use another tool folder (advanced)")}</summary>
          <p class="muet petit">${t("Only for a tool folder you moved or copied yourself: Athanor then uses that one, with its settings and token.")}</p>
          <input type="text" id="op-kit" data-option value="${h(o.kit)}" aria-label="${h(t("Use another tool folder (advanced)"))}"></details>
      </div>
      <div class="bloc" id="bloc-version"><h2>${t("Version")}</h2>${rendreBlocMaj(o)}</div>
    </div>
  </div>`;
};
// Settings › MCP clients (hub_clients.go, hub_mcp_client.go): Claude Desktop or Claude Code pair with the
// 6 digits shown here, the owner accepts with a scope (projects, tools), revokes; ready-to-copy snippets
const OUTILS_LECTURE = ["etat", "projets", "plan", "demandes", "missions", "approbations"];
function rendreClientsMCP(c) {
  if (!c) return "";
  const projets = ((etat.optionsVue || {}).projets || []).filter((p) => !p.archive).map((p) => p.nom);
  const exe = c.commande || "Athanor", q = (x) => /[\s"]/.test(x) ? JSON.stringify(x) : x;
  const code = c.code ? `<div class="bloc-code mt-1"><code class="code-client" id="clients-code">${h(c.code)}</code> <button class="btn btn-petit" type="button" data-clients="fermer">${t("Close")}</button>
      <div class="muet petit">${t("In a terminal on this PC: {0}, then type this code. It works once, for 10 minutes.", [`<code>${h(q(exe))} mcp-client appairer claude-desktop --sorte desktop</code>`])}</div></div>`
    : `<div class="ligne mt-1"><button class="btn" type="button" data-clients="code" ${c.urgence ? "disabled" : ""}>${t("Connect a client")}</button></div>`;
  const demandes = (c.demandes || []).map((d) => `<div class="demande-client mt-1" data-ref="${h(d.ref)}">
      <p class="m0">⚠ <b>${h(t("{0} wants to use Athanor's MCP", [d.nom]))}</b> <span class="badge">${h(d.sorte)}</span> <span class="muet petit">${h(d.ip || "")}</span></p>
      <div class="muet petit mt-1">${t("Projects it may see")}</div>
      <div class="ligne cases-client"><label class="interrupteur m0"><input type="checkbox" data-cl-projet="*"><span>${t("All projects")}</span></label>${projets.map((n) => `<label class="interrupteur m0"><input type="checkbox" data-cl-projet="${h(n)}"><span>${h(n)}</span></label>`).join("")}</div>
      <div class="muet petit mt-1">${t("Tools it may use")}</div>
      <div class="ligne cases-client">${(c.outils || []).concat(d.sorte === "athanor" ? c.outils_athanor || [] : []).map((o) => `<label class="interrupteur m0"><input type="checkbox" data-cl-outil="${h(o)}" ${OUTILS_LECTURE.includes(o) ? "checked" : ""}><span><code>${h(o)}</code></span></label>`).join("")}</div>
      <div class="ligne mt-1"><button class="btn btn-primaire" type="button" data-clients="accepter">${t("Accept")}</button> <button class="btn btn-petit" type="button" data-clients="refuser">${t("Refuse")}</button></div></div>`).join("");
  const liste = (c.clients || []).length ? `<table class="tab mt-1"><tbody>${c.clients.map((x) => `<tr><td><b>${h(x.nom)}</b> <span class="badge">${h(x.sorte)}</span><div class="muet petit">${x.projets.includes("*") ? t("All projects") : h(x.projets.join(", "))} · ${h(x.outils.join(", "))}</div></td>
      <td class="muet petit">${x.vu ? h(t("Used {0}", [dateLisible(x.vu)])) : t("Never used")}</td><td class="droite"><button class="btn btn-petit" type="button" data-clients="revoquer" data-id="${h(x.id)}">${t("Revoke")}</button></td></tr>`).join("")}</tbody></table>`
    : `<div class="vide mt-1">${t("No client connected.")}</div>`;
  const bureau = JSON.stringify({ mcpServers: { athanor: { command: exe, args: ["mcp-client", "claude-desktop"] } } }, null, 2);
  const extrait = (id, titre, texte) => `<div class="muet petit mt-1">${titre}</div><div class="ligne m0"><pre class="code croit m0" id="${id}">${h(texte)}</pre><button class="btn btn-petit" type="button" data-clients="copier" data-cible="${id}">${t("Copy")}</button></div>`;
  return `<div class="bloc local-seul" id="bloc-clients-mcp"><h2>${t("MCP clients")}</h2>
    <p class="intro petit">${t("Claude Desktop or Claude Code on this PC can follow your projects through Athanor's MCP: only the projects and tools you allow. No client can decide an approval, launch a mission or read the vault.")}</p>
    ${c.urgence ? `<p class="attention petit">${t("Emergency stop: the MCP clients are suspended and pairing is closed until Resume.")}</p>` : ""}
    ${code}${demandes}${liste}
    <details class="mt-1"><summary class="petit">${t("Ready-to-copy configuration")}</summary>
      ${extrait("cl-appairer", t("1. Pair (in a terminal, with the code shown above)"), `${q(exe)} mcp-client appairer claude-desktop --sorte desktop`)}
      ${extrait("cl-bureau", t("2. Claude Desktop: add to claude_desktop_config.json"), bureau)}
      ${extrait("cl-code", t("Claude Code (paired with --sorte code under the name claude-code)"), `claude mcp add athanor -- ${q(exe)} mcp-client claude-code`)}
    </details></div>`;
}
async function majClientsMCP(v) {
  v = v || await api("clients").catch(() => null);
  const b = $("#bloc-clients-mcp");
  if (!v || !b || JSON.stringify(v) === JSON.stringify(etat.clientsVue)) return;
  etat.clientsVue = v;
  b.outerHTML = rendreClientsMCP(v);
}
// a code open or a request waiting: the block follows the terminal's pairing
setInterval(() => {
  const v = etat.clientsVue;
  if (etat.page === "reglages" && $("#bloc-clients-mcp") && v && (v.code || (v.demandes || []).length)) majClientsMCP();
}, 3000);
$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-clients]"); if (!b) return;
  const a = b.dataset.clients;
  if (a === "copier") { const c = document.getElementById(b.dataset.cible); if (c) navigator.clipboard.writeText(c.textContent).then(() => toast(t("Copied."), "ok")); return; }
  const corps = { action: a };
  if (a === "revoquer") {
    if (!await confirmer(t("Revoke this client? Its token stops working at once; it must pair again."), { ok: t("Revoke") })) return;
    corps.id = b.dataset.id;
  }
  if (a === "accepter" || a === "refuser") {
    const d = b.closest(".demande-client");
    corps.ref = d.dataset.ref;
    corps.projets = [...d.querySelectorAll("[data-cl-projet]:checked")].map((i) => i.dataset.clProjet);
    corps.outils = [...d.querySelectorAll("[data-cl-outil]:checked")].map((i) => i.dataset.clOutil);
  }
  const v = await action(api("clients", "POST", corps));
  if (v) majClientsMCP(v);
});
// Settings › Remote Athanors › On this PC (hub_annonce.go): the other Athanors of this computer, read from
// their announcement; Pair asks the 6 digits the other one shows, never a link without them
function rendreInstancesPC(v) {
  if (!v) return "";
  const liste = (v.instances || []).map((i) => `<div class="ligne instance-pc" data-id="${h(i.id)}"><span><b>${h(i.nom)}</b> <span class="muet petit">Athanor ${h(i.version)} · ${h((i.modules || []).join(", "))}</span><br><span class="muet petit chemin-court">${h(i.dossier)}</span></span><span class="espace"></span>
      ${i.attente ? `<span class="petit attention">${t("Waiting for the other one's yes")}</span>` : `<input type="text" class="code-instance" inputmode="numeric" maxlength="6" placeholder="${t("6-digit code")}" aria-label="${t("6-digit code")}"> <button class="btn btn-petit" data-instance="appairer">${t("Pair")}</button>`}</div>`).join("");
  const ds = v.distants || {}, ETATS_DISTANT = { en_ligne: ["ok", "Online"], hors_ligne: ["", "Offline"], coffre: ["attention", "Vault locked"], revoque: ["attention", "Pairing revoked"] };
  const lies = (ds.liste || []).map((d) => { const [cl, lib] = ETATS_DISTANT[d.etat] || ["", "Connecting…"]; return `<div class="ligne distant" data-id="${h(d.id)}"><span><b>${h(d.nom)}</b> <span class="petit ${cl}">${t(lib)}</span> <span class="muet petit">${h((d.modules || []).join(", "))}</span><br><span class="muet petit chemin-court">${h(d.url || t("On this PC"))}${d.vu ? " · " + h(t("Seen {0}", [dateLisible(d.vu)])) : ""}</span>
      ${d.memoire ? `<br><span class="petit attention">${t("Unlock the vault: until then this Athanor's token is kept in memory only (lost if Athanor closes).")}</span>` : d.etat === "coffre" ? `<br><span class="petit attention">${t("The vault is locked: unlock it to reach this Athanor.")}</span>` : d.etat === "revoque" ? `<br><span class="petit attention">${t("The other Athanor revoked this link: remove it, then pair again.")}</span>` : ""}</span><span class="espace"></span>
      <button class="btn btn-petit" data-distant="retirer">${t("Remove")}</button></div>`; }).join("")
    + (ds.attentes || []).map((d) => `<div class="ligne distant-attente"><span><b>${h(d.nom)}</b><br><span class="muet petit chemin-court">${h(d.url || t("On this PC"))}</span></span><span class="espace"></span><span class="petit attention">${t("Waiting for the other one's yes")}</span></div>`).join("");
  return `<div class="bloc local-seul" id="bloc-instances-pc"><h2>${t("Remote Athanors")}</h2>
    ${ds.message ? `<p class="petit attention">${h(t(ds.message))}</p>` : ""}
    ${lies || `<p class="muet petit">${t("No remote Athanor linked yet.")}</p>`}
    <h3>${t("Add a remote Athanor")}</h3>
    <p class="muet petit">${t("Its remote access address and the 6-digit code it shows (Settings › MCP clients › Connect); its owner then accepts this Athanor on its screen. The token is kept in the vault.")}</p>
    <div class="ligne"><input type="url" id="distant-url" placeholder="https://…" aria-label="${t("Remote access address")}"> <input type="text" id="distant-code" inputmode="numeric" maxlength="6" placeholder="${t("6-digit code")}" aria-label="${t("6-digit code")}"> <button class="btn btn-petit" data-distant="ajouter">${t("Add")}</button></div>
    <h3>${t("On this PC")}</h3>
    <p class="muet petit">${t("The other Athanors running on this computer. Pairing is never automatic: type the 6-digit code the other one shows (Settings › MCP clients › Connect), then accept in its window.")}</p>
    ${liste || `<p class="muet petit">${t("No other Athanor is running on this computer.")}</p>`}
    <h3>${t("On the local network")}</h3>
    <label class="ligne"><input type="checkbox" id="mdns-actif" ${v.mdns ? "checked" : ""}> ${t("Discover the Athanors of the local network (mDNS)")}</label>
    <p class="muet petit">${t("Off by default. On: this Athanor tells the computers of your network its name and its remote access address (nothing else, and nothing without remote access), and lists the Athanors that do the same. Nothing is linked without the 6-digit code.")}</p>
    ${v.mdns ? `${v.tunnel ? "" : `<p class="petit attention">${t("Remote access is off: this Athanor announces nothing, it only looks for the others.")}</p>`}
    ${(v.reseau || []).map((i) => `<div class="ligne instance-reseau"><span><b>${h(i.nom || i.id)}</b><br><span class="muet petit chemin-court">${h(i.url)}</span></span></div>`).join("") || `<p class="muet petit">${t("No Athanor found on the local network yet.")}</p>`}
    ${(v.reseau || []).length ? `<p class="muet petit">${t("A proposal only: to link one, add its address with the 6-digit code it shows.")}</p>` : ""}` : ""}</div>`;
}
$("#page").addEventListener("change", async (ev) => {
  if (ev.target.id !== "mdns-actif") return;
  await action(api("instances/mdns", "POST", { actif: ev.target.checked }));
  const n = await api("instances").catch(() => null); const bloc = $("#bloc-instances-pc"); if (n && bloc) bloc.outerHTML = rendreInstancesPC(n);
});
$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-distant]"); if (!b) return;
  const corps = { action: b.dataset.distant };
  if (corps.action === "ajouter") { corps.url = $("#distant-url").value; corps.code = $("#distant-code").value; }
  else { if (!await confirmer(t("Remove this remote Athanor? Its token is revoked on both sides when it answers."), { ok: t("Remove") })) return; corps.id = b.closest(".distant").dataset.id; }
  const v = await action(api("distants", "POST", corps));
  if (v) { if (corps.action === "ajouter") toast(t("Code accepted: accept this Athanor in the other one's window.")); const n = await api("instances").catch(() => null); const bloc = $("#bloc-instances-pc"); if (n && bloc) bloc.outerHTML = rendreInstancesPC(n); }
});
$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-instance]"); if (!b) return;
  const l = b.closest(".instance-pc");
  const v = await action(api("instances", "POST", { id: l.dataset.id, code: l.querySelector(".code-instance").value }));
  if (v) { const n = await api("instances").catch(() => null); const bloc = $("#bloc-instances-pc"); if (n && bloc) bloc.outerHTML = rendreInstancesPC(n); }
});
// the updates of Athanor (maj.go): the version, the latest one published, "Update now"
function rendreBlocMaj(o) {
  const v = etat.vue || {}, m = v.maj || {};
  const quand = m.verifie ? new Date(m.verifie * 1000).toLocaleString(document.documentElement.lang || "en", { dateStyle: "short", timeStyle: "short" }) : "";
  let etatTexte = "";
  if (!m.possible) etatTexte = `<p class="muet petit">${t("This copy of Athanor was built from the source code: update it with git pull, then cd app && go run . construire.")}</p>`;
  else if (m.en_cours === "verification") etatTexte = `<p class="muet petit">${t("Looking for an update…")}</p>`;
  else if (m.en_cours === "telechargement") etatTexte = `<p class="petit">${t("Downloading the update… {0} %", [m.avancement || 0])}</p><div class="barre m0"><i style="width:${Math.min(100, m.avancement || 0)}%"></i></div>`;
  else if (m.en_cours === "redemarrage") etatTexte = `<p class="ok petit">${t("Athanor restarts…")}</p>`;
  else if (m.disponible) etatTexte = `<p class="attention m0"><b>${t("Athanor {0} is available (you have {1})", [h(m.derniere), h(v.version)])}</b></p>`;
  else if (m.derniere) etatTexte = `<p class="ok petit">✔ ${t("Up to date (checked {0}).", [h(quand)])}</p>`;
  if (m.erreur && !m.en_cours) etatTexte += `<p class="attention petit">${h(t(m.erreur))}</p>`;
  const langue = document.documentElement.lang || "en", parties = String(m.notes || "").split(/\n---\n/);
  const notes = m.disponible && m.notes ? `<details class="mt-1"><summary class="petit">${t("What's new")}</summary><div class="notes-maj">${mdChat((langue === "fr" && parties[1] ? parties[1] : parties[0]).trim())}</div></details>` : "";
  const occupe = !!m.en_cours;
  return `<p class="m0"><b>Athanor ${h(v.version || "?")}</b></p>${etatTexte}${notes}
    ${m.possible ? `<div class="ligne mt-1">${m.disponible ? `<button class="btn btn-primaire btn-petit" data-maj="installer" ${occupe ? "disabled" : ""}>${t("Update now")}</button>` : ""}<button class="btn btn-petit" data-maj="verifier" ${occupe ? "disabled" : ""}>${t("Check for updates")}</button>${m.page ? `<a href="#" class="petit" data-lien="${h(m.page)}">${t("Release page")}</a>` : ""}</div>
    <label class="interrupteur mt-1"><input type="checkbox" id="op-maj" data-option ${($("#op-maj") ? $("#op-maj").checked : o.verifier_maj) ? "checked" : ""}><span>${t("Look for an update when Athanor opens")}</span></label>
    <p class="muet petit">${t("Only asks GitHub for the latest version: nothing about you or your projects is sent. The update keeps your token, settings, projects, logs and tools; running pilots go on.")}</p>` : ""}`;
}
async function actionMaj(quoi) {
  if (quoi === "voir") { etat.ongletReglages = "options"; if (etat.page !== "reglages") { location.hash = "reglages"; } else await rendrePage(); setTimeout(() => { const b = $("#bloc-version"); if (b) b.scrollIntoView({ behavior: "smooth", block: "center" }); }, 150); return; }
  if (quoi === "verifier") {
    if (etat.vue && etat.vue.maj) etat.vue.maj.en_cours = "verification";
    if (etat.page === "reglages") await rendrePage();
    const m = await action(api("maj?verifier=1"));
    if (m && etat.vue) etat.vue.maj = m;
    if (m && !m.disponible && !m.erreur) toast(t("Athanor is up to date."));
    return etat.page === "reglages" ? rendrePage() : rafraichir();
  }
  if (quoi === "installer") {
    const m = (etat.vue || {}).maj || {};
    const enCours = ((etat.vue || {}).projets || []).filter((x) => x.actif).map((x) => x.nom);
    const texte = t("Athanor closes, puts version {0} in place and opens again by itself. Your token, settings and projects are kept; running pilots go on.", [m.derniere])
      + (enCours.length ? "\n\n" + t("{0} pilot(s) running ({1}): they go on without a break, on their current version, until they are started again (Stop after cycle, then Start). The app moves to {2} right away.", [enCours.length, enCours.join(", "), m.derniere]) : "");
    if (!await confirmer(texte, { titre: t("Update to {0}", [m.derniere]), ok: t("Update now") })) return;
    const r = await action(api("maj/installer", "POST", {}));
    if (r && etat.vue) etat.vue.maj = r;
    return rendrePage();
  }
}
document.addEventListener("click", (ev) => {
  const b = ev.target.closest("[data-maj]"); if (!b) return;
  ev.preventDefault();
  const quoi = b.dataset.maj === "ouvrir" ? ((etat.vue || {}).maj || {}).disponible ? "voir" : "verifier" : b.dataset.maj;
  if (b.dataset.maj === "ouvrir" && quoi === "verifier") { actionMaj("voir").then(() => actionMaj("verifier")); return; }
  actionMaj(quoi);
});

// the native Windows switch is sent only where it is shown (Windows)
function o_windows() { const b = $("#op-natif"); return !!(b && !b.closest(".bloc").hidden); }
async function enregistrerOptions() {
  const r = await action(api("options", "POST", { kit: $("#op-kit").value, lancer_pilote_au_demarrage: $("#op-lancer").checked, bureau_virtuel: $("#op-bureau").checked, programmes_supplementaires: $("#op-programmes").value, demarrage_windows: $("#op-windows").checked, mode_natif: $("#op-natif") && o_windows() ? $("#op-natif").checked : undefined, verifier_maj: $("#op-maj") ? $("#op-maj").checked : undefined, entendre_bureau: $("#op-son") ? $("#op-son").checked : undefined, terminal_pilote: $("#op-terminal") ? $("#op-terminal").checked : undefined }));
  if (r && document.activeElement && document.activeElement.matches("input[type=checkbox]")) rendrePage();
}

// ----------------------------------------------------------------------------- rendering and events
let numeroRendu = 0;
// --- Access (project page): the vault on the left, this project's grants by environment on the right
// (ARCHITECTURE §Accès); drag an entry onto a column, or tick its box (keyboard, touch). Names only, no value.
const acc = { projet: "" };
const AUTO_ACCES = { migration: "imported", pourvous: "added automatically (For you)" };
function ligneAcces(a, refusee) {
  const k = h(a.entree + "|" + a.env), auto = AUTO_ACCES[a.auto];
  return `<li class="acc-acces${refusee ? " acc-refusee" : ""}">
    <div class="ligne m0"><b class="cp-nom" title="${h(a.nom)}">${h(a.nom)}</b>${auto ? `<span class="badge">${t(auto)}</span>` : ""}${refusee ? `<span class="badge badge-infra">${t("refused in native mode")}</span>` : ""}<button type="button" class="btn btn-petit" data-acc-retirer="${k}">${t("Remove")}</button></div>
    ${a.portee === "infra" ? "" : `<input type="text" class="acc-var" data-acc-var="${k}" value="${h(a.variable)}" maxlength="100" spellcheck="false" aria-label="${h(t("Variable name") + " — " + a.nom + " (" + a.env + ")")}">`}
  </li>`;
}
pages.acces = async () => {
  const projet = (etat.vue && etat.vue.projet && etat.vue.projet.nom) || etat.projet;
  if (!projet) return `<div class="vide">${t("Choose a project")}</div>`;
  const [d, c, en, sv, fo] = await Promise.all([api("acces?projet=" + encodeURIComponent(projet)), api("coffre"),
    api("projets/" + encodeURIComponent(projet) + "/environnements"), api("serveurs").catch(() => ({})), api("fournisseurs").catch(() => ({}))]);
  acc.projet = projet; cof.d = c; acc.inv = sv.serveurs || []; acc.fourn = fo.fournisseurs || [];
  const entrees = (c.entrees || []).slice().sort((a, b) => a.famille.localeCompare(b.famille) || a.nom.localeCompare(b.nom));
  const inj = d.injection || {}, envs = d.envs || ["dev", "staging", "prod"];
  const donne = (id, env) => d.acces.some((a) => a.entree === id && a.env === env);
  const refusee = d.natif && !d.forcer_infra_natif; // C-M218-9: infra grants used by nobody until Force is ticked
  const carte = (x) => `<li class="acc-entree" draggable="true" data-acc-entree="${h(x.id)}">
    <div class="ligne m0"><b class="cp-nom" title="${h(x.note || x.nom)}">${h(x.nom)}</b><span class="badge">${h(x.famille === "secret" ? typeCoffre(x.type) : t("Variable"))}</span>${x.famille === "secret" ? `<span class="badge${x.portee === "infra" ? " badge-infra" : ""}">${x.portee === "infra" ? "infra" : "code"}</span>` : ""}</div>
    <div class="acc-cases" role="group" aria-label="${h(x.nom)}">${envs.map((e) => `<label><input type="checkbox" data-acc-case="${h(x.id + "|" + e)}"${donne(x.id, e) ? " checked" : ""}> ${e}</label>`).join("")}</div>
  </li>`;
  const colonne = (env) => {
    const l = d.acces.filter((a) => a.env === env), code = l.filter((a) => a.portee !== "infra"), infra = l.filter((a) => a.portee === "infra");
    return `<section class="acc-env${env === d.env ? " acc-courant" : ""}" data-acc-env="${env}" aria-label="${env}">
      <h3>${env}${env === d.env ? ` <span class="muet petit">· ${t("current")}</span>` : ""}</h3>
      ${code.length ? `<ul class="acc-liste">${code.map((a) => ligneAcces(a)).join("")}</ul>` : infra.length ? "" : `<p class="muet petit acc-depot">${t("Drop an entry here, or tick it on the left.")}</p>`}
      ${infra.length ? `<div class="acc-garde"><b class="petit">🔒 ${t("Kept by Athanor, never given to the pilot")}</b>
        <p class="muet petit">${t("The keys of your servers stay with Athanor for its own actions (deployments): the pilot never receives them.")}</p>
        <ul class="acc-liste">${infra.map((a) => ligneAcces(a, refusee)).join("")}</ul></div>` : ""}
    </section>`;
  };
  return `<div class="acces">
    <div class="bloc"><h2>${t("Access")}</h2><p class="intro petit">${t("Drag an entry of the vault onto an environment, or tick it: the pilot receives the project keys and variables of the current environment, under the variable name shown.")}</p>
      ${inj.etat === "verrouille" ? `<p class="petit attention">🔒 ${t("The vault is locked: the pilot's file is not updated until you unlock it.")}</p>` : ""}
      ${d.natif ? `<div class="acc-natif"><p class="petit attention">⚠ ${t(d.forcer_infra_natif ? "Forced: infra entries can be linked to this project, and its deployments' build command runs without a barrier. This choice is logged." : "Native Windows mode: this PC has no barrier, any agent can read every file. Infra entries (your servers' keys) are refused for this project; switch to WSL in Settings › Options to keep them out of reach.")}</p>
        <label class="petit"><input type="checkbox" data-acc-forcer${d.forcer_infra_natif ? " checked" : ""}> ${t("Force, knowing the risk")}</label> <a class="petit" href="#options">${t("Options")}</a></div>` : ""}</div>
    <div class="acc-grille">
      <div class="bloc acc-coffre"><h2>${t("Vault")}</h2>
        ${entrees.length ? `<ul class="acc-liste">${entrees.map(carte).join("")}</ul>` : `<p class="muet">${t("The vault is empty.")} <a href="#coffre">${t("Vault")}</a></p>`}</div>
      <div class="bloc acc-projet"><h2>${t("This project")}</h2>
        <div class="acc-envs">${envs.map(colonne).join("")}</div>
        ${(inj.ignorees || []).length ? `<p class="petit attention acc-ignorees">⚠ ${t("Not given to the pilot: a value on several lines does not fit its file ({0}).", [inj.ignorees.join(", ")])}</p>` : ""}
        ${(inj.repli || []).length ? `<div class="acc-repli">${inj.repli.map((n) => `<p class="ligne m0 petit"><code>${h(n)}</code><span class="muet croit">${t("in the old file, not in the vault")}</span><button type="button" class="btn btn-petit" data-acc-importer>${t("Import")}</button></p>`).join("")}</div>` : ""}
      </div>
    </div>
    ${blocEnvironnements(en, sv.serveurs || [], fo.fournisseurs || [], !!(etat.vue || {}).distant)}
  </div>`;
};
// --- Environments (ARCHITECTURE §Environnements): one card per environment, saved whole by POST; prod's policy is locked
const PROTECTIONS_ENV = { aucune: "None (public)", access: "Cloudflare Access (e-mail sign-in)", auth_proxy: "Password in front of the site" };
function blocEnvironnements(en, inventaire, fournisseurs, distant) {
  const ro = distant ? " disabled" : "";
  const options = (liste, val) => liste.map(([v, l]) => `<option value="${h(v)}"${v === val ? " selected" : ""}>${h(l)}</option>`).join("");
  const carte = (nom) => {
    const e = en.environnements[nom], prod = nom === "prod";
    const srv = inventaire.filter((x) => e.serveurs.includes(x.id) || (prod ? (x.roles || []).includes("prod") : !(x.roles || []).includes("prod") || x.roles.includes("dev")));
    const fourn = fournisseurs.map((f) => [f.id, f.nom]);
    if (e.fournisseur && !fourn.some(([id]) => id === e.fournisseur)) fourn.push([e.fournisseur, e.fournisseur]);
    return `<section class="env-carte${prod ? " env-prod" : ""}" data-env="${nom}" aria-label="${nom}">
      <h3>${nom}${prod ? ` <span class="badge">🔒 ${t("approval always")}</span>` : ""}</h3>
      <label class="env-champ">${t("Address (URL)")}<input type="url" data-env-champ="url" value="${h(e.url)}" placeholder="https://app.example.com" maxlength="300" spellcheck="false"${ro}></label>
      <fieldset class="env-serveurs"><legend>${t("Servers")}</legend>
        ${srv.length ? srv.map((x) => `<label><input type="checkbox" data-env-serveur="${h(x.id)}"${e.serveurs.includes(x.id) ? " checked" : ""}${ro}> <span class="croit">${h(x.nom)}</span>${(x.roles || []).map((r) => `<span class="badge">${h(r)}</span>`).join("")}</label>`).join("")
          : `<p class="muet petit">${t("No server for this environment yet.")}${distant ? "" : ` <a href="#serveurs">${t("Servers")}</a>`}</p>`}
        <p class="muet petit">${t(prod ? "Production only runs on servers with the prod role." : "Servers with the prod role alone are kept for production.")}</p>
      </fieldset>
      <label class="env-champ">${t("Protection")}<select data-env-champ="protection"${ro}>${options(Object.entries(PROTECTIONS_ENV).map(([v, l]) => [v, t(l)]), e.protection)}</select></label>
      ${prod ? `<div class="env-champ"><span>${t("Deployment policy")}</span><p class="petit">🔒 ${t("Production always asks for your approval before a deployment: this cannot be turned off.")}</p></div>`
        : `<label class="env-champ">${t("Deployment policy")}<select data-env-champ="politique"${ro}>${options([["accord", t("Ask for my approval")], ["auto", t("Automatic when nothing is destroyed")]], e.politique)}</select></label>`}
      ${nom === "dev" ? `<label class="env-champ env-relais"><span><input type="checkbox" data-env-champ="relais"${e.deployer_au_jalon ? " checked" : ""}${ro}> ${t("Deploy to dev after each milestone delivered")}</span>
        <span class="muet petit">${t("Each time a milestone is ticked, the project's last commit is deployed to dev with its recorded method, then checked. Never production.")}</span></label>` : ""}
      ${fourn.length ? `<label class="env-champ">${t("DNS provider")}<select data-env-champ="fournisseur"${ro}>${options([["", t("None")], ...fourn], e.fournisseur || "")}</select></label>`
        : `<div class="env-champ"><span>${t("DNS provider")}</span><p class="muet petit">${t("None: providers (Cloudflare) are added in Servers.")}</p></div>`}
      <p class="petit attention env-erreur" role="alert" hidden></p>
      ${distant ? "" : `<button type="button" class="btn btn-petit" data-env-enregistrer>${t("Save")}</button>`}
      ${blocSante((en.sante || {})[nom], e)}
      ${blocDeploiements(nom, e, (en.deploiements || {})[nom] || [], distant)}
    </section>`;
  };
  return `<div class="bloc environnements"><h2>${t("Environments")}</h2>
    <p class="intro petit">${t("Where each environment of the project runs: its address, its servers, its protection and whether a deployment waits for your approval.")}</p>
    ${distant ? `<p class="petit muet">${t("Shown only: change the environments on the PC.")}</p>` : ""}
    <div class="env-cartes">${en.envs.map(carte).join("")}</div>
    ${blocCleDeploiement(en, distant)}</div>`;
}
// M133: an environment's last deployments, their external check, and the way back to the previous version
const ETATS_DEPLOI = { ok: ["✔", "Deployed"], echec: ["✖", "Failed"], revenu: ["↩", "Replaced by going back"], en_cours: ["…", "Running"] };
const PROBLEMES_VERIF = { injoignable: "The site does not answer: {1}", tls: "The certificate is not valid: {2}", statut: "Unexpected status {0}", ouvert: "The site opens without identification (status {0})" };
function texteVerif(v, protection) {
  const x = v.http || {}, tls = x.tls || {}, l = [];
  if (v.ok) l.push("✔ " + t("Checked from this PC: status {0}", [x.statut]) + (protection !== "aucune" ? ", " + t("closed without identification") : ""));
  else l.push("⚠ " + (v.problemes || []).map((p) => t(PROBLEMES_VERIF[p] || p, [x.statut, x.erreur || "", tls.erreur_certificat || ""])).join(" · "));
  if (x.tls && tls.certificat_valide) l.push(t("certificate valid, {0} days left", [tls.jours_restants]));
  if ((x.en_tetes_manquants || []).length) l.push(t("missing security headers: {0}", [x.en_tetes_manquants.join(", ")]));
  return l.join(" · ");
}
// C71: a deployment that failed before its switch left the version served as it was: nothing to go back from
const avantBascule = (d) => d.etat === "echec" && !d.retour && !d.bascule && !d.verification;
function blocDeploiements(nom, e, liste, distant) {
  const ligne = (d) => {
    const [icone, etiquette] = d.retour && d.etat === "ok" ? ["↩", "Back to the previous version"] : ETATS_DEPLOI[d.etat] || ["", d.etat];
    return `<li class="env-deploi-${h(d.etat)}"><div class="ligne m0 petit"><span class="badge">${icone} ${h(t(etiquette))}</span><code>${h((d.commit || "").slice(0, 7))}</code>
      <span class="muet">${h(dateLisible(d.debut))} · ${h(t(AUTEURS_DEPLOI[d.auteur] || d.auteur))}</span></div>
      ${d.verification ? `<p class="petit${d.verification.ok ? "" : " attention"}">${h(texteVerif(d.verification, e.protection))}</p>` : d.erreur ? `<p class="petit attention">${h(d.message ? texteMessage(d.message) : t(d.erreur))}</p>` : ""}</li>`;
  };
  return `<div class="env-deploi" data-env-deploi><h4>${t("Deployments")}</h4>
    ${liste.length ? `<ul class="env-deploi-liste">${liste.map(ligne).join("")}</ul>` : `<p class="muet petit">${t("Nothing deployed yet.")}</p>`}
    ${!distant && e.deploiement && liste.length > 1 && !avantBascule(liste[0]) ? `<button type="button" class="btn btn-petit" data-env-revenir>${t("Go back to the previous version")}</button>` : ""}</div>`;
}
// M137: an environment's health (checked every 15 minutes by default) and its incident, if any
const ETATS_SANTE = { ok: ["✔", "Healthy"], alerte: ["⚠", "Warning"], incident: ["✖", "Incident"] };
const PROBLEMES_SANTE = { certificat: "The certificate expires in {0} days", disque: "Disk {1}% full on {0}", memoire: "Memory {1}% used on {0}" };
function blocSante(s, e) {
  if (!s) return "";
  const [icone, etiquette] = ETATS_SANTE[s.etat] || ["", s.etat], i = s.incident;
  const probleme = (p) => p.code === "verif" && s.verification ? texteVerif(s.verification, e.protection) : t(PROBLEMES_SANTE[p.code] || p.code, (p.args || []).map(String));
  return `<div class="env-sante env-sante-${h(s.etat)}" data-env-sante><h4>${t("Health")}</h4>
    <p class="ligne m0 petit"><span class="badge">${icone} ${h(t(etiquette))}</span>${s.t ? `<span class="muet">${h(t("Checked {0}", [dateLisible(s.t)]))}</span>` : ""}</p>
    ${(s.problemes || []).map((p) => `<p class="petit${p.rouge ? " attention" : ""}">${h(probleme(p))}</p>`).join("")}
    ${i ? `<p class="petit attention">${h(t("What failed: {0}.", [i.message ? texteMessage(i.message) : t(i.echec)]))} ${h(t("What was tried: {0}.", [i.tente]))}</p>
      <p class="petit muet">${i.pris_en_main ? h(t("You took over {0}.", [dateLisible(i.pris_en_main)])) : t(i.sans_retour ? "The bell offers: Retry, I take over." : "The bell offers: Retry, Go back to the previous version, I take over.")}</p>` : ""}</div>`;
}
// the Servers page: how often the deployed sites are checked (or not at all)
function reglageSante(sa, distant) {
  const choix = (sa.intervalles || [5, 15, 30, 60]).map((m) => [String(m), t("Every {0} minutes", [m])]).concat([["0", t("Off")]]);
  const val = sa.arret ? "0" : String(sa.minutes || 15);
  return `<div class="ligne m0 petit srv-reglage-sante"><label for="srv-sante">${t("Health of the deployed sites")}</label>
    <select id="srv-sante" data-srv-sante${distant ? " disabled" : ""}>${choix.map(([v, l]) => `<option value="${v}"${v === val ? " selected" : ""}>${h(l)}</option>`).join("")}</select>
    <span class="muet">${sa.dernier && !sa.arret ? h(t("Last check {0}", [dateLisible(sa.dernier)])) : t("Checked from this PC: the site, its certificate, the disk and memory of its servers.")}</span></div>`;
}
$("#page").addEventListener("change", async (ev) => {
  const sel = ev.target.closest("[data-srv-sante]"); if (!sel) return;
  const m = +sel.value;
  if (await action(api("serveurs/sante", "POST", m ? { minutes: m } : { arret: true }))) toast(m ? t("The sites are checked every {0} minutes.", [m]) : t("The sites' health checks are off."), "ok");
});
const AUTEURS_DEPLOI = { agent: "by the agent", relais: "after a milestone", proprietaire: "by you", auto: "automatically, after a failed check" };
$("#page").addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-env-revenir]"); if (!b) return;
  const c = b.closest("[data-env]"), env = c.dataset.env, err = c.querySelector(".env-erreur"), question = t("Serve the previous version of {0} again? It is checked from this PC right after.", [env]);
  const preuve = env === "prod" ? await preuveMdp(question, { ok: t("Go back") }, question) : (await confirmer(question, { ok: t("Go back") })) ? {} : false;
  if (preuve === false) return;
  b.disabled = true; err.hidden = true; b.textContent = t("Going back…");
  try {
    const r = await api("projets/" + encodeURIComponent(acc.projet) + "/revenir", "POST", { env, ...preuve }), tpl = document.createElement("template");
    tpl.innerHTML = blocDeploiements(env, { ...(r.deploiement ? { deploiement: true } : {}), protection: c.querySelector('[data-env-champ="protection"]').value }, r.deploiements || []);
    c.querySelector("[data-env-deploi]").replaceWith(tpl.content.firstElementChild);
    const d = r.deploiement || {};
    toast(d.etat === "ok" ? t("{0} serves its previous version again.", [env]) : d.message ? texteMessage(d.message) : t(d.erreur || "Failed"), d.etat === "ok" ? "ok" : "erreur");
  } catch (e) { err.textContent = e.message; err.hidden = false; b.disabled = false; b.textContent = t("Go back to the previous version"); }
});
// the project's GitHub deployment key (one for every environment), made on demand (M119)
function blocCleDeploiement(en, distant) {
  const cle = en.envs.some((n) => en.environnements[n].cle_deploiement);
  return `<div class="env-cle" data-env-cle-bloc><h3>${t("Deployment key")}</h3>
    <p class="petit">${cle ? `✔ ${t("Created by Athanor, read-only on the repository")}` : t("A read-only key on the project's GitHub repository, kept in the vault: the servers use it to fetch the code.")}</p>
    ${cle || distant ? "" : `<button type="button" class="btn btn-petit" data-env-cle>${t("Create the deployment key")}</button>`}
    <p class="petit attention env-erreur" role="alert" hidden></p></div>`;
}
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "acces") return;
  const b = ev.target.closest("[data-env-cle]"); if (!b) return;
  const err = b.closest("[data-env-cle-bloc]").querySelector(".env-erreur");
  b.disabled = true; err.hidden = true;
  try {
    const r = await api("projets/" + encodeURIComponent(acc.projet) + "/cle-deploiement", "POST", {});
    toast(r.deja ? t("The deployment key already exists.") : t("Deployment key created, read-only on {0}.", [r.depot]), "ok"); rendrePage();
  } catch (e) { err.textContent = e.message; err.hidden = false; b.disabled = false; }
});
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "acces") return;
  const b = ev.target.closest("[data-env-enregistrer]"); if (!b) return;
  const c = b.closest("[data-env]"), err = c.querySelector(".env-erreur"), champ = (k) => c.querySelector(`[data-env-champ="${k}"]`);
  const corps = { env: c.dataset.env, url: champ("url").value.trim(), serveurs: [...c.querySelectorAll("[data-env-serveur]:checked")].map((x) => x.dataset.envServeur),
    protection: champ("protection").value, politique: champ("politique") ? champ("politique").value : "accord", fournisseur: champ("fournisseur") ? champ("fournisseur").value : "", deployer_au_jalon: !!(champ("relais") && champ("relais").checked) };
  b.disabled = true; err.hidden = true;
  try {
    const en = await api("projets/" + encodeURIComponent(acc.projet) + "/environnements", "POST", corps), tpl = document.createElement("template");
    tpl.innerHTML = blocEnvironnements(en, acc.inv, acc.fourn, false); // B74: only this card is redrawn, the others keep what is typed
    c.replaceWith(tpl.content.querySelector(`[data-env="${corps.env}"]`)); toast(t("Environment {0} saved.", [corps.env]), "ok");
  } catch (e) { err.textContent = e.message; err.hidden = false; b.disabled = false; } // the server's reason in the card, the fields kept
});
// C25 C35 C37: what an agent of this PC must not decide is retyped with Athanor's password in this window (a paired
// phone, unlocked with it, passes): {} or {mot_de_passe} for the request, false when cancelled; sinon: asked anyway without the field
async function preuveMdp(texte, options, sinon) {
  const ap = await api("hub/approbations").catch(() => ({}));
  if (!ap.mot_de_passe_requis) return sinon && !(await confirmer(sinon, options)) ? false : {};
  const mdp = await confirmer(texte, { ...options, mdp: true });
  return mdp === false ? false : { mot_de_passe: mdp };
}
async function changerAcces(corps) {
  if (corps.action === "ajouter" || (corps.action === "forcer" && corps.forcer)) {
    const p = await preuveMdp(t("Giving a key to a project: retype Athanor's password."), { ok: t("Give the key") });
    if (!p) return rendrePage();
    corps = { ...corps, ...p };
  }
  const r = await action(api("acces", "POST", { projet: acc.projet, ...corps }));
  rendrePage(); // an error re-renders too: the box goes back to what the server holds
  return r;
}
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "acces") return;
  const b = ev.target.closest("[data-acc-retirer], [data-acc-importer]"); if (!b) return;
  if (b.dataset.accRetirer) { const [entree, env] = b.dataset.accRetirer.split("|"); return changerAcces({ action: "retirer", entree, env }); }
  b.disabled = true;
  const r = await action(api("coffre/migration", "POST")); if (r) toast(t("{0} keys imported.", [r.importees]), "ok");
  rendrePage();
});
$("#page").addEventListener("change", (ev) => {
  if (etat.page !== "acces") return;
  const x = ev.target;
  if (x.dataset.accCase) { const [entree, env] = x.dataset.accCase.split("|"); return changerAcces({ action: x.checked ? "ajouter" : "retirer", entree, env }); }
  if (x.matches("[data-acc-forcer]")) return forcerInfraNatif(x.checked);
  if (x.dataset.accVar) { const [entree, env] = x.dataset.accVar.split("|"); return changerAcces({ action: "renommer", entree, env, variable: x.value.trim() }); }
});
// native Windows mode: infra entries need a confirmed, logged "force" per project
async function forcerInfraNatif(oui) {
  if (oui && !(await confirmer(t("Once forced, an agent working on this PC could read this project's infra entries, and its deployments' build command would run without a barrier. Force anyway? This choice is logged."), { ok: t("Force"), danger: true }))) return rendrePage();
  return changerAcces({ action: "forcer", forcer: oui, confirme: oui });
}
$("#page").addEventListener("dragstart", (ev) => {
  const e = ev.target.closest && ev.target.closest("[data-acc-entree]"); if (!e) return;
  ev.dataTransfer.setData("text/plain", e.dataset.accEntree); ev.dataTransfer.effectAllowed = "copy";
});
for (const type of ["dragover", "dragleave", "drop"]) $("#page").addEventListener(type, (ev) => {
  const c = ev.target.closest && ev.target.closest("[data-acc-env]"); if (!c) return;
  if (type !== "dragleave") ev.preventDefault();
  c.classList.toggle("acc-survol", type === "dragover");
  if (type !== "drop") return;
  const entree = ev.dataTransfer.getData("text/plain"), env = c.dataset.accEnv;
  if (entree && !c.querySelector(`[data-acc-retirer="${CSS.escape(entree + "|" + env)}"]`)) changerAcces({ action: "ajouter", entree, env });
});
// --- Vault (Coffre module): the entries of /api/coffre, one tab per family; a secret's value never comes back
const cof = { onglet: "secret", form: null, colle: null, d: { entrees: [], etat: {} } }; // colle: {texte, props} of "Paste"
const TYPES_COFFRE = ["cloudflare", "ssh", "certificat", "github", "aws", "anthropic", "openai", "stripe", "mot_de_passe", "jeton", "smtp", "autre"];
const INFRA_COFFRE = new Set(["cloudflare", "ssh", "certificat", "github", "aws"]); // the scope proposed by type (ARCHITECTURE §Coffre)
const typeCoffre = (x) => ({ ssh: t("SSH key"), certificat: t("Certificate"), mot_de_passe: t("Password"), jeton: t("Token"), smtp: t("E-mail account"), autre: t("Other"),
  cloudflare: "Cloudflare", github: "GitHub", aws: "AWS", anthropic: "Anthropic", openai: "OpenAI", stripe: "Stripe" })[x] || x;
const entreeCoffre = (id) => (cof.d.entrees || []).find((x) => x.id === id);
const TESTABLES_COFFRE = new Set(["cloudflare", "github", "ssh"]); // the types POST /api/coffre/tester checks
// "Test": the server checks the key and answers {ok, resultat, note} as translation keys, never the value;
// the proposed note fills an empty note field, otherwise a button offers it (the field stays editable)
async function testerCoffre(corps, sortie, champNote) {
  sortie.hidden = false; sortie.innerHTML = `<span class="muet">${t("Testing…")}</span>`;
  const r = await action(api("coffre/tester", "POST", corps));
  if (!r) { sortie.hidden = true; return; }
  const n = r.note ? t(r.note.cle, r.note.args) : "";
  if (n && !champNote.value.trim()) champNote.value = n;
  sortie.innerHTML = `<span class="${r.ok ? "ok" : "echec"}">${r.ok ? "✓" : "✗"}</span> ${r.resultat.map((m) => h(t(m.cle, m.args))).join("<br>")}`
    + (n && champNote.value.trim() !== n ? ` <button type="button" class="btn btn-petit" data-cof-note="${h(n)}">${t("Use as note")}</button>` : "");
}
function carteCoffre(x) {
  const secret = x.famille === "secret", infra = x.portee === "infra";
  return `<div class="bloc carte-coffre" data-coffre-id="${h(x.id)}">
    <div class="cp-tete"><b class="cp-nom" title="${h(x.nom)}">${h(x.nom)}</b>${secret ? `<span class="badge">${h(typeCoffre(x.type))}</span><span class="badge${infra ? " badge-infra" : ""}" title="${h(infra ? t("Server key: opens only with your password") : t("Project key: reopens alone on this PC"))}">${infra ? "infra" : "code"}</span>` : ""}
      <details class="menu-coffre"><summary class="btn btn-petit" aria-label="${h(t("Actions"))}">⋯</summary><div class="menu-coffre-liste">
        ${secret && TESTABLES_COFFRE.has(x.type) ? `<button type="button" data-cof-tester="${h(x.id)}">${t("Test")}</button>` : ""}<button type="button" data-cof-modifier="${h(x.id)}">${t("Edit")}</button><button type="button" data-cof-remplacer="${h(x.id)}">${t("Replace the value")}</button><button type="button" class="danger" data-cof-supprimer="${h(x.id)}">${t("Delete")}</button>
      </div></details></div>
    <div class="petit cof-valeur">${secret ? `<span class="muet">•••• ${t("saved")}</span>` : `<code>${h(x.valeur)}</code>`}</div>
    <div class="petit cof-note${x.note ? "" : " muet"}" title="${h(x.note)}">${x.note ? h(x.note) : t("No note for the AI")}</div>
    <div class="muet petit">${x.source ? h(t("Imported from {0}", [x.source])) + " · " : ""}${h(dateLisible(x.modifie))}</div>
  </div>`;
}
// the add/edit form: a secret's field stays empty ("•••• saved"), typing in it replaces the value
function formulaireCoffre() {
  const x = cof.form.id && entreeCoffre(cof.form.id);
  if (cof.form.id && !x) { cof.form = null; return ""; }
  const secret = (x ? x.famille : cof.onglet) === "secret", mdp = cof.d.etat.mot_de_passe;
  const type = x ? x.type : secret ? "jeton" : "autre", portee = x ? x.portee : "code";
  return `<form class="bloc cof-form" id="cof-form" autocomplete="off">
    <h2>${x ? h(x.nom) : secret ? t("New secret") : t("New variable")}</h2>
    <label class="champ"><span>${t("Name")}</span><input type="text" id="cof-nom" maxlength="100" spellcheck="false" value="${h(x ? x.nom : "")}" placeholder="${secret ? "CLOUDFLARE_API_TOKEN" : "NODE_ENV"}"></label>
    ${secret ? `<div class="cof-choix">
      <label class="champ"><span>${t("Type")}</span><select id="cof-type">${TYPES_COFFRE.map((y) => `<option value="${y}"${y === type ? " selected" : ""}>${h(typeCoffre(y))}</option>`).join("")}</select></label>
      <label class="champ"><span>${t("Scope")}</span><select id="cof-portee"><option value="code">${t("code — reopens alone on this PC")}</option><option value="infra"${portee === "infra" ? " selected" : ""}${mdp ? "" : " disabled"}>${t("infra — only with your password")}</option></select></label>
    </div>${mdp ? "" : `<p class="muet petit">${t("The infra scope needs a password for Athanor.")} <a href="#options@bloc-verrou">${t("Settings › Options › Lock")}</a></p>`}` : ""}
    <label class="champ"><span>${t("Note for the AI")}</span><textarea id="cof-note" rows="2" maxlength="2000" placeholder="${h(t("What it is for, e.g. DNS of example.org"))}">${h(x ? x.note : "")}</textarea></label>
    <label class="champ"><span>${t("Value")}</span><input type="${secret ? "password" : "text"}" id="cof-valeur" autocomplete="new-password" spellcheck="false" value="${h(x && !secret ? x.valeur : "")}" placeholder="${x && secret ? "•••• " + h(t("saved")) : ""}"></label>
    ${x && secret ? `<p class="muet petit">${t("The value is never shown again: type a new one to replace it, or leave the field empty.")}</p>` : ""}
    ${x && secret && TESTABLES_COFFRE.has(x.type) ? `<div class="cof-test-bloc">
      ${x.type === "ssh" ? `<label class="champ"><span>${t("Server to try (optional)")}</span><input type="text" id="cof-hote" maxlength="255" spellcheck="false" placeholder="example.org:22"></label>` : ""}
      <div class="ligne m0"><button type="button" class="btn" data-cof-test>${t("Test")}</button><span class="muet petit">${t("Athanor checks the key with its provider and proposes a note for the AI.")}</span></div>
      <div class="petit cof-resultat" id="cof-test" aria-live="polite" hidden></div></div>` : ""}
    <div class="ligne"><button type="submit" class="btn btn-primaire">${x ? t("Save") : t("Add")}</button><button type="button" class="btn" data-cof-annuler>${t("Cancel")}</button></div>
  </form>`;
}
// "Paste": the text is analysed by the server, which proposes entries (never a value); saving sends the text back
function formulaireColle() {
  const c = cof.colle, mdp = cof.d.etat.mot_de_passe;
  const ligne = (p, i) => `<div class="cof-prop" data-cof-prop="${i}">
    <label class="ligne m0"><input type="checkbox" class="cof-garder" checked aria-label="${h(t("Keep"))}"><b>${h(p.famille === "secret" ? t("Secret") : t("Variable"))}</b></label>
    <input type="text" class="cof-p-nom" maxlength="100" spellcheck="false" value="${h(p.nom)}" aria-label="${h(t("Name"))}">
    ${p.famille === "secret" ? `<select class="cof-p-type" aria-label="${h(t("Type"))}">${TYPES_COFFRE.map((y) => `<option value="${y}"${y === p.type ? " selected" : ""}>${h(typeCoffre(y))}</option>`).join("")}</select>
    <select class="cof-p-portee" aria-label="${h(t("Scope"))}"><option value="code">code</option><option value="infra"${p.portee === "infra" ? " selected" : ""}${mdp ? "" : " disabled"}>infra</option></select>` : ""}
    <code class="petit">${h(p.indice)}</code>
    <input type="text" class="cof-p-note" maxlength="2000" placeholder="${h(t("Note for the AI"))}" aria-label="${h(t("Note for the AI"))}">
    ${p.famille === "secret" ? `<button type="button" class="btn btn-petit" data-cof-p-tester>${t("Test")}</button><div class="petit cof-resultat" aria-live="polite" hidden></div>` : ""}
  </div>`;
  return `<form class="bloc cof-form" id="cof-colle" autocomplete="off">
    <h2>${t("Paste")}</h2>
    <label class="champ"><span>${t("A .env block, an MCP configuration, a token or a key: Athanor recognises what it is.")}</span>
      <textarea id="cof-texte" rows="4" spellcheck="false" autocomplete="off">${h(c.texte || "")}</textarea></label>
    ${c && c.props ? c.props.length ? `<div class="cof-props">${c.props.map(ligne).join("")}</div>` : `<p class="muet">${t("Nothing to save was found in this text.")}</p>` : ""}
    <div class="ligne">${c && c.props && c.props.length ? `<button type="submit" class="btn btn-primaire" data-cof-enregistrer>${t("Save")}</button><button type="button" class="btn" data-cof-analyser>${t("Recognise")}</button>` : `<button type="submit" class="btn btn-primaire" data-cof-analyser>${t("Recognise")}</button>`}<button type="button" class="btn" data-cof-annuler>${t("Cancel")}</button></div>
  </form>`;
}
async function analyserColle() {
  const texte = $("#cof-texte").value;
  if (!texte.trim()) return;
  const r = await action(api("coffre/coller", "POST", { texte }));
  if (r) { cof.colle = { texte, analyse: texte, props: r.propositions || [] }; rendrePage(); }
}
pages.coffre = async () => {
  const d = await api("coffre"); cof.d = d;
  const e = d.etat || {}, m = d.migration || {}, entrees = d.entrees || [], n = (f) => entrees.filter((x) => x.famille === f).length;
  const liste = entrees.filter((x) => x.famille === cof.onglet).sort((a, b) => a.nom.localeCompare(b.nom));
  const onglet = (f, nom) => `<button type="button" class="${cof.onglet === f ? "actif" : ""}" data-cof-onglet="${f}">${nom} <span class="muet">${n(f)}</span></button>`;
  return `<div class="coffre">
    ${e.mot_de_passe && !e.infra ? `<form class="bloc cof-verrou" id="cof-verrou"><span class="croit">🔒 ${t("Vault locked: the keys of your servers open only with your password")}</span>
      <span class="ligne m0"><input type="password" id="cof-mdp" autocomplete="current-password" placeholder="${h(t("Password"))}" aria-label="${h(t("Password"))}"><button type="submit" class="btn btn-primaire">${t("Unlock")}</button></span></form>` : ""}
    <div class="ligne cof-tete"><div class="onglets">${onglet("secret", t("Secrets"))}${onglet("variable", t("Variables"))}</div><span class="espace"></span><button type="button" class="btn" data-cof-coller>${t("Paste")}</button><button type="button" class="btn btn-primaire" data-cof-ajouter>+ ${t("Add")}</button></div>
    ${e.mot_de_passe ? "" : `<p class="petit attention ligne m0 cof-sans-mdp"><span class="croit">${t("Set a password for Athanor to keep the keys of your servers in the vault.")}</span><a class="btn btn-petit" href="#options@bloc-verrou">${t("Set a password")}</a></p>`}
    ${m.importees ? `<p class="muet petit cof-migration">${t("{0} keys imported from your old files.", [m.importees])} <button type="button" class="btn btn-petit" data-cof-mig-annuler>${t("Undo the import")}</button></p>`
      : m.annulee ? `<p class="muet petit cof-migration">${t("Import from your old files undone (the files themselves are unchanged).")} <button type="button" class="btn btn-petit" data-cof-mig-importer>${t("Import again")}</button></p>` : ""}
    ${cof.colle ? formulaireColle() : cof.form ? formulaireCoffre() : ""}
    ${liste.length ? `<div class="grille-projets">${liste.map(carteCoffre).join("")}</div>` : `<div class="vide">${cof.onglet === "secret" ? t("No secret yet.") : t("No variable yet.")}</div>`}
    <p class="muet petit cof-explication">${t("Your project keys reopen alone on this PC; the keys of your servers open only with your password, and a copy of the disk opens nothing.")}</p>
  </div>`;
};
// rendrePage, then the field asked for gets the focus
const rendreCoffre = (champ) => rendrePage().then(() => { const c = champ && document.getElementById(champ); if (c) c.focus(); });
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "coffre") return;
  const b = ev.target.closest("[data-cof-onglet], [data-cof-ajouter], [data-cof-coller], [data-cof-analyser]:not([type=submit]), [data-cof-annuler], [data-cof-modifier], [data-cof-remplacer], [data-cof-supprimer], [data-cof-tester], [data-cof-test], [data-cof-p-tester], [data-cof-note], [data-cof-mig-annuler], [data-cof-mig-importer]");
  if (!b) return;
  const d = b.dataset, menu = b.closest("details"); if (menu) menu.open = false;
  if (d.cofOnglet) { cof.onglet = d.cofOnglet; cof.form = cof.colle = null; return rendrePage(); }
  if (d.cofMigAnnuler !== undefined) {
    if (!await confirmer(t("Remove the {0} keys imported from your old files? The old files stay as they are.", [cof.d.migration.importees]), { danger: true, ok: t("Undo the import") })) return;
    const r = await action(api("coffre/migration/annuler", "POST")); if (r) { toast(t("{0} keys removed from the vault.", [r.retirees]), "ok"); rendrePage(); }
    return;
  }
  if (d.cofMigImporter !== undefined) {
    const r = await action(api("coffre/migration", "POST")); if (r) { toast(t("{0} keys imported.", [r.importees]), "ok"); rendrePage(); }
    return;
  }
  if (d.cofAnnuler !== undefined) { cof.form = cof.colle = null; return rendrePage(); }
  if (d.cofColler !== undefined) { cof.form = null; cof.colle = {}; return rendreCoffre("cof-texte"); }
  if (d.cofAnalyser !== undefined) return analyserColle();
  if (d.cofAjouter !== undefined) { cof.form = {}; cof.colle = null; return rendreCoffre("cof-nom"); }
  if (d.cofNote !== undefined) { (b.closest("[data-cof-prop]")?.querySelector(".cof-p-note") || $("#cof-note")).value = d.cofNote; return b.remove(); }
  if (d.cofPTester !== undefined) {
    const l = b.closest("[data-cof-prop]"), type = l.querySelector(".cof-p-type");
    return testerCoffre({ texte: cof.colle.analyse, i: +l.dataset.cofProp, type: type && type.value }, l.querySelector(".cof-resultat"), l.querySelector(".cof-p-note"));
  }
  if (d.cofTester) { cof.form = { id: d.cofTester }; cof.colle = null; await rendreCoffre("cof-note"); }
  if (d.cofTester || d.cofTest !== undefined) {
    const hote = $("#cof-hote");
    return testerCoffre({ id: cof.form.id, type: $("#cof-type").value, hote: hote ? hote.value.trim() : "" }, $("#cof-test"), $("#cof-note"));
  }
  if (d.cofModifier || d.cofRemplacer) { cof.form = { id: d.cofModifier || d.cofRemplacer }; cof.colle = null; return rendreCoffre(d.cofRemplacer ? "cof-valeur" : "cof-nom"); }
  const x = entreeCoffre(d.cofSupprimer); if (!x) return;
  if (!await confirmer(t("Delete {0} from the vault? This cannot be undone.", [x.nom]), { danger: true, ok: t("Delete") })) return;
  if (await action(api("coffre/" + encodeURIComponent(x.id), "DELETE"))) { if (cof.form && cof.form.id === x.id) cof.form = null; toast(t("{0} deleted.", [x.nom]), "ok"); rendrePage(); }
});
// a type proposes its scope (infra only when Athanor has a password)
$("#page").addEventListener("input", (ev) => { if (ev.target.id === "cof-texte" && cof.colle) cof.colle.texte = ev.target.value; });
$("#page").addEventListener("change", (ev) => {
  if (ev.target.matches(".cof-p-type")) {
    const p = ev.target.parentElement.querySelector(".cof-p-portee"), infra = INFRA_COFFRE.has(ev.target.value) && !p.querySelector('[value="infra"]').disabled;
    p.value = infra ? "infra" : "code";
  }
  if (ev.target.id !== "cof-type") return;
  const p = $("#cof-portee"), infra = INFRA_COFFRE.has(ev.target.value) && !p.querySelector('[value="infra"]').disabled;
  p.value = infra ? "infra" : "code";
});
$("#page").addEventListener("submit", async (ev) => {
  if (etat.page !== "coffre") return;
  ev.preventDefault();
  if (ev.target.id === "cof-verrou") {
    const champ = $("#cof-mdp");
    if (await action(api("deverrouiller", "POST", { mot_de_passe: champ.value }))) { champ.value = ""; rendrePage(); }
    return;
  }
  if (ev.target.id === "cof-colle") {
    const c = cof.colle; // a text changed since its analysis is analysed again, never saved with the old proposals
    if (!c.props || !c.props.length || c.texte !== c.analyse || ev.submitter && ev.submitter.dataset.cofAnalyser !== undefined) return analyserColle();
    const enregistrer = [...document.querySelectorAll("[data-cof-prop]")].filter((l) => l.querySelector(".cof-garder").checked).map((l) => {
      const q = (c) => l.querySelector(c), o = { i: +l.dataset.cofProp, nom: q(".cof-p-nom").value.trim(), note: q(".cof-p-note").value.trim() };
      if (q(".cof-p-type")) { o.type = q(".cof-p-type").value; o.portee = q(".cof-p-portee").value; }
      return o;
    });
    if (!enregistrer.length) return;
    const r = await action(api("coffre/coller", "POST", { texte: c.analyse, enregistrer }));
    if (!r) return;
    cof.colle = null; // the pasted text leaves the page
    toast(r.entrees.length === 1 ? t("{0} added.", [r.entrees[0].nom]) : t("{0} entries added.", [r.entrees.length]), "ok");
    return rendrePage();
  }
  if (ev.target.id !== "cof-form") return;
  const x = cof.form.id && entreeCoffre(cof.form.id), secret = (x ? x.famille : cof.onglet) === "secret";
  const corps = { nom: $("#cof-nom").value.trim(), note: $("#cof-note").value.trim() }, valeur = $("#cof-valeur").value;
  if (secret) { corps.type = $("#cof-type").value; corps.portee = $("#cof-portee").value; }
  if (!x) { corps.famille = cof.onglet; if (!secret) corps.type = "autre"; }
  if (!x || (secret ? valeur !== "" : valeur !== x.valeur)) corps.valeur = valeur; // an empty secret field keeps the value
  if (x && x.portee === "infra" && corps.portee === "code") { // C35: code reopens alone on this PC
    const p = await preuveMdp(t("Moving a key from infra to code: retype Athanor's password."), { ok: t("Move to code") });
    if (!p) return;
    Object.assign(corps, p);
  }
  const r = await action(api(x ? "coffre/" + encodeURIComponent(x.id) : "coffre", "POST", corps));
  if (!r) return;
  $("#cof-valeur").value = ""; cof.form = null;
  toast(x ? t("{0} saved.", [r.nom]) : t("{0} added.", [r.nom]), "ok"); rendrePage();
});

// --- Audit log (#audit): what the modules asked, answered and did, newest first; read only (also from a paired device)
const aud = { f: { mois: "", projet: "", type: "", q: "" }, pages: 1, verif: null };
const TYPES_AUDIT = { demande: "Requests", reponse: "Answers", evenement: "Events" };
const RAISONS_AUDIT = { modifiee: "changed", retiree: "removed", inseree: "inserted", illisible: "unreadable" };
const quiAudit = (x) => x === "fenetre" ? t("window") : x === "hub" ? "hub" : x && x.startsWith("appareil:") ? t("device {0}", [x.slice(9)])
  : t((etat.hubModules || []).find((m) => m.id === x)?.nom || x || "?"); // a module by its name (dev: Studio); the hub itself stays "hub", not its home page
function ligneAudit(l, lang) {
  const d = new Date(l.t), heure = isNaN(d) ? l.t : d.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" });
  const qui = [l.mission, l.agent].filter(Boolean).join(" · ");
  return `<li class="aud-ligne aud-${h(l.type)}"><span class="muet aud-heure">${h(heure)}</span>
    <span class="aud-texte"><b>${h(quiAudit(l.de))}</b> → ${h(quiAudit(l.vers))}${l.projet ? ", " + h(l.projet) : ""} : <code>${h(l.action || l.type)}</code>${l.resultat ? " — " + h(l.resultat) : ""}${qui ? ` <span class="muet">(${h(qui)})</span>` : ""}</span>
    <span class="muet petit aud-n" title="${h(l.id)}">#${l.n}</span></li>`;
}
// --- Servers (#serveurs): inventory cards and the add wizard (address → fingerprint to confirm → one of three ways)
const srv = { aj: null, texte: "", ouvertes: new Set() }; // aj: the wizard ({etape: "adresse" | "empreinte" | "cle" | "coupure", …}), kept across renders
function carteServeur(x, fiche, envs, santeSites) {
  const sante = { ok: t("Reachable"), injoignable: t("Unreachable"), empreinte_changee: t("Host fingerprint changed") }[x.etat] || t("Waiting for its key"), cls = x.etat === "ok" ? "travaille" : "attend", f = fiche || {};
  const tourne = [...(f.conteneurs || []).filter((c) => /^(up|running)/i.test(c.etat)).map((c) => c.nom), ...(f.services || [])];
  const resume = !fiche ? t("Its sheet is not made yet.")
    : tourne.length ? t("Running: {0}", [tourne.slice(0, 6).join(", ") + (tourne.length > 6 ? " +" + (tourne.length - 6) : "")]) : t("Nothing seen running.");
  return `<div class="bloc carte-serveur" data-srv-id="${h(x.id)}">
    <div class="cp-tete"><a class="cp-nom" href="#serveur/${encodeURIComponent(x.id)}"><b>${h(x.nom)}</b></a><span class="pastille ${cls}">${sante}</span></div>
    <div class="muet petit"><code>${h(x.utilisateur)}@${h(x.hote)}${x.port === 22 ? "" : ":" + x.port}</code>${f.systeme && f.systeme.nom ? " · " + h(f.systeme.nom) : ""}</div>
    <div class="ligne m0">${(x.roles || []).length ? x.roles.map((r) => `<span class="badge${r === "prod" ? " badge-infra" : ""}">${h(r)}</span>`).join("") : `<span class="muet petit">${t("No role yet")}</span>`}${(envs || []).length ? `<span class="badge badge-env" title="${h(t("Used by") + " " + envs.join(", "))}">${h(t("Environments: {0}", [envs.length]))}</span>` : ""}</div>
    <div class="petit srv-tourne" title="${h(tourne.join(", "))}">${h(resume)}</div>
    ${(envs || []).length ? `<div class="ligne m0 petit srv-sante">${envs.map((pe) => { const st = ((santeSites || {}).envs || {})[pe], [ic, et] = ETATS_SANTE[st] || ["·", "Not checked yet"];
      return `<span class="badge srv-sante-${h(st || "aucun")}" title="${h(t(et))}">${ic} ${h(pe)}</span>`; }).join("")}</div>` : ""}
    <div class="muet petit">${x.vu ? h(t("Seen {0}", [dateLisible(x.vu)])) : h(t("Added {0}", [dateLisible(x.cree)]))}</div>
    ${x.etat === "inconnu" && !(etat.vue || {}).distant ? `<div class="ligne m0"><button type="button" class="btn btn-petit btn-primaire" data-srv-verifier="${h(x.id)}">${t("Verify")}</button><span class="muet petit">${t("once its public key is on the server")}</span></div>` : ""}
  </div>`;
}
// what the vault lets the wizard do: no password for Athanor, or locked (null from a paired device)
function messagesCoffreServeurs(c) {
  const e = (c || {}).etat || {};
  if (!c) return "";
  if (!e.mot_de_passe) return `<p class="bloc petit attention srv-msg">⚠ ${t("No password for Athanor: the keys of your servers need one (they open only with it).")} <a href="#options@bloc-verrou">${t("Settings › Options › Lock")}</a></p>`;
  if (!e.infra) return `<form class="bloc cof-verrou" id="srv-verrou"><span class="croit">🔒 ${t("Vault locked: the keys of your servers open only with your password")}</span>
      <span class="ligne m0"><input type="password" id="srv-deverrou" autocomplete="current-password" placeholder="${h(t("Password"))}" aria-label="${h(t("Password"))}"><button type="submit" class="btn btn-primaire">${t("Unlock")}</button></span></form>`;
  return "";
}
function assistantServeur(c) {
  const a = srv.aj, cles = ((c || {}).entrees || []).filter((e) => e.famille === "secret" && e.type === "ssh"), v = etat.vue || {};
  const role = (r) => `<label class="radio"><input type="checkbox" class="srv-role" value="${r}"${(a.roles || []).includes(r) ? " checked" : ""}> ${r === "dev" ? t("dev (tests, previews)") : t("prod (your live site)")}</label>`;
  if (a.etape === "cle") return `<div class="bloc cof-form srv-form" id="srv-cle">
    <h2>${h(t("Put this key on {0}", [a.serveur.nom]))}</h2>
    <p class="petit">${t("Athanor made a key for this server and keeps its private part in the vault. Add its public part to the server, then press Verify.")}</p>
    <label class="champ"><span>${t("Public key")}</span><textarea readonly rows="2" class="code" id="srv-public">${h(a.public)}</textarea></label>
    <div class="ligne m0"><button type="button" class="btn btn-petit" data-srv-copier="srv-public">${t("Copy")}</button></div>
    <label class="champ"><span>${h(t("Or run this command on the server (as {0})", [a.serveur.utilisateur]))}</span><textarea readonly rows="3" class="code" id="srv-commande">${h(a.commande)}</textarea></label>
    <div class="ligne m0"><button type="button" class="btn btn-petit" data-srv-copier="srv-commande">${t("Copy")}</button></div>
    <div class="ligne"><button type="button" class="btn btn-primaire" data-srv-verifier="${h(a.serveur.id)}">${t("Verify")}</button><button type="button" class="btn" data-srv-fermer>${t("Later")}</button></div>
  </div>`;
  if (a.etape === "coupure") return `<div class="bloc cof-form srv-form" id="srv-coupure">
    <h2>${h(t("{0} added", [a.serveur.nom]))}</h2>
    <p class="petit">${t("Athanor now connects with its own key; your password was not kept. Cut password logins on this server? Nobody could then try passwords on it (recommended).")}</p>
    <div class="ligne"><button type="button" class="btn btn-primaire" data-srv-couper="${h(a.serveur.id)}">${t("Cut password logins")}</button><button type="button" class="btn" data-srv-fermer>${t("Later")}</button></div>
  </div>`;
  const empreinte = a.etape === "empreinte", fige = empreinte ? " readonly" : "";
  return `<form class="bloc cof-form srv-form" id="srv-form" autocomplete="off">
    <h2>${t("Add a server")}</h2>
    <div class="cof-choix">
      <label class="champ"><span>${t("Address")}</span><input type="text" id="srv-hote" maxlength="253" spellcheck="false" placeholder="vps.example.org" value="${h(a.hote || "")}"${fige} required></label>
      <label class="champ"><span>${t("Port")}</span><input type="number" id="srv-port" min="1" max="65535" value="${a.port || 22}"${fige}></label>
      <label class="champ"><span>${t("User")}</span><input type="text" id="srv-utilisateur" maxlength="32" spellcheck="false" placeholder="root" value="${h(a.utilisateur || "")}"${fige} required></label>
      <label class="champ"><span>${t("Name (optional)")}</span><input type="text" id="srv-nom" maxlength="100" placeholder="${h(t("e.g. Production VPS"))}" value="${h(a.nom || "")}"></label>
    </div>
    <div class="champ"><span>${t("Roles")}</span><div class="ligne m0">${role("dev")}${role("prod")}</div></div>
    ${empreinte ? `<div class="srv-empreinte">
      <p class="petit">${t("The server presents this fingerprint. Check that it is your server's (on the server: {0}), then confirm it: Athanor will refuse any other.", ["<code>ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub</code>"])}</p>
      <p><code class="srv-empreinte-valeur">${h(a.empreinte)}</code></p>
      <label class="radio"><input type="checkbox" id="srv-confirme"${a.confirme ? " checked" : ""} required> ${t("It is my server's fingerprint")}</label>
      <fieldset class="srv-facons"><legend>${t("How should Athanor connect?")}</legend>
        <label class="radio srv-facon"><input type="radio" name="srv-mode" value="coffre"${a.mode === "coffre" ? " checked" : ""}${cles.length ? "" : " disabled"}> <span><b>${t("A key of the vault")}</b><br><span class="muet petit">${cles.length ? t("An SSH key you already use for this server.") : t("No SSH key in the vault yet.")}</span></span></label>
        ${a.mode === "coffre" && cles.length ? `<div class="srv-sous"><select id="srv-cle-coffre" aria-label="${h(t("A key of the vault"))}">${cles.map((e) => `<option value="${h(e.id)}"${e.id === a.cle ? " selected" : ""}>${h(e.nom)}</option>`).join("")}</select>
          ${v.natif ? `<label class="champ"><span>${t("Athanor's password")}</span><input type="password" id="srv-mdp-athanor" autocomplete="current-password"></label><p class="muet petit">${t("Native Windows mode: any program of this PC can drive this window, so giving an existing server key asks for Athanor's password.")}</p>` : ""}</div>` : ""}
        <label class="radio srv-facon"><input type="radio" name="srv-mode" value="generee"${a.mode === "generee" ? " checked" : ""}> <span><b>${t("A new key made by Athanor")}</b><br><span class="muet petit">${t("You copy its public part onto the server, then press Verify.")}</span></span></label>
        <label class="radio srv-facon"><input type="radio" name="srv-mode" value="mot_de_passe"${a.mode === "mot_de_passe" ? " checked" : ""}> <span><b>${t("The server's password, once")}</b><br><span class="muet petit">${t("Athanor uses it once to put its own key, and never keeps it.")}</span></span></label>
        ${a.mode === "mot_de_passe" ? `<div class="srv-sous"><input type="password" id="srv-mdp-serveur" autocomplete="off" aria-label="${h(t("The server's password"))}" placeholder="${h(t("The server's password"))}"></div>` : ""}
      </fieldset></div>` : `<p class="muet petit">${t("Athanor first reads the server's fingerprint, for you to confirm.")}</p>`}
    <div class="ligne"><button type="submit" class="btn btn-primaire">${empreinte ? t("Add the server") : t("Continue")}</button><button type="button" class="btn" data-srv-fermer>${t("Cancel")}</button></div>
  </form>`;
}
// --- Missions (M159): one line per mission (state, sentence, lanes as they are now, date), its detail at #missions/<id>
const ETATS_MISSION = { preparation: "Being prepared", proposee: "Proposed", acceptee: "Accepted", lancee: "Launched", terminee: "Finished", annulee: "Cancelled", echec: "Failed", en_pause: "Paused", arretee: "Stopped" };
const ETATS_VOIE_DEV = { travaille: ["Working", "pv-claude"], termine: ["Finished", "pv-ok"], arrete: ["Stopped", ""], hors_ligne: ["Offline: the mission waits", "pv-vous"], attente: ["Waiting to be sent", "pv-vous"] };
// M190: a Studio lane, on this PC (dev) or on a remote Athanor (dev_distant, ref "remote id/project", nom = nom@instance)
const voieStudio = (v) => v.module === "dev" || v.module === "dev_distant";
// a lane's state read now by the API (voiesEnDirect): Studio = the project's pilot, Servers = its task
function etatVoie(v) {
  if (v.etat === "refusee") return [t("Refused: native Windows mode"), "pv-vous"];
  if (!v.actuel || v.actuel === "inconnu") return [t("Not found"), "pv-vous"];
  if (voieStudio(v)) { const [l, c] = ETATS_VOIE_DEV[v.actuel] || [v.actuel, ""]; return [t(l), c]; }
  return ETATS_TACHE_SRV()[v.actuel] || [v.actuel, ""];
}
// lien: the project (selected, then its Overview) or the task's server; never inside a line that is a link itself
function voieMission(v, lien) {
  const [libelle, classe] = etatVoie(v), dev = voieStudio(v), distant = v.module === "dev_distant";
  const mod = distant ? t("Remote Studio") : t((etat.hubModules || []).find((m) => m.id === v.module)?.nom || v.module);
  const nom = distant ? v.nom || v.ref : dev ? v.ref : ((etat.mentionsSugg || []).find((x) => x.type === "serveur" && x.ref === v.serveur) || {}).nom || v.serveur || v.ref;
  const cible = !lien || v.actuel === "inconnu" ? "" : distant ? `href="#global"` : dev ? `href="#suivi" data-projet="${h(v.ref)}" data-cible="suivi"` : v.serveur ? `href="#serveur/${encodeURIComponent(v.serveur)}"` : "";
  return `<span class="mis-voie-etat"><span class="pastille-module"><span class="ico ico-${dev ? "dev" : "serveurs"}" aria-hidden="true"></span>${h(mod)}</span>${cible ? `<a ${cible}>${h(nom)}</a>` : `<b>${h(nom)}</b>`}<span class="pv-etat ${classe}">${h(libelle)}</span>${v.detail ? `<span class="muet petit mis-voie-detail">${h(v.detail)}</span>` : ""}</span>`;
}
// M172: the relay's step under way of a launched or paused mission ("Relay: Verification"), "" otherwise
function etapeRelais(m) {
  const rang = m.relais && ["lancee", "en_pause"].includes(m.etat) ? RANG_RELAIS[m.relais.etape] ?? 0 : 5;
  return rang < 5 ? t("Relay: {0}", [t(RELAIS_MISSION[rang])]) : "";
}
function ligneMission(m) {
  const titre = (m.carte || {}).titre, phrase = m.phrase.length > 140 ? m.phrase.slice(0, 140) + "…" : m.phrase;
  return `<li><a class="bloc mis-ligne" href="#missions/${encodeURIComponent(m.id)}"><span class="badge mis-etat-${h(m.etat)}">${h(t(ETATS_MISSION[m.etat] || m.etat))}</span>
    <span class="mis-phrase">${titre ? `<b>${h(titre)}</b> ` : ""}<span class="muet">${h(phrase)}</span></span>
    <span class="muet petit">${h(new Date(m.cree).toLocaleDateString(document.documentElement.lang || "en"))}</span>
    ${etapeRelais(m) ? `<span class="petit mis-relais-etape">${h(etapeRelais(m))}</span>` : ""}
    ${(m.voies || []).length ? `<span class="mis-voies-ligne">${m.voies.map((v) => voieMission(v, false)).join("")}</span>` : ""}</a></li>`;
}
// Home's Missions block: the 3 newest, See all; the empty state says where to start
function blocMissions(l) {
  return `<section class="bloc missions" aria-labelledby="missions-titre">
    <div class="ligne m0 tuile-tete"><h2 class="m0" id="missions-titre">${t("Missions")}</h2><span class="espace"></span>${l.length ? `<a class="btn btn-petit" href="#missions">${t("See all")} (${l.length}) →</a>` : ""}</div>
    ${l.length ? `<ul class="mis-liste">${l.slice(0, 3).map(ligneMission).join("")}</ul>`
      : `<div class="missions-vide"><span class="missions-icone" aria-hidden="true"></span><div><b>${t("No mission yet")}</b><div class="muet petit">${t("One sentence in the bar above, and Studio and Servers work together.")}</div></div></div>`}
  </section>`;
}
pages.missions = async () => {
  const [d, sugg] = await Promise.all([api("missions"), api("mentions").catch(() => ({}))]);
  etat.mentionsSugg = sugg.mentions || [];
  return `<div class="missions">
    <div class="ligne cof-tete"><h2 class="m0">${t("Missions")} <span class="muet">${d.missions.length}</span></h2>${d.missions.length ? `<span class="espace"></span><a class="btn btn-primaire" href="#accueil@accueil-texte" data-nouvelle-mission>${t("New mission")}</a>` : ""}</div>
    ${d.missions.length ? `<ul class="mis-liste">${d.missions.map(ligneMission).join("")}</ul>`
      : `<div class="vide srv-vide"><p><b>${t("No mission yet")}</b></p>
      <p class="muet">${t("Say what you want in the bar of the home page, for example “Put @my-shop online at shop.example.com”. The orchestrator prepares a mission card first: what will be built and deployed, what will be touched, what it will ask your approval for. Nothing starts before you launch it.")}</p>
      <a class="btn btn-primaire" href="#accueil">${t("Go to the home page")}</a></div>`}
  </div>`;
};
// One mission (#missions/<id>): its card (still to launch: the home's, with its buttons) and its lanes with their links
pages.mission = async () => {
  const [d, sugg] = await Promise.all([api("missions/" + encodeURIComponent(etat.missionId || "")), api("mentions").catch(() => ({}))]);
  etat.mentionsSugg = sugg.mentions || []; etat.missionVue = d;
  const m = d.mission, aLancer = ACTIVE_MISSION.includes(m.etat), fin = (m.historique || []).slice(-1)[0] || {};
  clearTimeout(minuterieMission);
  if (aLancer && travailleMission(d)) minuterieMission = setTimeout(lireMissionVue, 2000);
  const retour = `<div class="ligne m0"><a class="btn btn-petit" href="#missions">← ${t("Missions")}</a></div>`;
  // still being prepared or to launch: the home's card, its question and its buttons (the lanes come at the launch)
  if (aLancer) return `<div class="missions mis-detail">${retour}<div id="mission-zone" aria-live="polite">${zoneMission()}</div></div>`;
  if (etat.missionJournalDe !== m.id) { etat.missionJournalDe = m.id; etat.missionJournalN = 50; }
  suivreMissionDirect();
  return `<div class="missions mis-detail">${retour}
    <div><div class="ligne m0 tuile-tete"><h2 class="m0" id="mis-titre">${h((m.carte || {}).titre || t("New mission"))}</h2><span class="badge mis-etat-${h(m.etat)}" id="mis-badge">${h(t(ETATS_MISSION[m.etat] || m.etat))}</span></div>
      <p class="muet petit mis-phrase">${h(m.phrase)}</p>${m.etat === "echec" && fin.detail ? `<p class="mis-avert" role="note">${h(fin.message ? texteMessage(fin.message) : fin.detail)}</p>` : ""}
      ${["lancee", "en_pause"].includes(m.etat) ? controlesMission(m) : ""}</div>
    <div id="mission-direct" class="mis-detail">${missionDirect(m)}</div>
    ${m.carte ? `<section class="bloc mis-carte" aria-labelledby="mis-titre">${carteMission(d)}</section>` : ""}
  </div>`;
};
// M171: a launched mission, live (DESIGN §Mission en direct): its lanes side by side, the relay's steps and URLs, the
// journal in plain words; read again every 2 s while the page is shown, the tab visible and the mission followed
const RELAIS_MISSION = ["Milestone delivered", "Dev deployment", "Verification", "Your accord", "Production"];
const RANG_RELAIS = { construction: 0, dev_deploye: 2, dev_verifie: 3, accord_prod: 3, prod_deploye: 4, termine: 5 };
const ETATS_ETAPE = { fait: "done", cours: "in progress", attente: "in progress", echec: "failed", "": "to come" };
// the relay step of the newest journal entry when it is an error (a deployment, a check, the prod): red until it moves on
function etapeEchec(j) {
  const e = j[j.length - 1], env = ((e || {}).params || [])[0];
  if (!e || e.niveau !== "erreur") return -1;
  if (/^(incident|prod):/.test(e.id)) return 4;
  if (/^(deploi|verif):.*:echec$/.test(e.id)) return env === "prod" ? 4 : env === "dev" ? (e.id.startsWith("deploi") ? 1 : 2) : -1;
  return -1;
}
function friseRelais(m) {
  const envs = (m.carte || {}).envs || [], dev = envs.find((x) => x.nom === "dev"), prod = envs.find((x) => x.nom === "prod");
  const rang = RANG_RELAIS[m.relais.etape] ?? 0, echec = etapeEchec(m.journal || []);
  const etapes = (prod ? RELAIS_MISSION : RELAIS_MISSION.slice(0, 3)).map((l, i) => {
    const c = echec >= 0 ? (i < echec ? "fait" : i === echec ? "echec" : "") : i < rang ? "fait" : i === rang ? (m.etat === "lancee" ? "cours" : "attente") : "";
    return `<li class="r-${c || "avenir"}"${c === "cours" ? ' aria-current="step"' : ""}><span class="mis-pastille" aria-hidden="true"></span><span>${t(l)}</span><span class="cache-visuel"> (${t(ETATS_ETAPE[c])})</span></li>`;
  }).join("");
  const url = (e, l) => e && e.url ? `<span>${t(l)} <a href="${h(e.url)}" target="_blank" rel="noopener noreferrer" class="mis-url">${h(e.url)}</a></span>` : "";
  const urls = [rang >= 2 ? url(dev, "Dev:") : "", prod && rang >= 4 ? url(prod, "Prod:") : ""].filter(Boolean);
  return `<ol class="mis-relais">${etapes}</ol>${urls.length ? `<p class="ligne m0 petit mis-relais-url">${urls.join("")}</p>` : ""}`;
}
// a journal parameter said in the page's language: who acted, a state, a policy, an action; names and URLs as they are
// shortcut: a parameter equal to one of these words is translated wherever it stands (an environment named "auto"), fine for the names a card uses
const MOTS_JOURNAL = { fenetre: "from this computer", "emergency stop": "emergency stop", refusee: "refused", expiree: "expired", echec: "failed",
  arretee: "stopped", accord: "after your accord", auto: "automatically", deploiement: "deployment", mission: "launch", plan_serveurs: "Servers plan" };
function motJournal(cle, x) {
  if (cle.startsWith("Policy changed")) return t((POLITIQUES_MISSION.find((p) => p[0] === x) || [, x])[1]);
  if (x.startsWith("appareil:")) return t("from a paired phone");
  if (MOTS_JOURNAL[x]) return t(MOTS_JOURNAL[x]);
  return PREFIXES.some((p) => x.startsWith(p)) ? t(x) : x;
}
const texteJournal = (e) => t(e.cle, (e.params || []).map((x) => motJournal(e.cle, x)));
function ligneJournal(e) {
  const d = new Date(e.t), lang = document.documentElement.lang || "en";
  const quand = d.toDateString() === new Date().toDateString() ? d.toLocaleTimeString(lang, { hour: "2-digit", minute: "2-digit" }) : d.toLocaleString(lang, { day: "numeric", month: "short", hour: "2-digit", minute: "2-digit" });
  return `<li class="mis-j-${h(e.niveau)}"><time datetime="${h(e.t)}">${h(quand)}</time><span>${h(texteJournal(e))}</span></li>`;
}
// a lane, live: the module's line (state, link) then Studio's milestone n/N and what the project lead does
function voieDirect(v) {
  const n = v.fait || 0, avance = !voieStudio(v) || !v.total ? "" : v.titre ? t("Milestone {0}/{1}: {2}", [Math.min(n + 1, v.total), v.total, v.titre]) : t("{0} / {1} milestones done", [n, v.total]);
  const decider = v.actuel === "attente_accord" ? `<a class="btn btn-petit" href="#pourvous">${t("See the plan")}</a>` : "";
  return `<li class="mis-voie">${voieMission(v, true)}${avance ? `<p class="m0">${h(avance)}</p>` : ""}${v.activite ? `<p class="m0 muet petit">${h(t(v.activite))}</p>` : ""}${decider}</li>`;
}
function missionDirect(m) {
  const lancee = (m.historique || []).find((x) => x.etat === "lancee"), j = [...(m.journal || [])].reverse(), n = etat.missionJournalN || 50;
  const pol = POLITIQUES_MISSION.find((p) => p[0] === m.politique), r = m.relais || {};
  const nonDonne = m.etat === "lancee" && r.etape === "accord_prod" && (m.journal || []).some((e) => e.id === `accord:${r.accord}:refusee` || e.id === `accord:${r.accord}:expiree`);
  return `${incidentMission(m)}<section class="bloc" aria-labelledby="mis-voies-titre"><div class="ligne m0 tuile-tete"><h2 class="m0" id="mis-voies-titre">${t("Lanes")}</h2><span class="espace"></span>${pol ? `<span class="muet petit">${h(t("Policy: {0}", [t(pol[1])]))}</span>` : ""}</div>
      ${(m.voies || []).length ? `<ul class="mis-voies-liste mis-voies-direct">${m.voies.map(voieDirect).join("")}</ul>${lancee ? `<p class="muet petit m0">${t("Launched on {0}", [h(new Date(lancee.t).toLocaleString(document.documentElement.lang || "en"))])}</p>` : ""}`
        : `<p class="muet m0">${t("The lanes start when the mission is launched.")}</p>`}
    </section>
    ${m.relais ? `<section class="bloc" aria-labelledby="mis-relais-titre"><h2 id="mis-relais-titre">${t("Relay")}</h2>${friseRelais(m)}
      ${nonDonne ? `<p class="ligne m0 mis-redemander"><span>${t("The production accord was not given.")}</span><button type="button" class="btn btn-petit btn-primaire" data-mis-ctl="redemander">${t("Ask again")}</button></p>` : ""}</section>` : ""}
    <section class="bloc" aria-labelledby="mis-journal-titre"><h2 id="mis-journal-titre">${t("Journal")} <span class="muet">${j.length}</span></h2>
      ${j.length ? `<ol class="mis-journal">${j.slice(0, n).map(ligneJournal).join("")}</ol>${j.length > n ? `<button type="button" class="btn btn-petit" id="mis-plus" data-mis-plus>${t("See more")} (${j.length - n})</button>` : ""}`
        : `<p class="muet m0">${t("No event yet: the journal fills in as the lanes move on.")}</p>`}
    </section>`;
}
// M172: the header's controls of a launched or paused mission: its policy (looser = the owner's password), Pause or
// Resume (the owner's password), Stop (confirmed: nothing is undone)
const RIGUEUR_POLITIQUE = ["autonome", "prod", "tout"];
function controlesMission(m) {
  return `<div class="ligne m0 mis-controles">
    <fieldset class="mis-pol-direct"><legend>${t("Policy")}</legend>${POLITIQUES_MISSION.map(([v, l, aide]) => `<label class="radio" title="${h(t(aide))}"><input type="radio" name="mis-pol-direct" value="${v}"${m.politique === v ? " checked" : ""}> ${t(l)}</label>`).join("")}</fieldset>
    <span class="espace"></span>
    ${m.etat === "en_pause" ? `<button type="button" class="btn btn-primaire" data-mis-ctl="reprendre">${t("Resume")}</button>` : `<button type="button" class="btn" data-mis-ctl="pause">${t("Pause")}</button>`}
    <button type="button" class="btn btn-danger" data-mis-ctl="arreter">${t("Stop")}</button></div>`;
}
// P6: the incident of the mission's project (the bell's, from the health of its environments), its actions as the bell's
function incidentMission(m) {
  const projet = ((m.voies || []).find((v) => v.module === "dev") || {}).ref;
  const a = projet && [...alertesHub(), ...((etat.hub || {}).ignorees || [])].find((x) => (x.element || "").startsWith(projet + "/") && (x.actions || []).some((y) => (y.route || "").endsWith("/incident")));
  if (!a) return "";
  const distant = (etat.vue || {}).distant;
  return `<section class="bloc mis-incident" aria-labelledby="mis-inc-titre"><h2 id="mis-inc-titre" class="m0">✖ ${t("Incident")}</h2>
    <p class="m0">${h(t(a.raison, (a.args || []).map(texteMessage)))}</p>
    <div class="ligne m0">${a.actions.map((x, i) => `<button type="button" class="btn btn-petit${i === 0 ? " btn-primaire" : ""}" data-mis-inc="${i}" data-empreinte="${h(a.empreinte)}"${distant && !x.distance ? ` disabled title="${h(t("On the computer only"))}"` : ""}>${h(t(x.libelle))}</button>`).join("")}</div></section>`;
}
document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-mis-ctl]"), m = (etat.missionVue || {}).mission; if (!b || !m) return;
  const a = b.dataset.misCtl;
  if (a === "arreter" && await choisir(t("Stop the mission? Nothing will be undone: the environments and the accesses stay."), [{ id: "non", texte: t("Keep it going") }, { id: "oui", texte: t("Stop the mission"), danger: true }]) !== "oui") return;
  const p = a === "reprendre" ? await preuveMdp(t("Resuming the mission: retype Athanor's password."), { ok: t("Resume") }) : {};
  if (!p) return;
  b.disabled = true;
  if (!(await actionMission(a, p))) b.disabled = false;
});
document.addEventListener("change", async (ev) => {
  const m = (etat.missionVue || {}).mission; if (ev.target.name !== "mis-pol-direct" || !m) return;
  const v = ev.target.value, souple = RIGUEUR_POLITIQUE.indexOf(v) < RIGUEUR_POLITIQUE.indexOf(m.politique);
  const p = souple ? await preuveMdp(t("A looser policy: retype Athanor's password."), { ok: t("Change the policy") }) : {};
  if (!p || !(await actionMission("politique", { politique: v, ...p }))) rendrePage(); // the radio goes back to the policy held
});
document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-mis-inc]"); if (!b) return;
  const a = [...alertesHub(), ...((etat.hub || {}).ignorees || [])].find((x) => x.empreinte === b.dataset.empreinte), x = a && (a.actions || [])[+b.dataset.misInc];
  if (!x) return;
  b.disabled = true;
  const r = await agirAlerte(x, b);
  if (r && r.page) return allerProjet(r.projet, r.page.slice(1)); // I take over: the server's sheet
  etat.hub = await api("hub").catch(() => etat.hub);
  if (etat.vue) rendreBandeau(etat.vue);
  if (r && etat.page === "mission") await rendrePage();
});
let minuterieDirect;
function suivreMissionDirect() {
  clearTimeout(minuterieDirect);
  const m = (etat.missionVue || {}).mission;
  if (!m || !["lancee", "en_pause"].includes(m.etat)) return;
  minuterieDirect = setTimeout(async () => {
    const ici = () => etat.page === "mission" && $("#mission-direct") && (etat.missionVue || {}).mission?.id === m.id;
    if (!ici()) return;
    if (document.visibilityState === "visible") {
      try {
        const d = await api("missions/" + encodeURIComponent(m.id));
        if (!ici()) return;
        etat.missionVue = d;
        if (d.mission.etat !== m.etat) { await rendrePage(); return; } // a new state: header and card again
        const z = $("#mission-direct"); garderFocus(z, () => { z.innerHTML = missionDirect(d.mission); });
      } catch { /* next try in 2 s */ }
    }
    suivreMissionDirect();
  }, 2000);
}
document.addEventListener("click", (ev) => {
  if (!ev.target.closest("[data-mis-plus]") || !etat.missionVue) return;
  etat.missionJournalN = (etat.missionJournalN || 50) + 50;
  const z = $("#mission-direct"); if (z) garderFocus(z, () => { z.innerHTML = missionDirect(etat.missionVue.mission); });
});

pages.serveurs = async () => {
  const distant = !!(etat.vue || {}).distant;
  const [d, c] = await Promise.all([api("serveurs"), distant ? null : api("coffre").catch(() => null)]);
  // shortcut: one request per server for its sheet; fine up to the tens of servers aimed at (E12: 10)
  const fiches = await Promise.all(d.serveurs.map((x) => api("serveurs/" + encodeURIComponent(x.id)).then((r) => r.fiche).catch(() => null)));
  const liste = d.serveurs.map((x, i) => carteServeur(x, fiches[i], (d.environnements || {})[x.id], d.sante)).join("");
  return `<div class="serveurs">
    ${srv.aj ? messagesCoffreServeurs(c) : ""}
    <div class="ligne cof-tete"><h2 class="m0">${t("Servers")} <span class="muet">${d.serveurs.length}</span></h2><span class="espace"></span>${distant ? `<span class="muet petit">${t("Adding a server is done on the computer.")}</span>` : srv.aj ? "" : `<button type="button" class="btn btn-primaire" data-srv-ajouter>+ ${t("Add a server")}</button>`}</div>
    ${srv.aj ? assistantServeur(c) : ""}
    ${d.serveurs.length && !srv.aj ? reglageSante(d.sante || {}, distant) : ""}
    ${liste ? `<div class="grille-projets">${liste}</div>` : srv.aj ? "" : `<div class="vide srv-vide"><p><b>${t("No server yet")}</b></p>
      <p class="muet">${t("Add your VPS or your home server: Athanor checks its fingerprint, connects with a key kept in the vault, then reads what runs on it.")}</p>
      ${distant ? "" : `<button type="button" class="btn btn-primaire" data-srv-ajouter>+ ${t("Add a server")}</button>`}</div>`}
  </div>`;
};
// the wizard's fields as typed, kept before each render (the address step, then the chosen way)
function lireAssistantServeur() {
  const a = srv.aj, val = (id) => { const e = document.getElementById(id); return e ? e.value : undefined; };
  if (!a || !$("#srv-form")) return a;
  Object.assign(a, { hote: val("srv-hote").trim(), port: +val("srv-port") || 22, utilisateur: val("srv-utilisateur").trim(), nom: val("srv-nom").trim(),
    roles: [...document.querySelectorAll(".srv-role:checked")].map((e) => e.value) });
  const m = document.querySelector('[name="srv-mode"]:checked'), cle = val("srv-cle-coffre"), conf = $("#srv-confirme");
  if (m) a.mode = m.value;
  if (cle !== undefined) a.cle = cle;
  if (conf) a.confirme = conf.checked;
  return a;
}
// a server reached: its sheet's error told, then the next step (by password: propose to cut password logins)
function serveurAjoute(r, mode) {
  if (r.fiche_erreur) toast(t(r.fiche_erreur), "erreur");
  toast(t("{0} added", [r.serveur.nom]), "ok");
  srv.aj = mode === "mot_de_passe" && !r.serveur.mot_de_passe_coupe ? { etape: "coupure", serveur: r.serveur } : null;
  return rendrePage();
}
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "serveurs") return;
  const b = ev.target.closest("[data-srv-ajouter], [data-srv-fermer], [data-srv-verifier], [data-srv-couper], [data-srv-copier], [name=srv-mode]");
  if (!b) return;
  const d = b.dataset;
  if (b.name === "srv-mode") { lireAssistantServeur(); return rendrePage().then(() => { const c = $("#srv-mdp-serveur") || $("#srv-cle-coffre"); if (c) c.focus(); }); }
  if (d.srvAjouter !== undefined) { srv.aj = { etape: "adresse", roles: [] }; return rendrePage().then(() => $("#srv-hote").focus()); }
  if (d.srvFermer !== undefined) { srv.aj = null; return rendrePage(); }
  if (d.srvCopier) { const c = document.getElementById(d.srvCopier); return navigator.clipboard.writeText(c.value).then(() => toast(t("Copied."), "ok"), () => c.select()); }
  b.disabled = true;
  if (d.srvVerifier) { const r = await action(api("serveurs/ajouter", "POST", { id: d.srvVerifier })); b.disabled = false; return r ? serveurAjoute(r, "generee") : null; }
  const r = await action(api("serveurs/" + encodeURIComponent(d.srvCouper), "POST", { couper_mot_de_passe: true }));
  b.disabled = false;
  if (r) { toast(t("Password logins are cut on {0}.", [srv.aj.serveur.nom]), "ok"); srv.aj = null; rendrePage(); }
});
$("#page").addEventListener("submit", async (ev) => {
  if (etat.page !== "serveurs") return;
  ev.preventDefault();
  if (ev.target.id === "srv-verrou") {
    const champ = $("#srv-deverrou");
    if (await action(api("deverrouiller", "POST", { mot_de_passe: champ.value }))) { champ.value = ""; rendrePage(); }
    return;
  }
  if (ev.target.id !== "srv-form") return;
  const a = lireAssistantServeur(), bouton = ev.submitter || ev.target.querySelector("[type=submit]");
  bouton.disabled = true;
  try {
    if (a.etape === "adresse") { // the fingerprint first, never accepted silently
      const r = await action(api("serveurs/empreinte", "POST", { hote: a.hote, port: a.port }));
      if (r) { Object.assign(a, { etape: "empreinte", empreinte: r.empreinte, confirme: false, mode: a.mode || "generee" }); await rendrePage(); $("#srv-confirme").focus(); }
      return;
    }
    const corps = { nom: a.nom, hote: a.hote, port: a.port, utilisateur: a.utilisateur, roles: a.roles, empreinte: a.empreinte, mode: a.mode };
    if (a.mode === "coffre") { corps.cle = a.cle; const m = $("#srv-mdp-athanor"); if (m) corps.mot_de_passe = m.value; }
    if (a.mode === "mot_de_passe") corps.mot_de_passe_serveur = $("#srv-mdp-serveur").value; // read here, never kept in srv.aj
    const r = await action(api("serveurs/ajouter", "POST", corps));
    if (!r) return;
    if (a.mode === "generee") { srv.aj = { etape: "cle", serveur: r.serveur, public: r.cle_publique, commande: r.commande }; return rendrePage(); }
    return serveurAjoute(r, a.mode);
  } finally { bouton.disabled = false; }
});
// --- Providers (#fournisseurs): the Cloudflare tokens (infra vault entries) the Servers agent uses for DNS and HTTPS (fournisseurs.go)
const fou = { aj: null }; // aj: the add form ({mode: "coller" | "coffre", nom, cle}), kept across renders
const DROITS_CF = { "dns:lire": "Read DNS records", "dns:écrire": "Change DNS records", ssl: "HTTPS settings", access: "Cloudflare Access (sign-in in front of a site)", certificats: "Origin certificates" };
const ETATS_FOU = { ok: "Works", refuse: "Refused", expire: "Expired", desactive: "Disabled", sans_zone: "No zone", injoignable: "Unreachable" };
function carteFournisseur(f, envs, distant) {
  const droits = (l) => l.length ? l.map((d) => `<span class="badge">${h(t(DROITS_CF[d] || d))}</span>`).join("") : `<span class="muet petit">${t("No right seen")}</span>`;
  return `<div class="bloc carte-serveur fou-carte" data-fou-id="${h(f.id)}">
    <div class="cp-tete"><b class="cp-nom">${h(f.nom)}</b><span class="pastille ${f.etat === "ok" ? "travaille" : "attend"}">${t(ETATS_FOU[f.etat] || "Not tested yet")}</span></div>
    <div class="muet petit">Cloudflare · ${f.teste ? h(t("Tested {0}", [dateLisible(f.teste)])) : t("Press Test to see its zones and rights.")}</div>
    ${f.bilan ? `<p class="petit m0 fou-bilan">${h(t(f.bilan.cle, f.bilan.args))}</p>` : ""}
    ${(f.zones || []).length ? `<ul class="fou-zones">${f.zones.map((z) => `<li><b>${h(z.nom)}</b><span class="ligne m0">${droits(z.droits || [])}</span></li>`).join("")}</ul>` : ""}
    <div class="ligne m0 srv-utilise"><span class="muet petit">${t("Used by")}</span>${envs.length ? envs.map((pe) => `<a class="badge" href="#acces" data-projet="${h(pe.slice(0, pe.lastIndexOf("/")))}" data-cible="acces">${h(pe)}</a>`).join("") : `<span class="muet petit">${t("No environment uses it yet.")}</span>`}</div>
    ${distant ? "" : `<div class="ligne m0"><button type="button" class="btn btn-petit" data-fou-tester="${h(f.id)}">${t("Test")}</button><button type="button" class="btn btn-petit btn-danger" data-fou-retirer="${h(f.id)}">${t("Remove")}</button></div>`}
  </div>`;
}
function formulaireFournisseur(c) {
  const a = fou.aj, cles = ((c || {}).entrees || []).filter((e) => e.famille === "secret" && e.type === "cloudflare");
  return `<form class="bloc cof-form srv-form" id="fou-form" autocomplete="off">
    <h2>${t("Add a provider")}</h2>
    <label class="champ"><span>${t("Name (optional)")}</span><input type="text" id="fou-nom" maxlength="100" placeholder="${h(t("e.g. Cloudflare – my domains"))}" value="${h(a.nom || "")}"></label>
    <fieldset class="srv-facons"><legend>${t("Its Cloudflare token")}</legend>
      <label class="radio srv-facon"><input type="radio" name="fou-mode" value="coller"${a.mode === "coller" ? " checked" : ""}> <span><b>${t("Paste a new token")}</b><br><span class="muet petit">${t("It goes straight to the vault, in the infra scope: no agent ever sees it.")}</span></span></label>
      ${a.mode === "coller" ? `<div class="srv-sous"><input type="password" id="fou-jeton" autocomplete="off" spellcheck="false" aria-label="${h(t("Cloudflare token"))}" placeholder="${h(t("Cloudflare token"))}" required></div>` : ""}
      <label class="radio srv-facon"><input type="radio" name="fou-mode" value="coffre"${a.mode === "coffre" ? " checked" : ""}${cles.length ? "" : " disabled"}> <span><b>${t("A Cloudflare entry of the vault")}</b><br><span class="muet petit">${cles.length ? t("Entries in the code scope must move to infra first, in the Vault.") : t("No Cloudflare entry in the vault yet.")}</span></span></label>
      ${a.mode === "coffre" && cles.length ? `<div class="srv-sous"><select id="fou-cle" aria-label="${h(t("A Cloudflare entry of the vault"))}">${cles.map((e) => `<option value="${h(e.id)}"${e.id === a.cle ? " selected" : ""}${e.portee === "infra" ? "" : " disabled"}>${h(e.nom)}${e.portee === "infra" ? "" : " (code)"}</option>`).join("")}</select></div>` : ""}
    </fieldset>
    <p class="muet petit">${lienGuide("fournisseurs", t("Create a token with the minimum rights"))}</p>
    <div class="ligne"><button type="submit" class="btn btn-primaire">${t("Add")}</button><button type="button" class="btn" data-fou-fermer>${t("Cancel")}</button></div>
  </form>`;
}
pages.fournisseurs = async () => {
  const distant = !!(etat.vue || {}).distant;
  const [d, c] = await Promise.all([api("fournisseurs"), distant ? null : api("coffre").catch(() => null)]);
  const envs = d.environnements || {};
  return `<div class="serveurs fournisseurs">
    ${fou.aj && !distant ? messagesCoffreServeurs(c) : ""}
    <div class="ligne cof-tete"><h2 class="m0">${t("Providers")} <span class="muet">${d.fournisseurs.length}</span></h2><span class="espace"></span>${distant ? `<span class="muet petit">${t("Providers are added on the computer.")}</span>` : fou.aj ? "" : `<button type="button" class="btn btn-primaire" data-fou-ajouter>+ ${t("Add a provider")}</button>`}</div>
    <p class="muet petit">${t("A provider lets the Servers agent manage your domains' DNS records and HTTPS for your environments. Its token stays in the vault: agents get tools that use it, never the token.")} ${lienGuide("fournisseurs", t("Create a token with the minimum rights"))}</p>
    ${fou.aj && !distant ? formulaireFournisseur(c) : ""}
    ${d.fournisseurs.length ? `<div class="grille-projets">${d.fournisseurs.map((f) => carteFournisseur(f, envs[f.id] || [], distant)).join("")}</div>`
      : fou.aj ? "" : `<div class="vide srv-vide"><p><b>${t("No provider yet")}</b></p><p class="muet">${t("Add a Cloudflare token to let Athanor point your domains at your servers and set up HTTPS.")}</p></div>`}
  </div>`;
};
function lireFormFournisseur() {
  const a = fou.aj, m = document.querySelector('[name="fou-mode"]:checked'), cle = $("#fou-cle");
  if (!a || !$("#fou-form")) return a;
  a.nom = $("#fou-nom").value.trim();
  if (m) a.mode = m.value;
  if (cle) a.cle = cle.value;
  return a;
}
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "fournisseurs") return;
  const b = ev.target.closest("[data-fou-ajouter], [data-fou-fermer], [data-fou-tester], [data-fou-retirer], [name=fou-mode]");
  if (!b) return;
  const d = b.dataset;
  if (b.name === "fou-mode") { lireFormFournisseur(); return rendrePage().then(() => { const c = $("#fou-jeton") || $("#fou-cle"); if (c) c.focus(); }); }
  if (d.fouAjouter !== undefined) { fou.aj = { mode: "coller" }; return rendrePage().then(() => $("#fou-nom").focus()); }
  if (d.fouFermer !== undefined) { fou.aj = null; return rendrePage(); }
  if (d.fouRetirer) {
    const nom = b.closest(".fou-carte").querySelector(".cp-nom").textContent;
    if (await choisir(t("Remove the provider {0}? Its token stays in the vault.", [nom]), [{ id: "non", texte: t("Cancel") }, { id: "oui", texte: t("Remove"), danger: true }]) !== "oui") return;
    if (await action(api("fournisseurs/" + encodeURIComponent(d.fouRetirer), "DELETE"))) { toast(t("{0} removed.", [nom]), "ok"); rendrePage(); }
    return;
  }
  b.disabled = true; b.textContent = t("Testing…");
  const r = await action(api("fournisseurs/" + encodeURIComponent(d.fouTester) + "/tester", "POST"));
  if (r) toast(t(r.bilan.cle, r.bilan.args), r.etat === "ok" ? "ok" : "erreur");
  rendrePage();
});
$("#page").addEventListener("submit", async (ev) => {
  if (etat.page !== "fournisseurs") return;
  ev.preventDefault();
  if (ev.target.id === "srv-verrou") {
    const champ = $("#srv-deverrou");
    if (await action(api("deverrouiller", "POST", { mot_de_passe: champ.value }))) { champ.value = ""; rendrePage(); }
    return;
  }
  if (ev.target.id !== "fou-form") return;
  const a = lireFormFournisseur(), bouton = ev.submitter || ev.target.querySelector("[type=submit]");
  bouton.disabled = true;
  try {
    let cle = a.cle;
    if (a.mode === "coller") { // the token goes to the vault first (infra), read here and never kept in fou.aj
      const e = await action(api("coffre", "POST", { nom: a.nom || "Cloudflare", famille: "secret", type: "cloudflare", portee: "infra", valeur: $("#fou-jeton").value.trim() }));
      if (!e) return;
      cle = e.id; a.mode = "coffre"; a.cle = cle; // a failure below keeps the entry, chosen from the vault next time
    }
    const f = await action(api("fournisseurs", "POST", { nom: a.nom, cle }));
    if (!f) return rendrePage();
    fou.aj = null; toast(t("{0} added", [f.nom]), "ok"); return rendrePage();
  } finally { bouton.disabled = false; }
});
// --- One server (#serveur/<id>): its sheet as read on it, its roles, Refresh, cut password logins, Remove (read only from a paired device)
const goLisible = (mo) => t("{0} GB", [(mo / 1024).toFixed(mo >= 10240 ? 0 : 1)]);
function ficheServeur(f) {
  const s = f.systeme || {}, r = f.ressources || {}, j = Math.floor((s.allume_s || 0) / 86400);
  const lignes = (l) => l.filter(([, v]) => v !== "" && v != null).map(([k, v]) => `<div class="srv-kv"><span class="muet">${t(k)}</span><span>${v}</span></div>`).join("");
  const bloc = (titre, corps, n) => `<div class="bloc srv-section"><h3>${t(titre)}${n == null ? "" : ` <span class="muet">${n}</span>`}</h3>${corps}</div>`;
  const vide = (x) => `<p class="muet petit">${t(x)}</p>`;
  const table = (tetes, rangs) => `<div class="srv-table"><table><thead><tr>${tetes.map((x) => `<th>${t(x)}</th>`).join("")}</tr></thead><tbody>${rangs.map((r) => `<tr>${r.map((c) => `<td>${c}</td>`).join("")}</tr>`).join("")}</tbody></table></div>`;
  const ports = f.ports || [], logiciels = f.logiciels || [], services = f.services || [], conteneurs = f.conteneurs || [];
  const presents = logiciels.filter((l) => l.present), absents = logiciels.filter((l) => !l.present);
  const pareFeu = { actif: t("on"), inactif: t("off"), absent: t("not installed") }[f.pare_feu] || t("unknown");
  return `<div class="srv-sections">
    ${bloc("System", lignes([["Name", h(s.nom || "?")], ["Host name", s.nom_hote && `<code>${h(s.nom_hote)}</code>`], ["Kernel", s.noyau && h(s.noyau + (s.architecture ? " · " + s.architecture : ""))],
      ["Up for", s.allume_s ? h(j >= 2 ? t("{0} days", [j]) : t("{0} h", [Math.floor(s.allume_s / 3600)])) : ""], ["Sudo without a password", f.sudo ? t("yes") : t("no")], ["Firewall", h(pareFeu)]]))}
    ${bloc("Resources", lignes([["Processors", r.processeurs || ""], ["Memory", r.memoire_mo ? h(t("{0} free of {1}", [goLisible(r.memoire_libre_mo), goLisible(r.memoire_mo)])) : ""],
      ["Disk (/)", r.disque_mo ? h(t("{0} free of {1}", [goLisible(r.disque_libre_mo), goLisible(r.disque_mo)])) : ""]]))}
    ${bloc("Listening ports", ports.length ? table(["Port", "Protocol", "Address", "Program"], ports.map((p) => [`<b>${p.port}</b>`, h(p.proto), `<code>${h(p.adresse)}</code>`, h(p.processus || "—")])) : vide("None seen."), ports.length)}
    ${bloc("Software", (presents.length ? `<div class="ligne m0">${presents.map((l) => `<span class="badge">${h(l.nom)}${l.version ? ` <span class="muet">${h(l.version)}</span>` : ""}</span>`).join("")}</div>` : vide("None of the usual software."))
      + (absents.length ? `<p class="muet petit">${h(t("Not installed: {0}", [absents.map((l) => l.nom).join(", ")]))}</p>` : ""), presents.length)}
    ${bloc("Services", services.length ? `<div class="ligne m0">${services.map((x) => `<span class="badge">${h(x)}</span>`).join("")}</div>` : vide("None seen."), services.length)}
    ${bloc("Containers", conteneurs.length ? table(["Name", "Image", "State"], conteneurs.map((c) => [`<b>${h(c.nom)}</b>`, `<code>${h(c.image)}</code>`, h(c.etat)])) : vide("None seen."), conteneurs.length)}
    ${(f.remarques || []).length ? `<div class="bloc srv-section srv-remarques"><h3>${t("Not read")}</h3><ul>${f.remarques.map((x) => `<li class="petit">${h(t(x))}</li>`).join("")}</ul></div>` : ""}
  </div>`;
}
pages.serveur = async () => {
  const distant = !!(etat.vue || {}).distant;
  const [d, c, ta] = await Promise.all([api("serveurs/" + encodeURIComponent(srv.id || "")), distant ? null : api("coffre").catch(() => null),
    api("serveurs/" + encodeURIComponent(srv.id || "") + "/taches").catch(() => null)]);
  srv.tachesSig = JSON.stringify(ta);
  const x = d.serveur, f = d.fiche, attente = x.etat === "inconnu";
  srv.fiche = x; // the actions below act on the server shown
  const sante = { ok: t("Reachable"), injoignable: t("Unreachable"), empreinte_changee: t("Host fingerprint changed") }[x.etat] || t("Waiting for its key");
  const role = (r) => distant ? ((x.roles || []).includes(r) ? `<span class="badge${r === "prod" ? " badge-infra" : ""}">${r}</span>` : "")
    : `<label class="radio"><input type="checkbox" class="srv-fiche-role" value="${r}"${(x.roles || []).includes(r) ? " checked" : ""}> ${r === "dev" ? t("dev (tests, previews)") : t("prod (your live site)")}</label>`;
  return `<div class="serveurs srv-fiche">
    <p class="m0"><a href="#serveurs" class="petit">← ${t("Servers")}</a></p>
    ${distant ? "" : messagesCoffreServeurs(c)}
    <div class="bloc srv-tete">
      <div class="cp-tete"><h2 class="m0 cp-nom">${h(x.nom)}</h2><span class="pastille ${x.etat === "ok" ? "travaille" : "attend"}">${sante}</span></div>
      <div class="muet petit"><code>${h(x.utilisateur)}@${h(x.hote)}${x.port === 22 ? "" : ":" + x.port}</code>${x.empreinte_hote ? ` · ${t("Fingerprint")} <code class="srv-empreinte-courte">${h(x.empreinte_hote)}</code>` : ""}</div>
      <div class="ligne m0"><span class="muet petit">${t("Roles")}</span>${role("dev")}${role("prod")}${distant && !x.roles.length ? `<span class="muet petit">${t("No role yet")}</span>` : ""}</div>
      <div class="ligne m0 srv-utilise"><span class="muet petit">${t("Used by")}</span>${(d.environnements || []).length ? d.environnements.map((pe) => `<a class="badge" href="#acces" data-projet="${h(pe.slice(0, pe.lastIndexOf("/")))}" data-cible="acces">${h(pe)}</a>`).join("") : `<span class="muet petit">${t("No project environment uses this server yet.")}</span>`}</div>
      <div class="muet petit">${x.mot_de_passe_coupe ? "🔒 " + t("Password logins are cut: Athanor and you connect with keys only.") : t("Password logins are still open on this server.")}</div>
      <div class="muet petit">${f && f.date ? h(t("Sheet read {0}", [dateLisible(f.date)])) : ""}${x.vu ? (f && f.date ? " · " : "") + h(t("Seen {0}", [dateLisible(x.vu)])) : ""}</div>
      ${distant ? `<p class="muet petit m0">${t("Read only from this device: the server is changed on the computer.")}</p>` : `<div class="ligne m0">
        ${attente ? `<button type="button" class="btn btn-primaire" data-srv-verifier="${h(x.id)}">${t("Verify")}</button>` : `<button type="button" class="btn" data-srv-rafraichir>↻ ${t("Refresh")}</button>`}
        ${!attente && !x.mot_de_passe_coupe ? `<button type="button" class="btn" data-srv-couper="${h(x.id)}">${t("Cut password logins")}</button>` : ""}
        ${attente ? "" : `<button type="button" class="btn" data-srv-durcir="${h(x.id)}" title="${h(t("A fixed plan: a user with sudo instead of root, SSH keys only, no root login, a firewall that refuses all but SSH, 80 and 443, security updates, the clock synchronised, Caddy. Each step already in place is skipped; you accept the plan first."))}">🛡 ${t("Harden this server")}</button>`}</div>`}
    </div>
    ${blocTaches(ta, distant, (d.annulations || []).length ? `<div class="srv-annulations"><h4 class="m0">${t("Changes waiting for confirmation")}</h4>
      <p class="muet petit">${t("Athanor put a safety net on these firewall or SSH changes: unless you keep them, the server undoes them by itself when the countdown ends.")}</p>
      ${d.annulations.map((a) => `<div class="ligne m0"><code>${h(a.quoi)}</code><span class="muet petit" data-srv-echeance="${h(a.echeance)}"></span>${distant ? "" : `<button type="button" class="btn btn-primaire" data-srv-garder="${h(a.id)}">${t("Keep this change")}</button>`}</div>`).join("")}</div>` : "", d.environnements || [])}
    ${f ? ficheServeur(f) : `<div class="vide">${attente ? t("Its sheet is made once Athanor reaches the server with its key.") : t("Its sheet is not made yet.")}</div>`}
    ${d.runbook ? `<details class="bloc srv-runbook"><summary><b>${t("Runbook")}</b> <span class="muet petit">${t("What was done on this server, its backups and how to go back, kept by Athanor and its agents.")}</span></summary><pre class="chat-code srv-runbook-texte">${h(d.runbook)}</pre></details>` : ""}
    ${distant ? "" : `<div class="bloc srv-retirer"><h3>${t("Remove this server")}</h3>
      <p class="muet petit">${t("Athanor forgets it and its sheet; nothing is changed on the server. Its key stays in the vault unless you tick the box.")}</p>
      <label class="radio"><input type="checkbox" id="srv-retirer-cle"${x.cle ? "" : " disabled"}> ${t("Also remove its key from the vault")}</label>
      <div class="ligne m0"><button type="button" class="btn btn-danger" data-srv-retirer>${t("Remove")}</button></div></div>`}
  </div>`;
};
// --- A server's tasks (M99): asked here, run by the ops lead agent; its plans are decided on their approval cards
const ETATS_TACHE_SRV = () => ({ preparation: [t("Reading the server"), "pv-claude"], critique: [t("Security critic reading the plan"), "pv-claude"],
  attente_accord: [t("Waiting for your decision"), "pv-vous"], application: [t("Applying the plan"), "pv-claude"], verification: [t("External check"), "pv-claude"],
  finie: [t("Done"), "pv-ok"], echec: [t("Failed"), "pv-vous"], arretee: [t("Stopped"), "pv-vous"] });
const tacheFinie = (x) => ["finie", "echec", "arretee"].includes(x.etat);
function dureeTache(x) {
  const s = Math.max(0, Math.round(((x.fin ? Date.parse(x.fin) : Date.now()) - Date.parse(x.cree)) / 1000));
  return s < 60 ? t("{0} s", [s]) : s < 3600 ? t("{0} min", [Math.round(s / 60)]) : t("{0} h", [(s / 3600).toFixed(1)]);
}
function carteTacheServeur(x, plans, distant) {
  const [lib, cl] = ETATS_TACHE_SRV()[x.etat] || [x.etat, ""], heure = (d) => new Date(d).toLocaleTimeString(document.documentElement.lang || "en", { hour: "2-digit", minute: "2-digit" });
  const etapes = (x.historique || []).map((e) => `<li><span class="muet">${h(heure(e.t))}</span> ${h((ETATS_TACHE_SRV()[e.etat] || [e.etat])[0])}${["echec", "arretee"].includes(e.etat) && e.detail ? ` <span class="muet">— ${h(t(e.detail))}</span>` : ""}</li>`).join("");
  const blocsPlans = (x.plans || []).map((id, i) => {
    const p = plans[id], attend = x.etat === "attente_accord" && i === x.plans.length - 1;
    if (!p) return `<p class="muet petit">${h(t("Plan {0}: no longer kept.", [i + 1]))}</p>`;
    const faites = (p.etapes || []).filter((e) => e.etat === "ok").length;
    return `<div class="srv-tache-plan"><h4 class="m0">${h(t("Plan {0}", [i + 1]))} <span class="muet petit">${h(t("{0} of {1} step(s) applied", [faites, (p.etapes || []).length]))}</span></h4>${blocPlan({}, p)}
      ${attend ? `<p class="m0"><a href="#pourvous" class="btn btn-primaire" data-srv-tache-carte="${h(id)}">${t("Decide on its card")}</a></p>` : ""}</div>`;
  }).join("");
  const v = x.verification;
  return `<details class="srv-tache" data-tache="${h(x.id)}"${srv.ouvertes.has(x.id) ? " open" : ""}><summary><span class="pv-etat ${cl}">${h(lib)}</span> <span class="srv-tache-texte">${h(x.texte)}</span>
      <span class="muet petit">${h(dateLisible(x.cree))} · ${h(dureeTache(x))}</span></summary>
    <div class="srv-tache-corps"><p class="pv-details">${h(x.texte)}</p>
      ${x.forcee ? `<p class="petit pv-vous">${t("Forced in native Windows mode, without the barrier.")}</p>` : ""}
      <ol class="srv-tache-etapes petit">${etapes}</ol>
      ${blocsPlans || (tacheFinie(x) ? `<p class="muet petit">${t("No plan: nothing was changed on the server.")}</p>` : "")}
      ${v ? `<div class="pv-note"><strong>${t("External check")}</strong> <span class="pv-etat ${v.verdict === "ok" ? "pv-ok" : "pv-vous"}">${v.verdict === "ok" ? t("Passed") : t("Failed")}</span>
        ${(v.lignes || []).length ? `<ul class="petit m0">${v.lignes.map((l) => `<li>${h(l)}</li>`).join("")}</ul>` : ""}</div>` : ""}
      ${x.resultat ? `<div class="pv-note"><strong>${t("The agent's answer")}</strong><p class="m0 srv-tache-resultat">${h(x.resultat)}</p></div>` : ""}
      ${!tacheFinie(x) && !distant ? `<div class="ligne m0"><button type="button" class="btn btn-danger" data-srv-tache-arreter="${h(x.id)}">${t("Stop")}</button></div>` : ""}
    </div></details>`;
}
function blocTaches(ta, distant, annulations, envs) {
  const l = (ta || {}).taches || [];
  return `<div class="bloc srv-taches"><h3>${t("Tasks")}${l.length ? ` <span class="muet">${l.length}</span>` : ""}</h3>
    ${distant ? `<p class="muet petit">${t("Tasks are launched from the computer; their plans are decided on their cards, on the For you page.")}</p>` : `<form id="srv-tache-form" class="srv-tache-form">
      <label for="srv-tache-texte"><b>${t("What should be done on this server?")}</b></label>
      <textarea id="srv-tache-texte" rows="3" maxlength="4000" placeholder="${h(t("e.g. Install nginx and serve my site over HTTPS"))}">${h(srv.texte)}</textarea>
      <p class="muet petit m0">${t("The Servers agent reads the server, then proposes a plan. Nothing changes before you accept it on its card; a security critic reads it first, the paths a step names are archived before it runs, a file the agent rewrites keeps its previous copy, and an external check follows.")}</p>
      ${(ta || {}).natif ? `<p class="petit pv-vous m0">${t("Native Windows mode has no barrier: a task runs only if you force it, with Athanor's password.")}</p>` : ""}
      ${envs.length ? `<label class="petit" for="srv-tache-env">${t("For the environment")} <select id="srv-tache-env"><option value="">${t("None (this server only)")}</option>${envs.map((pe) => `<option value="${h(pe)}">${h(pe)}</option>`).join("")}</select></label>
      <p class="muet petit m0">${t("With an environment, the agent can also change the DNS, HTTPS and protection of its address, on that name only.")}</p>` : ""}
      <div class="ligne m0"><button type="submit" class="btn btn-primaire">${t("Launch")}</button></div></form>`}
    ${annulations}
    ${ta ? l.length ? l.map((x) => carteTacheServeur(x, ta.plans || {}, distant)).join("") : `<p class="muet petit">${t("No task yet on this server.")}</p>` : `<p class="muet petit">${t("The tasks cannot be read right now.")}</p>`}
  </div>`;
}
$("#page").addEventListener("toggle", (ev) => { // a task opened stays open across a live refresh
  const d = ev.target; if (etat.page !== "serveur" || !d.matches || !d.matches(".srv-tache")) return;
  if (d.open) srv.ouvertes.add(d.dataset.tache); else srv.ouvertes.delete(d.dataset.tache);
}, true);
$("#page").addEventListener("input", (ev) => { if (etat.page === "serveur" && ev.target.id === "srv-tache-texte") srv.texte = ev.target.value; });
$("#page").addEventListener("submit", async (ev) => {
  if (etat.page !== "serveur" || ev.target.id !== "srv-tache-form") return;
  ev.preventDefault();
  const champ = $("#srv-tache-texte"), texte = champ.value.trim(), b = ev.submitter || ev.target.querySelector("[type=submit]");
  if (!texte) return champ.focus();
  const url = "serveurs/" + encodeURIComponent(srv.fiche.id) + "/taches";
  b.disabled = true;
  const env = ($("#srv-tache-env") || {}).value || "";
  try {
    const r = await api(url, "POST", { texte, env }).catch(async (e) => {
      if (!e.data || !e.data.proposer_wsl) throw e;
      const c = await choisir(t("Native Windows mode has no barrier: an agent working on your servers would run with all your rights on this PC. Switch to WSL, or force this task knowing the risk."),
        [{ id: "wsl", texte: t("Switch to WSL"), primaire: true }, { id: "forcer", texte: t("Force this task"), danger: true }, { id: "", texte: t("Cancel") }], { titre: t("Native Windows mode") });
      if (c === "wsl") { location.hash = "#options@bloc-direct"; return null; }
      if (c !== "forcer") return null;
      const p = await preuveMdp(t("Forcing a Servers task without the barrier: retype Athanor's password."), { ok: t("Force this task"), danger: true }, t("Force this task without the barrier?"));
      return p ? api(url, "POST", { texte, env, forcer: true, ...p }) : null;
    }).catch((e) => { if (e.message !== t("Locked")) toast(e.message, "erreur"); return null; });
    if (!r) return;
    srv.texte = ""; srv.ouvertes.add(r.tache.id);
    toast(t("Task launched: the agent reads the server first."), "ok");
    rendrePage();
  } finally { b.disabled = false; }
});
// the countdown of the changes waiting for confirmation; at its end the server has undone the change: read it again
setInterval(() => {
  let fini = false;
  for (const e of document.querySelectorAll("[data-srv-echeance]")) {
    const s = Math.max(0, Math.round((Date.parse(e.dataset.srvEcheance) - Date.now()) / 1000));
    e.textContent = t("Undone automatically in {0}", [`${Math.floor(s / 60)}:${String(s % 60).padStart(2, "0")}`]);
    if (s === 0 && !e.dataset.fini) { e.dataset.fini = "1"; fini = true; }
  }
  if (fini && etat.page === "serveur") setTimeout(rendrePage, 3000);
}, 1000);
$("#page").addEventListener("change", async (ev) => {
  if (etat.page !== "serveur" || !ev.target.matches(".srv-fiche-role")) return;
  const roles = [...document.querySelectorAll(".srv-fiche-role:checked")].map((e) => e.value);
  let p = {}; // C37: without prod, an agent may act on it as on a test server
  if ((srv.fiche.roles || []).includes("prod") && !roles.includes("prod")) p = await preuveMdp(t("Taking the prod role away: retype Athanor's password."), { ok: t("Take prod away"), danger: true });
  if (p && await action(api("serveurs/" + encodeURIComponent(srv.fiche.id), "POST", { roles, ...p }))) toast(t("Roles saved."), "ok");
  rendrePage();
});
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "serveur") return;
  const b = ev.target.closest("[data-srv-rafraichir], [data-srv-couper], [data-srv-durcir], [data-srv-verifier], [data-srv-retirer], [data-srv-garder], [data-srv-tache-arreter], [data-srv-tache-carte]");
  if (!b) return;
  const x = srv.fiche, url = "serveurs/" + encodeURIComponent(x.id), d = b.dataset;
  if (d.srvTacheCarte) { etat.apprCible = d.srvTacheCarte; return; } // the link opens For you, where the card is scrolled to
  if (d.srvTacheArreter) {
    if (!(await confirmer(t("Stop this task? Its agent is cut at once and its access revoked; a change already made stays (see the Runbook)."), { ok: t("Stop"), danger: true }))) return;
    b.disabled = true;
    if (await action(api(url + "/taches/" + encodeURIComponent(d.srvTacheArreter) + "/arreter", "POST"))) toast(t("Task stopped."), "ok");
    return rendrePage();
  }
  if (d.srvGarder) {
    b.disabled = true;
    if (await action(api(url, "POST", { garder: d.srvGarder }))) toast(t("Change kept: its automatic rollback is cancelled."), "ok");
    return rendrePage();
  }
  if (d.srvRetirer !== undefined) {
    const cle = $("#srv-retirer-cle").checked;
    const texte = cle ? t("Remove {0} and its key from the vault? Nothing is changed on the server.", [x.nom]) : t("Remove {0}? Its key stays in the vault; nothing is changed on the server.", [x.nom]);
    const p = await preuveMdp(texte + " " + t("Retype Athanor's password."), { ok: t("Remove"), danger: true }, texte); // C37
    if (!p || !(await action(api(url, "DELETE", p)))) return;
    if (cle) await action(api("coffre/" + encodeURIComponent(x.cle), "DELETE"));
    toast(t("{0} removed.", [x.nom]), "ok");
    location.hash = "#serveurs"; return;
  }
  const texte = b.textContent;
  b.disabled = true;
  if (d.srvRafraichir !== undefined || d.srvDurcir) b.textContent = t("Reading the server…");
  try {
    const r = await action(api(d.srvVerifier ? "serveurs/ajouter" : url, "POST", d.srvVerifier ? { id: x.id } : d.srvCouper ? { couper_mot_de_passe: true } : d.srvDurcir ? { durcir: true } : { rafraichir: true }));
    if (!r) return;
    if (d.srvDurcir) { toast(r.deja ? t("{0} is already hardened: every step is in place.", [x.nom]) : t("Hardening plan ready ({0} steps): read it and accept it in Approvals.", [String(r.etapes.length)]), "ok"); return; }
    if (r.fiche_erreur) toast(t(r.fiche_erreur), "erreur");
    toast(d.srvCouper ? t("Password logins are cut on {0}.", [x.nom]) : d.srvVerifier ? t("{0} added", [x.nom]) : t("Sheet of {0} read again.", [x.nom]), "ok");
  } finally { b.disabled = false; b.textContent = texte; }
  rendrePage();
});

pages.audit = async () => {
  const f = aud.f, qs = new URLSearchParams(Object.entries(f).filter(([, v]) => v)), lignes = [];
  let d, avant = 0;
  for (let i = 0; i < aud.pages; i++) { // the pages already opened with Older stay open across a live refresh
    if (avant) qs.set("avant", avant);
    d = await api("hub/audit?" + qs); if (i === 0) aud.d = d;
    lignes.push(...d.lignes); avant = d.suite; if (!avant) break;
  }
  const lang = document.documentElement.lang || "en", v = aud.verif;
  const choix = (id, val, opts) => `<select id="${id}">${opts.map(([k, lib]) => `<option value="${h(k)}" ${k === val ? "selected" : ""}>${h(lib)}</option>`).join("")}</select>`;
  let jour = "", liste = "";
  for (const l of lignes) {
    const j = new Date(l.t).toLocaleDateString(lang, { weekday: "long", day: "numeric", month: "long", year: "numeric" });
    if (j !== jour) { liste += `${jour ? "</ul>" : ""}<h3 class="aud-jour">${h(j)}</h3><ul class="aud-liste">`; jour = j; }
    liste += ligneAudit(l, lang);
  }
  return `<div class="audit">
    <div class="journal-tools">
      ${choix("aud-mois", f.mois, [["", t("All months")], ...aud.d.mois.map((m) => [m, m])])}
      ${choix("aud-projet", f.projet, [["", t("All projects")], ...aud.d.projets.map((p) => [p, p])])}
      ${choix("aud-type", f.type, [["", t("All types")], ...Object.entries(TYPES_AUDIT).map(([k, lib]) => [k, t(lib)])])}
      <input type="text" id="aud-q" placeholder="${h(t("filter…"))}" value="${h(f.q)}" aria-label="${h(t("filter…"))}">
      <span class="espace"></span>
      <button type="button" class="btn" data-aud-verifier>${t("Check the log")}</button>
    </div>
    ${v ? v.ok ? `<p class="bloc aud-verif ok">✓ ${t("The log is intact: {0} lines checked, none changed, removed or inserted.", [v.lignes])}</p>`
      : `<p class="bloc aud-verif echec">✗ ${t("Line {0} of {1} is {2}: the log was touched from that line on.", [v.ligne, v.fichier, t(RAISONS_AUDIT[v.raison] || v.raison)])}</p>` : ""}
    ${liste ? liste + "</ul>" : `<div class="vide">${Object.values(f).some(Boolean) ? t("No line matches these filters.") : t("Nothing in the audit log yet: the modules' requests, the access given or taken and the approvals will be written here.")}</div>`}
    ${avant ? `<p><button type="button" class="btn" data-aud-plus>${t("Older lines")}</button></p>` : ""}
    <p class="muet petit">${t("Each line carries the fingerprint of the one before: a line changed or removed shows at the check. This log is only written by Athanor, never by a page; the pilot's own log is in Log.")}</p>
  </div>`;
};
$("#page").addEventListener("click", async (ev) => {
  if (etat.page !== "audit") return;
  const b = ev.target.closest("[data-aud-verifier], [data-aud-plus]"); if (!b) return;
  if (b.dataset.audPlus !== undefined) { aud.pages++; return rendrePage(); }
  const r = await action(api("hub/audit/verifier", "POST")); if (r) { aud.verif = r; rendrePage(); }
});
$("#page").addEventListener("change", (ev) => {
  const k = etat.page === "audit" && { "aud-mois": "mois", "aud-projet": "projet", "aud-type": "type", "aud-q": "q" }[ev.target.id];
  if (k) { aud.f[k] = ev.target.value.trim(); aud.pages = 1; rendrePage(); }
});

async function rendrePage() {
  // renders overlap (a live refresh of one page, a click to another): only the last one asked is shown
  if (!etat.page) return;
  const n = ++numeroRendu;
  const p = pages[etat.page] || pages.suivi;
  let html;
  try { html = await p(); } catch (e) { if (e.message === t("Locked")) return; html = `<div class="vide">${h(e.message)}</div>`; }
  if (n !== numeroRendu) return;
  const defils = positionsDefil();
  // the field being typed in keeps its focus and its cursor across a live refresh (e.g. the Team page's search)
  const a = document.activeElement, saisie = a && a.id && $("#page").contains(a) && a.matches("input[type=text], textarea") ? [a.id, a.selectionStart, a.selectionEnd] : null;
  garderFocus($("#page"), () => { $("#page").innerHTML = html; });
  etat.pageRendue = etat.page;
  if (etat.visite >= 0 && !$("#visite").hidden) { etat.visiteCible = null; placerVoile(); placerBulle(); } // the page rebuilt: the target found again, or the bubble aside
  // text of the Home bar: kept across a live refresh, then handed to the chat or the new project's request
  const barre = $("#accueil-texte"); if (barre) barre.value = etat.accueilTexte || "";
  const pre = etat.preRempli;
  if (pre && pre.page === etat.page) {
    const z = $(pre.page === "chat" ? "#chat-texte" : "#np-demande");
    if (z) { etat.preRempli = null; z.value = pre.texte; z.focus(); }
  } else if (pre && etat.page !== "accueil") etat.preRempli = null; // went elsewhere: the text is not carried to a later visit
  remettreDefil(defils);
  // an approval opened from the bell: its card in view, its first field focused
  const appr = etat.apprCible && etat.page === "pourvous" && document.getElementById("appr-" + etat.apprCible);
  if (appr) { etat.apprCible = null; appr.scrollIntoView({ block: "center" }); appr.querySelector("select, input, button")?.focus({ preventScroll: true }); }
  const champ = saisie && document.getElementById(saisie[0]);
  if (champ) { champ.focus({ preventScroll: true }); champ.setSelectionRange(saisie[1], saisie[2]); }
  suivrePlan();
  proposerTutoriel();
  if (etat.page === "journal") rendreJournal();
  if (etat.page === "sante") rendreSortie();
}

// the scroll of each list of the page, kept across a rebuild: same element = same classes, same rank
function cleDefil(el) { return el.tagName + "." + [...el.classList].filter((c) => c !== "ouvert").sort().join("."); }
function positionsDefil() {
  const vus = {}, out = [];
  for (const el of $("#page").querySelectorAll("*")) {
    if (el.scrollHeight <= el.clientHeight && el.scrollWidth <= el.clientWidth) continue;
    const k = cleDefil(el); vus[k] = (vus[k] || 0) + 1;
    if (el.scrollTop || el.scrollLeft) out.push([k, vus[k], el.scrollTop, el.scrollLeft]);
  }
  return out;
}
function remettreDefil(defils) {
  if (!defils.length) return;
  const vus = {}, par = new Map(defils.map((d) => [d[0] + "#" + d[1], d]));
  for (const el of $("#page").querySelectorAll("*")) {
    if (el.scrollHeight <= el.clientHeight && el.scrollWidth <= el.clientWidth) continue;
    const k = cleDefil(el); vus[k] = (vus[k] || 0) + 1;
    const d = par.get(k + "#" + vus[k]); if (d) { el.scrollTop = d[2]; el.scrollLeft = d[3]; }
  }
}
// auto-scroll of the plan: the milestone being built brought into view when it changes (or when switched on)
function suivrePlan(force) {
  const liste = $("#page .plan:not(.jalons-mini)"); if (!liste) return;
  const li = liste.querySelector("li.en-cours");
  const cle = li ? li.dataset.jalonCle : "";
  if (!etat.defilAuto.plan || !li || (!force && cle === etat.planSuivi)) { if (!etat.defilAuto.plan) etat.planSuivi = ""; return; }
  etat.planSuivi = cle;
  const z = li.getBoundingClientRect(), c = liste.getBoundingClientRect();
  liste.scrollTo({ top: liste.scrollTop + (z.top - c.top) - (liste.clientHeight - li.offsetHeight) / 2, behavior: force ? "smooth" : "auto" });
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
  const bd = ev.target.closest("[data-defil]");
  if (bd) {
    ev.stopImmediatePropagation();
    const q = bd.dataset.defil; etat.defilAuto[q] = !etat.defilAuto[q]; ecrireDefilAuto(); majBoutonDefil(q);
    if (q === "journal" && etat.defilAuto.journal) { const el = $("#journal"); if (el) el.scrollTop = el.scrollHeight; }
    if (q === "plan") suivrePlan(true);
    return;
  }
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
  const pageMode = page === "projet" || (page === "nouveau" && etat.npMode);
  if (b.dataset.max && pageMode) return changerMode({ parallele_max: Number(b.dataset.max) });
  if (b.dataset.cadrage !== undefined && pageMode) return changerMode({ cadrage_max: Number(b.dataset.cadrage) });
  if (b.dataset.dureeMax !== undefined && pageMode) return changerMode({ duree_max: Number(b.dataset.dureeMax) });
  if (b.dataset.detailReglage) return detailReglage(b.dataset.detailReglage);
  if (b.dataset.detailMode) return detailMode(b.dataset.detailMode);
  if (page === "nouveau") {
    if (b.dataset.npMode) { etat.npRejoindre = b.dataset.npMode === "rejoindre"; return rendrePage(); }
    if (b.id === "rj-rejoindre") {
      if (b.disabled) return;
      const depot = depotSaisi($("#rj-depot").value);
      if (!depot) { toast(t("Write the repository as OWNER/NAME (e.g. octocat/my-site)."), "erreur"); return; }
      const nom = $("#np-nom").value.trim() || nomDepuisDepot(depot);
      const branche = $("#rj-branche").value.trim() || "main";
      if (!(etat.ghConnexion || {}).connecte) { toast(t("To join a project, connect this computer to GitHub first, once (Settings › Options › GitHub)."), "erreur"); return; }
      b.disabled = true;
      const r = await action(api("projets", "POST", { nom, dossier: $("#np-dossier").value, demande: "", rejoindre: true }));
      b.disabled = false;
      if (r) {
        etat.projet = r.nom; etat.evenements = []; etat.dernierJournal = 0;
        const corps = { action: "lier", mode: "existant", valeur: depot, auto: true, atelier: true, branche, equipe: true };
        b.disabled = true; b.textContent = t("Linking…");
        const rl = await action(api("github", "POST", corps));
        b.disabled = false; b.textContent = t("Join the project");
        if (rl && rl.lie_compte) { if (!(await resultatLierCompte(rl, corps))) return; }
        else toast(t("A window opens: sign in to GitHub there. The project then arrives from the other computers."), "ok");
        etat.npRejoindre = false;
        await rafraichir(); location.hash = "#suivi";
      }
      return;
    }
    if (b.id === "np-creer") {
      if (b.disabled) return;
      const gh = lireChoixGitHub("np", $("#np-nom").value); if (gh === false) return;
      if (gh && !(etat.ghConnexion || {}).connecte) { toast(t("To link a project, connect this computer to GitHub first, once (Settings › Options › GitHub)."), "erreur"); return; }
      b.disabled = true; // one project per click, even on a double-click
      const r = await action(api("projets", "POST", { nom: $("#np-nom").value, dossier: $("#np-dossier").value, demande: $("#np-demande").value }));
      b.disabled = false;
      if (r) {
        etat.projet = r.nom; etat.evenements = []; etat.dernierJournal = 0;
        if ((etat.pieces.np || []).length) await envoyerPieces("np", "demande", r.nom);
        // the settings chosen beside the form, when they differ from the defaults
        const md = etat.npMode; etat.npMode = null;
        if (md && (md.parallele || md.eco || md.rapide || md.cadrage_max || md.duree_max !== -1)) await api("mode", "POST", { parallele: md.parallele, parallele_max: md.parallele_max, eco: md.eco, rapide: md.rapide, cadrage_max: md.cadrage_max || 0, duree_max: md.duree_max }).catch((e) => toast(e.message, "erreur"));
        // the sign-in window opens (the link shows under Manage), or, with this computer connected, linked at once
        if (gh) { const rl = await action(api("github", "POST", gh)); if (rl && rl.lie_compte) await resultatLierCompte(rl, gh); if (rl && rl.cree) toast(t("Private repository created and linked: {0}", [rl.cree]), "ok"); }
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
  if (page === "journal" && b.classList.contains("ev")) { const n = Number(b.dataset.n); etat.ouverts.has(n) ? etat.ouverts.delete(n) : etat.ouverts.add(n); if (etat.defilAuto.journal) { etat.defilAuto.journal = false; ecrireDefilAuto(); majBoutonDefil("journal"); } return rendreJournal(); }
  if (page === "sante") {
    if (b.dataset.ongletSante) { etat.ongletSante = b.dataset.ongletSante; return rendrePage(); }
    const ligne = b.closest(".verif"); const id = ligne && ligne.dataset.id;
    if (b.id === "st-tout") { await action(api("sante/verifier", "POST", { id: "tout" })); return rendrePage(); }
    if (b.id === "st-installer") return action(api("sante/installer", "POST"));
    if (b.id === "st-recompiler") { const r = await action(api("sante/recompiler", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-mcp-hub") { const r = await action(api("sante/mcp-hub", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-agent-serveurs") { const r = await action(api("sante/agent-serveurs", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-coffre") { const r = await action(api("sante/coffre", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-barriere") { const r = await action(api("sante/barriere", "POST")); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; } return rendrePage(); }
    if (b.id === "st-libre-btn") { const r = await action(api("sante/libre", "POST", { texte: $("#st-libre").value })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = null; rendrePage(); } return; }
    if (b.dataset.voir !== undefined) { etat.tacheChoisie = b.dataset.voir; etat.verifChoisie = id; return rendrePage(); }
    if (b.hasAttribute("data-verifier")) { const r = await action(api("sante/verifier", "POST", { id })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
    if (b.hasAttribute("data-reparer")) { const r = await action(api("sante/reparer", "POST", { id })); if (r && r.tache) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
    if (b.hasAttribute("data-claude")) { const r = await action(api("sante/claude", "POST", { id, complement: $("#st-complement").value })); if (r) { etat.tacheChoisie = r.tache; etat.verifChoisie = id; } return rendrePage(); }
  }
  if (page === "chat") {
    if (b.id === "chat-envoyer") return envoyerChat();
    if (b.id === "chat-info") return aideChat();
    if (b.id === "chat-stop") { await action(api("chat/arreter", "POST")); return majChat(true); }
    if (b.id === "chat-nouveau") { if (await confirmer(t("Start a new conversation? Claude forgets this one."), { ok: t("New conversation") })) { await action(api("chat/effacer", "POST")); majChat(true); } return; }
    if (b.dataset.chatModele !== undefined) { chat.modele = b.dataset.chatModele; try { localStorage.setItem("chatModele", chat.modele); } catch (e) { /* ignore */ } b.parentElement.querySelectorAll("button").forEach((x) => x.classList.toggle("actif", x === b)); return; }
    if (b.dataset.chatAgent !== undefined) return ajouterAgentChat(b);
    if (b.dataset.chatMcp !== undefined) return ajouterMCPChat(b);
    if (b.dataset.chatNote !== undefined) { b.disabled = true; await action(api("chat/note", "POST", { index: Number(b.dataset.chatNote) })); return majChat(true); }
  }
  if (page === "reglages") {
    if (b.dataset.ongletReglages) { etat.ongletReglages = b.dataset.ongletReglages; return rendrePage(); }
    if (b.dataset.pour) { const i = $("#" + b.dataset.pour); i.value = b.dataset.val; b.parentElement.querySelectorAll("button").forEach((x) => x.classList.toggle("actif", x === b)); if (i.dataset.cle === "CLAUDE_AUTH") majModeConnexion(modeConnexion(i.value)); return; }
    if (b.id === "rg-enregistrer") return action(api("env", "POST", lireEnvFormulaire()));
    if (b.id === "rg-redemarrer") { const r = await action(api("env", "POST", lireEnvFormulaire())); if (r) await action(api("pilote/redemarrer-apres-cycle", "POST")); return; }
  }
  if (page === "reglages") {
    if (b.dataset.gerer) { if (b.dataset.archive) { location.hash = "#archives"; return; } etat.projet = b.dataset.gerer; etat.evenements = []; etat.dernierJournal = 0; await rafraichir(); location.hash = "#projet"; return; }
    if (b.dataset.retirerProjet) { if (await confirmer(t("Remove “{0}” from the list? Its folder stays intact.", [b.dataset.retirerProjet]), { ok: t("Remove from list"), danger: true })) { await action(api("projets/" + encodeURIComponent(b.dataset.retirerProjet), "DELETE")); if (etat.projet === b.dataset.retirerProjet) etat.projet = ""; await rafraichir(); rendrePage(); } return; }
    if (b.id === "op-guide") { // the wizard, then every tutorial again at the next visit of its part (false: reset, never a reminder)
      const tours = Object.fromEntries((etat.hubModules || []).filter((m) => (m.tutoriel || []).length).map((m) => [m.id, false]));
      await action(api("preferences", "POST", { bienvenue_vue: false, tours })); etat.toursVus = new Set(); if (etat.vue) etat.vue.tours = { ...etat.vue.tours, ...tours };
      etat.assistant = 0; return rendreAssistant();
    }
    if (b.id === "mdp-enregistrer") { const r = await action(api("mot-de-passe", "POST", { ancien: ($("#mdp-ancien") || {}).value || "", nouveau: $("#mdp-nouveau").value })); if (r) { toast(r.defini ? t("Lock enabled: the padlock is at the bottom left.") : t("Lock removed."), "ok"); rendrePage(); rafraichir(); } return; }
  }
});

// an image pasted in the chat's field (a screenshot): attached to the message
$("#page").addEventListener("paste", (ev) => {
  if (!["chat-texte", "accueil-texte"].includes(ev.target.id) || !ev.clipboardData || !ev.clipboardData.files.length) return;
  ev.preventDefault();
  ajouterPieces(ev.target.id === "chat-texte" ? "ct" : "hc", ev.clipboardData.files);
});
$("#page").addEventListener("keydown", (ev) => {
  // on a touch screen, Enter is a new line (the keyboard has no Shift+Enter): the Send button sends
  if (ev.target.id === "chat-texte" && ev.key === "Enter" && !ev.shiftKey && !ev.isComposing && !tactile()) { ev.preventDefault(); envoyerChat(); }
});

$("#page").addEventListener("change", (ev) => {
  const i = ev.target;
  if (i.id === "chat-modif") { chat.modif = i.checked; const m = $("#chat-mode"); if (m) m.innerHTML = modeChat(); }
  if (i.id === "chat-internet") { chat.internet = i.checked; try { localStorage.setItem("chatInternet", i.checked ? "1" : "0"); } catch (e) { /* this device only */ } }
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
  if (i.hasAttribute("data-gh-atelier")) action(api("github", "POST", { action: "atelier", atelier: i.checked })).then((r) => { if (!r) i.checked = !i.checked; });
  if (i.id === "md-parallele" || i.id === "md-eco" || i.id === "md-rapide") changerMode({}); // the switches add up
  // the framing time box typed by hand (10 to 240 min) and the work time limit (1 to 87600 h), checked by the
  // server, saved when the field is left
  const entre = (min, max) => i.value !== "" && Number.isFinite(Number(i.value)) && Math.round(Number(i.value)) >= min && Math.round(Number(i.value)) <= max;
  if (i.id === "md-cadrage" && i.value !== "") { if (entre(10, 240)) changerMode({ cadrage_max: Math.round(Number(i.value)) }); else toast(t("The framing time box is between 10 and 240 minutes."), "erreur"); }
  if (i.id === "md-duree-max" && i.value !== "") { if (entre(1, 87600)) changerMode({ duree_max: Math.round(Number(i.value)) }); else toast(t("The work time limit is between 1 and 87600 hours, or unlimited."), "erreur"); }
});
// attached files: chosen with the button or dropped on a form's zone; a file dropped elsewhere is ignored
// (otherwise the window would open it in place of the application)
window.addEventListener("dragover", (ev) => {
  if (!fichiersGlisses(ev)) return;
  const z = ev.target.closest && ev.target.closest("[data-pieces]");
  document.querySelectorAll("[data-pieces].survol").forEach((x) => x !== z && x.classList.remove("survol"));
  if (z) z.classList.add("survol");
  ev.dataTransfer.dropEffect = z ? "copy" : "none";
});
window.addEventListener("dragleave", (ev) => { if (!ev.relatedTarget) document.querySelectorAll("[data-pieces].survol").forEach((x) => x.classList.remove("survol")); });
window.addEventListener("drop", (ev) => {
  if (!fichiersGlisses(ev)) return;
  document.querySelectorAll("[data-pieces].survol").forEach((x) => x.classList.remove("survol"));
  const z = ev.target.closest && ev.target.closest("[data-pieces]");
  if (z && z.dataset.pieces === "hc") { // a folder dropped on the home conversation is pointed, its files never sent
    const entrees = [...ev.dataTransfer.items].map((i) => i.webkitGetAsEntry && i.webkitGetAsEntry());
    const dossier = entrees.find((e) => e && e.isDirectory);
    const fichiers = [...ev.dataTransfer.files].filter((f, i) => !(entrees[i] && entrees[i].isDirectory));
    if (fichiers.length) ajouterPieces("hc", fichiers);
    if (dossier) ajouterDossierHub(dossier.name);
    return;
  }
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
const CHAMPS_AGENT = { "ag-nom": "nom", "ag-description": "description", "ag-consignes": "consignes", "ag-modele": "modele", "ag-lecture": "lecture_seule" };
for (const genre of ["input", "change"]) $("#page").addEventListener(genre, (ev) => {
  const c = CHAMPS_AGENT[ev.target.id];
  if (c && etat.agForm) etat.agForm[c] = ev.target.type === "checkbox" ? ev.target.checked : ev.target.value;
});
$("#page").addEventListener("input", (ev) => {
  if (ev.target.id === "jr-recherche") { etat.recherche = ev.target.value; rendreJournal(); }
  if (ev.target.id === "ag-recherche") { etat.agRecherche = ev.target.value; filtrerAgents(); }
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
  try { await api("deverrouiller", "POST", { mot_de_passe: $("#verrou-mdp").value }); etat.verrouille = false; $("#verrou-mdp").value = ""; $("#verrou-erreur").textContent = ""; afficherVerrou(); rafraichir(); rendrePage(); chargerMenu(); }
  catch (e) { $("#verrou-erreur").textContent = e.message; }
});
$("#btn-verrou").addEventListener("click", async () => { await action(api("verrouiller", "POST")); etat.verrouille = true; afficherVerrou(); });
$("#btn-urgence").addEventListener("click", async () => {
  if (!(await confirmer(t("Emergency stop? Every pilot stops now (the current cycle is cut), every hub session is revoked, the pending approvals are refused, the “never ask again” rules are removed and the vault is locked. To resume: unlock Athanor, then Start each project."), { ok: t("Emergency stop"), danger: true }))) return;
  const r = await action(api("hub/urgence", "POST", {}));
  if (r && r.resultat && r.resultat.verrouille) { etat.verrouille = true; afficherVerrou(); }
  rafraichir();
});

// --- refresh loop
async function rafraichir() {
  if (etat.nonAppaire) return; // a device the computer removed: nothing more to ask
  try {
    const demande = etat.projet;
    const v = await fetch("/api/etat" + (demande ? "?p=" + encodeURIComponent(demande) : ""), { headers: { "X-Athanor": CSRF } }).then((r) => r.json());
    if (v.erreur_key === "Not paired") return afficherNonAppaire();
    if (etat.projet !== demande) return; // another project chosen meanwhile: this answer is stale, the choice refreshes again
    etat.vue = v;
    document.body.classList.toggle("distant", !!v.distant);
    if (!v.distant && v.dernier_appareil !== etat.dernierAppareil) {
      if (etat.dernierAppareil !== undefined && v.dernier_appareil) toast(t("New device paired: {0}", [v.dernier_appareil.split("|").slice(1).join("|")]), "ok");
      etat.dernierAppareil = v.dernier_appareil;
    }
    marquerDistance(v);
    // a language changed elsewhere (another window, a device, the TUI): the page too, not only the menu (never at start: B2)
    if (v.langue && v.langue !== etat.langue) { const avant = etat.langue; etat.langue = v.langue; appliquerLangue(v.langue); traduireNav(); if (avant && etat.page && etat.assistant < 0) rendrePage(); }
    if ((v.theme || "") !== etat.theme || (v.accent || "") !== etat.accent) { etat.theme = v.theme || ""; etat.accent = v.accent || ""; appliquerTheme(etat.theme, etat.accent); }
    if (!v.verrouille && v.bienvenue && !v.distant && etat.assistant < 0) { etat.assistant = 0; rendreAssistant(); }
    if (v.verrouille !== etat.verrouille) { etat.verrouille = v.verrouille; afficherVerrou(); if (!v.verrouille) rendrePage(); }
    rendreBandeau(v);
    if (etat.visite >= 0 && !$("#visite").hidden) { placerVoile(); placerBulle(); }
    annoncerFins(v);
    if (!v.verrouille) {
      await lireJournal();
      // the Team page follows live, except while text is selected in it (a brief, a report)
      if ((etat.page === "equipe" && !String(window.getSelection() || "") && !etat.agForm) || (etat.page === "suivi" && !document.activeElement.matches("input, textarea, select"))) rendrePage();
      // /api/hub every other tick (every tick on Projects): the bell's alerts; Projects and Home are rebuilt only
      // when a module's tile or an element changed (the card or the bar under the keyboard keeps its focus)
      if (etat.page === "global" || ++etat.hubTic % 2 === 0 || !etat.hub) {
        const d = await api("hub").catch(() => null);
        if (d) {
          const cloche = (x) => JSON.stringify([(x || {}).alertes, (x || {}).ignorees]), change = cloche(d) !== cloche(etat.hub);
          etat.hub = d; etat.hubLu = Date.now(); proposerTutoriel(); // the first /api/hub of a visit: the page's tour may wait for it
          const sig = JSON.stringify([d.modules || [], d.elements || []]);
          if (etat.page === "global" ? sig !== etat.globalSig : etat.page === "accueil" && sig !== etat.hubSig) rendrePage();
          if (change) rendreBandeau(v);
        }
      }
      if (etat.page === "chat") majChat(false);
      if (etat.page === "accueil") majHubChat(false);
      if (etat.page === "distance") await majDistance();
      // after "Connect to GitHub": the window is the user's; the blocks follow until the connection is there
      if (etat.ghConnexionSuivi && Date.now() < etat.ghConnexionSuivi) {
        const c = await majConnexionGH();
        if (c && c.connecte) {
          etat.ghConnexionSuivi = 0; toast(t("Connected to GitHub as {0}.", [c.login]), "ok");
          if (etat.page === "projet") majGitHub();
          // the new project's page keeps what was typed: only its notices change
          document.querySelectorAll("[data-avis-connexion]").forEach((x) => { x.className = "ok petit"; x.textContent = "✔ " + t("Connected as {0}", [c.login]); });
          const rj = $("#rj-rejoindre"); if (rj) rj.disabled = false;
        }
      }
      if (etat.ghAccesSuivi && Date.now() < etat.ghAccesSuivi) {
        const c = await majConnexionGH();
        if (c && c.acces_garde) { etat.ghAccesSuivi = 0; toast(t("This computer now creates your repositories without a window."), "ok"); }
      }
      // the update's progress, in place (Settings › Options)
      const blocMaj = document.getElementById("bloc-version");
      if (blocMaj && etat.optionsVue) {
        const sig = JSON.stringify(v.maj || {});
        if (sig !== etat.majSig) { etat.majSig = sig; blocMaj.innerHTML = `<h2>${t("Version")}</h2>${rendreBlocMaj(etat.optionsVue)}`; }
      }
      if (etat.page === "serveur" && srv.tachesSig && !document.activeElement.matches("input, textarea, select") && ++etat.srvTic % 2 === 0) { // a task's states, live
        const r = await api("serveurs/" + encodeURIComponent(srv.id || "") + "/taches").catch(() => null);
        if (r && JSON.stringify(r) !== srv.tachesSig) rendrePage();
      }
      if (etat.page === "pourvous" && !document.activeElement.matches("input, textarea, select") && ++etat.pvTic % 4 === 0) {
        const [r, ap] = await Promise.all([api("pourvous").catch(() => null), api("hub/approbations").catch(() => null)]);
        if ((r && JSON.stringify(r.taches || []) !== etat.pvSignature) || (ap && signatureAppr(ap) !== etat.apprSignature)) rendrePage();
      }
      annoncerTaches(v);
      // the checks and the output update in place: the page itself is never rebuilt under the user's typing
      if (etat.page === "sante" && (document.querySelector(".verif.v-en_cours") || etat.tacheChoisie)) await majSante();
    }
    etat.majRedemarre = !!(v.maj && v.maj.en_cours === "redemarrage");
  } catch (e) {
    // server briefly unreachable; after an update, the new version opens its own window
    if (etat.majRedemarre && !etat.majAnnonce) { etat.majAnnonce = true; toast(t("Athanor restarts in a new window: you can close this one.")); try { window.close(); } catch (x) { /* not ours to close */ } }
  }
}

(async () => {
  await rafraichir();
  const v = etat.vue || {};
  etat.langue = v.langue || "en"; appliquerLangue(etat.langue); traduireNav();
  await chargerMenu();
  if (location.hash === "#bienvenue") location.hash = "#suivi";
  naviguer();
  setInterval(rafraichir, 1500);
})();
