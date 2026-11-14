/* Athanor — the e-mail module's page (its own file: a module joins the hub without touching app.js). */
"use strict";

pages.email = async () => {
  const d = await api("email/comptes");
  const comptes = d.comptes || [];
  const carte = (c) => `<div class="carte"><div class="ligne"><span class="ico ico-email" aria-hidden="true"></span><strong>${h(c.nom)}</strong></div>
    <p class="muet petit">${h(c.utilisateur)} · ${h(c.hote)}:${h(String(c.port))}</p></div>`;
  return `<div class="email">
    <p class="muet email-explication">${t("Add an SMTP account (your provider's sending server): Athanor keeps its password in the vault, tests the sending and gives your deployed apps what they need to send emails.")}</p>
    ${comptes.length ? `<div class="grille-projets">${comptes.map(carte).join("")}</div>` : `<div class="vide">${t("No email account yet.")}</div>`}
  </div>`;
};
