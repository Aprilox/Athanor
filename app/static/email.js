/* Athanor — the e-mail module's page (its own file: a module joins the hub without touching app.js). */
"use strict";

// presets: host, port and security of common providers, filled into the form and still editable
const PRESETS_EMAIL = [
  { id: "", nom: "Other", hote: "", port: 587, securite: "starttls" },
  { id: "gmail", nom: "Gmail", hote: "smtp.gmail.com", port: 587, securite: "starttls" },
  { id: "outlook", nom: "Outlook / Microsoft 365", hote: "smtp.office365.com", port: 587, securite: "starttls" },
  { id: "ovh", nom: "OVHcloud", hote: "ssl0.ovh.net", port: 465, securite: "tls" },
  { id: "brevo", nom: "Brevo", hote: "smtp-relay.brevo.com", port: 587, securite: "starttls" },
  { id: "mailgun", nom: "Mailgun", hote: "smtp.mailgun.org", port: 587, securite: "starttls" },
  { id: "sendgrid", nom: "SendGrid", hote: "smtp.sendgrid.net", port: 587, securite: "starttls" },
];
const SECURITES_EMAIL = { starttls: "STARTTLS (port 587)", tls: "TLS (port 465)", none: "None (this PC only)" };
const em = { comptes: [], form: null, test: null, config: null, projets: [], envs: [] }; // form: null, {} (new) or {id}; test, config: the account whose test or sending form is open

function formulaireEmail() {
  const x = em.form.id && em.comptes.find((c) => c.id === em.form.id);
  if (em.form.id && !x) { em.form = null; return ""; }
  const v = x || { nom: "", hote: "", port: 587, securite: "starttls", utilisateur: "", expediteur: "" };
  return `<form class="bloc cof-form" id="em-form" autocomplete="off">
    <h2>${x ? h(x.nom) : t("New email account")}</h2>
    ${x ? "" : `<label class="champ"><span>${t("Provider")}</span><select id="em-preset">${PRESETS_EMAIL.map((p) => `<option value="${p.id}">${h(p.id ? p.nom : t(p.nom))}</option>`).join("")}</select></label>
    <p class="muet petit">${t("The provider fills in the server, port and security: you can still change them.")}</p>`}
    <label class="champ"><span>${t("Name")}</span><input type="text" id="em-nom" maxlength="100" value="${h(v.nom)}" placeholder="${h(t("e.g. Shop newsletter"))}" required></label>
    <div class="cof-choix">
      <label class="champ"><span>${t("SMTP server")}</span><input type="text" id="em-hote" maxlength="253" spellcheck="false" value="${h(v.hote)}" placeholder="smtp.example.org" required></label>
      <label class="champ"><span>${t("Port")}</span><input type="number" id="em-port" min="1" max="65535" value="${h(String(v.port))}" required></label>
      <label class="champ"><span>${t("Security")}</span><select id="em-securite">${Object.entries(SECURITES_EMAIL).map(([k, l]) => `<option value="${k}"${k === v.securite ? " selected" : ""}>${h(t(l))}</option>`).join("")}</select></label>
    </div>
    <label class="champ"><span>${t("User name")}</span><input type="text" id="em-utilisateur" maxlength="254" spellcheck="false" autocomplete="off" value="${h(v.utilisateur)}" placeholder="contact@example.org"></label>
    <label class="champ"><span>${t("Sender")}</span><input type="text" id="em-expediteur" maxlength="320" spellcheck="false" value="${h(v.expediteur)}" placeholder="Shop &lt;contact@example.org&gt;" required></label>
    <label class="champ"><span>${t("Password")}</span><input type="password" id="em-mdp" maxlength="1024" autocomplete="new-password" spellcheck="false" placeholder="${x ? "•••• " + h(t("saved")) : ""}"${x ? "" : " required"}></label>
    <p class="muet petit">${x ? t("The password is never shown again: type a new one to replace it, or leave the field empty.") : t("The password goes into the vault (type “E-mail account”), never into the account's file.")}</p>
    <div class="ligne"><button type="submit" class="btn btn-primaire">${x ? t("Save") : t("Add")}</button><button type="button" class="btn" data-em-annuler>${t("Cancel")}</button></div>
  </form>`;
}

