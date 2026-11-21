// Launch animation: once per launch of Athanor (the server's id changes at each start; "" = turned off in
// Settings), never at a reload or when the window is opened again; a click or a key skips it.
(function () {
  const el = document.getElementById("splash");
  const meta = document.querySelector('meta[name="lancement"]');
  const id = meta ? meta.content : "";
  let vu = "";
  try { const th = localStorage.getItem("theme"), ac = localStorage.getItem("accent"); if (th) document.documentElement.dataset.theme = th; if (ac) document.documentElement.dataset.accent = ac; vu = localStorage.getItem("lancement") || ""; } catch (e) { /* storage unavailable */ }
  // animation on: the lock screen is the scene's second part (app.css html[data-scene]); paused while hidden
  if (id && id !== "__LANCEMENT__") document.documentElement.dataset.scene = "";
  document.addEventListener("visibilitychange", () => document.documentElement.classList.toggle("scene-pause", document.hidden));
  if (!el) return;
  if (!id || id === vu || id === "__LANCEMENT__") { el.remove(); return; }
  try { localStorage.setItem("lancement", id); } catch (e) { /* shown again next time, no harm */ }
  el.hidden = false;
  const reduit = window.matchMedia && matchMedia("(prefers-reduced-motion: reduce)").matches;
  let fini = false;
  // locked: part 2 is already under the first one, its field focused; a key typed during part 1 goes into it
  const champ = () => { const v = document.getElementById("verrou"); return v && !v.hidden ? document.getElementById("verrou-mdp") : null; };
  function passer(ev) {
    const c = champ();
    if (ev && ev.type === "keydown") {
      if (c && ev.key !== "Escape") c.focus(); // the key lands in the password field, never lost
      else { ev.preventDefault(); ev.stopPropagation(); } // the key only skips it
    }
    if (fini) return;
    fini = true;
    if (c) c.focus();
    document.removeEventListener("keydown", passer, true);
    el.classList.add("splash-fin");
    setTimeout(() => el.remove(), reduit ? 200 : 350);
  }
  el.addEventListener("click", passer);
  document.addEventListener("keydown", passer, true);
  setTimeout(passer, reduit ? 900 : 2400);
})();