pages.email = async () => {
  const d = await api("email/comptes");
  em.comptes = d.comptes || []; em.projets = d.projets_hub || []; em.envs = d.envs || [];
  const carte = (c) => `<div class="carte" data-em-compte="${h(c.id)}"><div class="ligne"><span class="ico ico-email" aria-hidden="true"></span><strong>${h(c.nom)}</strong></div>
    <p class="muet petit">${h(c.utilisateur || c.expediteur)} · ${h(c.hote)}:${h(String(c.port))} · ${h(t(SECURITES_EMAIL[c.securite] || c.securite))}</p>
    <p class="muet petit">${t("Sender")}: ${h(c.expediteur)}</p>
    ${c.dernier_test ? `<p class="petit ${c.dernier_test.ok ? "ok" : "echec"}" data-em-resultat>${t("Last test ({0}, to {1}): {2}", [new Date(c.dernier_test.t).toLocaleString(), h(c.dernier_test.destinataire), h(t(c.dernier_test.message))])}</p>` : ""}
    ${em.test === c.id ? `<form class="ligne m0" data-em-test-form="${h(c.id)}" autocomplete="off"><label class="champ"><span>${t("Recipient")}</span><input type="email" id="em-dest" maxlength="320" spellcheck="false" required value="${h(em.dest || "")}" placeholder="you@example.org"></label>
      <button type="submit" class="btn btn-primaire">${t("Send")}</button><button type="button" class="btn" data-em-test-annuler>${t("Cancel")}</button></form>` : ""}
    ${(c.projets || []).length ? `<p class="petit" data-em-projets>${t("Sends for")}: ${c.projets.map((p) => `<span class="puce-mention">${h(p.projet)} · ${h(p.env)}<button type="button" class="puce-x" data-em-arreter="${h(c.id)}" data-projet="${h(p.projet)}" data-env="${h(p.env)}" aria-label="${h(t("Stop sending"))} ${h(p.projet)} ${h(p.env)}">×</button></span>`).join(" ")}</p>` : `<p class="muet petit">${t("Not used by any project yet.")}</p>`}
    ${em.config === c.id ? (em.projets.length ? `<form class="m0" data-em-config-form="${h(c.id)}"><div class="ligne m0"><label class="champ"><span>${t("Project")}</span><select id="em-projet">${em.projets.map((p) => `<option>${h(p)}</option>`).join("")}</select></label>
      <label class="champ"><span>${t("Environment")}</span><select id="em-env">${em.envs.map((e) => `<option>${h(e)}</option>`).join("")}</select></label></div>
      <p class="muet petit">${t("The project receives SMTP_HOST, SMTP_PORT, SMTP_SECURE, SMTP_USER, SMTP_FROM and SMTP_PASSWORD: in dev through the pilot's secrets, in staging and prod in the deployed app's .env.")}</p>
      <div class="ligne m0"><button type="submit" class="btn btn-primaire">${t("Give the settings")}</button><button type="button" class="btn" data-em-config-annuler>${t("Cancel")}</button></div></form>` : `<p class="muet petit">${t("No project yet.")}</p>`) : ""}
    <div class="ligne m0"><button type="button" class="btn" data-em-tester="${h(c.id)}">${t("Send a test e-mail")}</button><button type="button" class="btn" data-em-configurer="${h(c.id)}">${t("Configure sending")}</button><button type="button" class="btn" data-em-modifier="${h(c.id)}">${t("Edit")}</button><button type="button" class="btn btn-danger" data-em-supprimer="${h(c.id)}">${t("Delete")}</button></div></div>`;
  return `<div class="email">
    <p class="muet email-explication">${t("Add an SMTP account (your provider's sending server): Athanor keeps its password in the vault, tests the sending and gives your deployed apps what they need to send emails.")}</p>
    ${em.form ? formulaireEmail() : `<div class="ligne"><button type="button" class="btn btn-primaire" data-em-ajouter>${t("Add an email account")}</button></div>`}
    ${em.comptes.length ? `<div class="grille-projets">${em.comptes.map(carte).join("")}</div>` : em.form ? "" : `<div class="vide">${t("No email account yet.")}</div>`}
  </div>`;
};

document.addEventListener("change", (ev) => {
  if (ev.target.id !== "em-preset") return;
  const p = PRESETS_EMAIL.find((x) => x.id === ev.target.value);
  if (!p || !p.id) return;
  $("#em-hote").value = p.hote; $("#em-port").value = p.port; $("#em-securite").value = p.securite;
  if (!$("#em-nom").value.trim()) $("#em-nom").value = p.nom;
});

document.addEventListener("click", async (ev) => {
  const b = ev.target.closest("[data-em-ajouter], [data-em-modifier], [data-em-annuler], [data-em-supprimer], [data-em-tester], [data-em-test-annuler], [data-em-configurer], [data-em-config-annuler], [data-em-arreter]");
  if (!b) return;
  if (b.hasAttribute("data-em-configurer")) { em.config = b.dataset.emConfigurer; return rendrePage().then(() => $("#em-projet")?.focus()); }
  if (b.hasAttribute("data-em-config-annuler")) { em.config = null; return rendrePage(); }
  if (b.hasAttribute("data-em-arreter")) {
    const { projet, env } = b.dataset;
    if (!(await confirmer(t("Stop sending for {0} ({1})? The app loses its SMTP_ settings.", [projet, env]), { ok: t("Stop sending"), danger: true }))) return;
    if (await action(api("email/configurer", "POST", { compte: b.dataset.emArreter, projet, env, retirer: true }))) { toast(t("Sending stopped for {0} ({1}).", [projet, env]), "ok"); rendrePage(); }
    return;
  }
  if (b.hasAttribute("data-em-tester")) { em.test = b.dataset.emTester; return rendrePage().then(() => $("#em-dest")?.focus()); }
  if (b.hasAttribute("data-em-test-annuler")) { em.test = null; return rendrePage(); }
  if (b.hasAttribute("data-em-ajouter")) { em.form = {}; return rendrePage().then(() => $("#em-preset")?.focus()); }
  if (b.hasAttribute("data-em-modifier")) { em.form = { id: b.dataset.emModifier }; return rendrePage().then(() => $("#em-nom")?.focus()); }
  if (b.hasAttribute("data-em-annuler")) { em.form = null; return rendrePage(); }
  const c = em.comptes.find((x) => x.id === b.dataset.emSupprimer);
  const question = (c && c.projets || []).length ? t("Delete the email account {0}? Its password leaves the vault, and these projects lose their sending settings: {1}.", [c.nom, c.projets.map((p) => p.projet + " (" + p.env + ")").join(", ")])
    : t("Delete the email account {0}? Its password leaves the vault, and the projects it was given to lose it.", [c && c.nom]);
  if (!c || !(await confirmer(question, { ok: t("Delete"), danger: true }))) return;
  if (await action(api("email/comptes/" + encodeURIComponent(c.id), "DELETE"))) { toast(t("{0} deleted.", [c.nom]), "ok"); rendrePage(); }
});

// the test send: its result in plain words (a toast and the card); 20 s at most on the server's side
document.addEventListener("submit", async (ev) => {
  const f = ev.target.closest("[data-em-test-form]");
  if (!f) return;
  ev.preventDefault();
  em.dest = $("#em-dest").value.trim();
  const bouton = f.querySelector("[type=submit]");
  bouton.disabled = true; bouton.textContent = t("Sending…");
  let r = null;
  try { r = await api("email/comptes/" + encodeURIComponent(f.dataset.emTestForm) + "/test", "POST", { destinataire: em.dest }); }
  catch (e) { toast(e.message, "erreur"); bouton.disabled = false; bouton.textContent = t("Send"); return; }
  toast(t(r.message), r.ok ? "ok" : "erreur");
  em.test = null; rendrePage();
});

// « Configure sending »: the account's SMTP_ settings granted to a project and an environment (Athanor's password retyped)
document.addEventListener("submit", async (ev) => {
  const f = ev.target.closest("[data-em-config-form]");
  if (!f) return;
  ev.preventDefault();
  const projet = $("#em-projet").value, env = $("#em-env").value;
  const p = await preuveMdp(t("Giving the sending settings to a project: retype Athanor's password."), { ok: t("Give the settings") });
  if (!p) return;
  if (await action(api("email/configurer", "POST", { compte: f.dataset.emConfigForm, projet, env, ...p }))) {
    const c = em.comptes.find((x) => x.id === f.dataset.emConfigForm);
    toast(t("{0} now sends for {1} ({2}).", [c ? c.nom : "", projet, env]), "ok"); em.config = null; rendrePage();
  }
});

document.addEventListener("submit", async (ev) => {
  if (ev.target.id !== "em-form") return;
  ev.preventDefault();
  const id = em.form && em.form.id;
  const corps = { nom: $("#em-nom").value.trim(), hote: $("#em-hote").value.trim(), port: Number($("#em-port").value) || 0, securite: $("#em-securite").value,
    utilisateur: $("#em-utilisateur").value.trim(), expediteur: $("#em-expediteur").value.trim(), mot_de_passe: $("#em-mdp").value };
  const r = await action(api(id ? "email/comptes/" + encodeURIComponent(id) : "email/comptes", "POST", corps));
  $("#em-mdp").value = ""; // the password never stays in the page
  if (!r) return;
  em.form = null;
  toast(id ? t("{0} saved.", [r.nom]) : t("{0} added.", [r.nom]), "ok"); rendrePage();
});
