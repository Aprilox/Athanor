#!/usr/bin/env python3
"""Athanor's AI-free tools: they save Claude tokens.

  athanor screenshots [folder]    lists new, changed or suspicious screenshots
                                   and prepares reduced sheets to look at
  athanor screenshots --accept   the current screenshots become the reference
  athanor map                 writes .athanor/MAP.md: files, functions, outlines
  athanor archive              archives old checked milestones and old
                                   log entries into .athanor/archives/
  athanor todo ...             tasks for the user (what you cannot do yourself):
                                   add, edit, list, ok, missing, resolved, wait

Everything is deterministic: nothing is summarised or guessed, nothing is deleted.
"""
import os, re, subprocess, sys, shutil
from pathlib import Path

_p = os.environ.get("PROJECT", "/projet")
if os.name == "nt" and re.match(r"^/[A-Za-z](/|$)", _p):   # native Windows: /c/x is C:/x
    _p = _p[1] + ":/" + _p[3:]
RACINE = Path(_p)
ETAT = RACINE / ".athanor"

# ============================================================================ screenshots
REF = ETAT / "screenshots_ref"
PLANCHES = ETAT / "sheets"
SEUIL_CHANGEMENT = 1.0   # mean difference (%) above which a screenshot counts as "changed"
SEUIL_UNIFORME = 3.0     # standard deviation below which an image is almost uniform


def _signature(chemin):
    from PIL import Image
    im = Image.open(chemin).convert("RGB")
    petit = im.resize((160, 90))
    return im.size, petit


def _ecart(a, b):
    from PIL import ImageChops, ImageStat
    d = ImageChops.difference(a, b)
    return sum(ImageStat.Stat(d).mean) / 3 / 255 * 100


def _ecart_type(im):
    from PIL import ImageStat
    return sum(ImageStat.Stat(im).stddev) / 3


def _planches(fichiers):
    from PIL import Image, ImageDraw
    if PLANCHES.exists():
        shutil.rmtree(PLANCHES)
    if not fichiers:
        return []
    PLANCHES.mkdir(parents=True)
    sorties, L, H, marge = [], 640, 360, 4
    for i in range(0, len(fichiers), 4):
        lot = fichiers[i:i + 4]
        cols = 2 if len(lot) > 1 else 1
        lignes = (len(lot) + 1) // 2
        planche = Image.new("RGB", (cols * (L + marge) + marge, lignes * (H + 22 + marge) + marge), (24, 24, 24))
        dessin = ImageDraw.Draw(planche)
        for j, f in enumerate(lot):
            im = Image.open(f).convert("RGB")
            im.thumbnail((L, H))
            x = marge + (j % 2) * (L + marge)
            y = marge + (j // 2) * (H + 22 + marge)
            dessin.text((x + 2, y + 4), f.name, fill=(235, 235, 235))
            planche.paste(im, (x, y + 20))
        sortie = PLANCHES / f"planche_{i // 4 + 1}.png"
        planche.save(sortie)
        sorties.append(sortie)
    return sorties


def screenshots(args):
    valider = "--accept" in args
    noms = [a for a in args if not a.startswith("--")]
    dossier = (RACINE / noms[0]) if noms else ETAT / "screenshots"
    images = sorted(p for p in dossier.glob("*.png")) if dossier.is_dir() else []
    if valider:
        REF.mkdir(parents=True, exist_ok=True)
        for vieux in REF.glob("*.png"):
            vieux.unlink()
        for p in images:
            shutil.copy2(p, REF / p.name)
        print(f"{len(images)} screenshots saved as the reference.")
        return 0
    if not images:
        print(f"No .png screenshot in {dossier.relative_to(RACINE) if dossier.is_relative_to(RACINE) else dossier}.")
        return 0
    nouvelles, changees, suspectes, identiques = [], [], [], 0
    for p in images:
        taille, sig = _signature(p)
        et = _ecart_type(sig)
        if et < SEUIL_UNIFORME:
            suspectes.append((p, f"almost uniform image (standard deviation {et:.1f}): empty or black screen?"))
        ref = REF / p.name
        if not ref.exists():
            nouvelles.append(p)
            continue
        taille_ref, sig_ref = _signature(ref)
        if taille != taille_ref:
            changees.append((p, f"size {taille_ref[0]}x{taille_ref[1]} -> {taille[0]}x{taille[1]}"))
            continue
        e = _ecart(sig, sig_ref)
        if e >= SEUIL_CHANGEMENT:
            changees.append((p, f"{e:.1f} % difference"))
        else:
            identiques += 1
    disparues = sorted(r.name for r in REF.glob("*.png") if not (dossier / r.name).exists()) if REF.is_dir() else []
    print(f"Screenshots: {len(images)} | identical to the reference: {identiques} | "
          f"changed: {len(changees)} | new: {len(nouvelles)} | suspicious: {len(suspectes)}")
    for p, raison in suspectes:
        print(f"  SUSPICIOUS  {p.name}: {raison}")
    for p, raison in changees:
        print(f"  changed     {p.name} ({raison})")
    for p in nouvelles:
        print(f"  new         {p.name}")
    for n in disparues:
        print(f"  gone        {n}")
    a_voir = []
    for p in [s for s, _ in suspectes] + [c for c, _ in changees] + nouvelles:
        if p not in a_voir:
            a_voir.append(p)
    sorties = _planches(a_voir)
    if sorties:
        print("To look at (4 screenshots per sheet): " + ", ".join(str(s.relative_to(RACINE)) for s in sorties))
    else:
        print("Nothing new to look at: all screenshots are identical to the reference.")
    if not REF.is_dir():
        print("(No reference yet: it is saved after each successful cycle.)")
    return 0


# ============================================================================ map
EXTENSIONS_CODE = {".gd", ".gdshader", ".py", ".js", ".mjs", ".cjs", ".ts", ".tsx", ".jsx", ".vue", ".svelte",
                   ".rs", ".go", ".java", ".kt", ".cs", ".cpp", ".cc", ".c", ".h", ".hpp", ".rb", ".php",
                   ".lua", ".sh", ".glsl", ".frag", ".vert", ".wgsl", ".swift", ".dart", ".sql"}
DEFINITION = re.compile(
    r"^\s*(?:export\s+)?(?:default\s+)?(?:pub(?:\(\w+\))?\s+)?(?:static\s+)?(?:async\s+)?"
    r"(?:func|def|function|fn|class|class_name|struct|interface|enum|signal|trait|impl|type)\s+([A-Za-z_][\w.]*)"
    r"|^\s*(?:export\s+)?const\s+([A-Za-z_]\w*)\s*=\s*(?:async\s*)?(?:\([^)]*\)|[A-Za-z_]\w*)\s*=>"
    r"|^\s*(?:uniform)\s+\w+\s+([A-Za-z_]\w*)"
    r"|^\s*(?:void|vec[234]|float|int|bool|mat[234])\s+([A-Za-z_]\w*)\s*\(")
TAILLE_MAX_FICHIER = 400_000


def _fichiers_suivis():
    try:
        sortie = subprocess.run(["git", "-c", "core.fsmonitor=false", "-c", "core.hooksPath=/dev/null", "-c", "core.untrackedCache=false",
                                 "-C", str(RACINE), "ls-files", "-co", "--exclude-standard"],
                                capture_output=True, text=True, timeout=60).stdout
        return sorted(set(sortie.split("\n")) - {""})
    except Exception:
        return sorted(p.relative_to(RACINE).as_posix() for p in RACINE.rglob("*") if p.is_file())


def carte(args):
    code, docs = [], []
    # the project's files, plus the working documents of the workshop (requirements, design, architecture)
    atelier_docs = sorted(p.relative_to(RACINE).as_posix() for p in (ETAT / "docs").rglob("*.md") if p.is_file() and not p.is_symlink()) if (ETAT / "docs").is_dir() else []
    for rel in dict.fromkeys(_fichiers_suivis() + atelier_docs):
        if rel.startswith((".athanor/", ".pilote/", ".git/", ".claude/", "node_modules/", ".godot/", ".verif/")) and not rel.startswith(".athanor/docs/"):
            continue
        p = RACINE / rel
        if not p.is_file() or p.stat().st_size > TAILLE_MAX_FICHIER:
            continue
        ext = p.suffix.lower()
        if ext in EXTENSIONS_CODE or ext == ".md":
            try:
                lignes = p.read_text(encoding="utf-8", errors="replace").split("\n")
            except Exception:
                continue
            if ext == ".md":
                titres, dans_code = [], False
                for i, l in enumerate(lignes, 1):
                    if l.lstrip().startswith("```"):
                        dans_code = not dans_code
                    elif not dans_code and (m := re.match(r"^(#{1,3})\s+(.*)", l)):
                        titres.append(f"{m.group(1)} {m.group(2).strip()} ({i})")
                docs.append((rel, len(lignes), titres))
            else:
                defs = []
                for i, l in enumerate(lignes, 1):
                    m = DEFINITION.match(l)
                    if m:
                        defs.append(f"{next(g for g in m.groups() if g)} {i}")
                code.append((rel, len(lignes), defs))
    ETAT.mkdir(parents=True, exist_ok=True)
    sortie = ETAT / "MAP.md"
    with open(sortie, "w", encoding="utf-8") as f:
        f.write("# Project map (written by the pilot before each cycle, do not edit)\n\n")
        f.write("Format: `path (lines): name line, name line...`. Search with `grep -n word .athanor/MAP.md`,\n"
                "then read only the useful lines of the file (Read with offset and limit).\n\n")
        f.write("## Documents (headings and line numbers)\n\n")
        for rel, n, titres in docs:
            f.write(f"- `{rel}` ({n}): " + (" · ".join(titres) if titres else "no heading") + "\n")
        f.write("\n## Code (definitions and line numbers)\n\n")
        for rel, n, defs in code:
            f.write(f"- `{rel}` ({n})" + (": " + ", ".join(defs) if defs else "") + "\n")
    print(f"Map written: {sortie.relative_to(RACINE)} ({len(code)} code files, {len(docs)} documents)")
    return 0


# ============================================================================ archive
GARDER_JALONS = 3        # checked milestones kept in PLAN.md, for context
GARDER_ENTREES = 6       # entries kept in LOG.md
SEUIL_PLAN = 6           # only archive beyond this number of surplus checked milestones
SEUIL_JOURNAL = 10
NOTE_PLAN = "> Older completed milestones: `.athanor/archives/PLAN_TERMINE.md`."
NOTE_JOURNAL = "> Older entries: `.athanor/archives/JOURNAL_ANCIEN.md`."


def _ecrire_archive(chemin, titre, texte):
    chemin.parent.mkdir(parents=True, exist_ok=True)
    debut = "" if chemin.exists() else f"# {titre}\n\nArchived automatically by the pilot. Nothing in it is modified.\n\n"
    with open(chemin, "a", encoding="utf-8") as f:
        f.write(debut + texte.rstrip("\n") + "\n\n")


def _inserer_note(lignes, note):
    if any(l.strip() == note for l in lignes):
        return lignes
    for i, l in enumerate(lignes):
        if l.startswith("# "):
            return lignes[:i + 1] + ["", note] + lignes[i + 1:]
    return [note, ""] + lignes


def _compacter(lignes):
    sortie, vides = [], 0
    for l in lignes:
        vides = vides + 1 if l.strip() == "" else 0
        if vides <= 1:
            sortie.append(l)
    return sortie


def _archiver_plan():
    plan = ETAT / "PLAN.md"
    if not plan.exists():
        return "PLAN.md missing"
    lignes = plan.read_text(encoding="utf-8").split("\n")
    blocs, i = [], 0               # (start, end exclusive, checked)
    while i < len(lignes):
        m = re.match(r"^- \[([ xX])\] ", lignes[i])
        if m:
            j = i + 1
            while j < len(lignes) and (lignes[j].startswith((" ", "\t")) and lignes[j].strip() != ""):
                j += 1
            blocs.append((i, j, m.group(1) in "xX"))
            i = j
        else:
            i += 1
    coches = [b for b in blocs if b[2]]
    a_archiver = coches[:-GARDER_JALONS] if len(coches) > GARDER_JALONS else []
    if len(a_archiver) < SEUIL_PLAN:
        return f"PLAN.md: {len(coches)} checked milestones, nothing to archive"
    retires = set()
    texte = []
    for d, f, _ in a_archiver:
        texte += lignes[d:f] + [""]
        retires.update(range(d, f))
    restant = _compacter(_inserer_note([l for k, l in enumerate(lignes) if k not in retires], NOTE_PLAN))
    # Check: no unchecked milestone may disappear.
    avant = [l for l in lignes if re.match(r"^- \[ \] ", l)]
    apres = [l for l in restant if re.match(r"^- \[ \] ", l)]
    if avant != apres:
        return "PLAN.md: check failed, nothing modified"
    _ecrire_archive(ETAT / "archives" / "PLAN_TERMINE.md", "Completed milestones", "\n".join(texte))
    plan.write_text("\n".join(restant), encoding="utf-8")
    return f"PLAN.md: {len(a_archiver)} checked milestones archived"


def _archiver_journal():
    journal = ETAT / "LOG.md"
    if not journal.exists():
        return "LOG.md missing"
    lignes = journal.read_text(encoding="utf-8").split("\n")
    debuts = [i for i, l in enumerate(lignes) if l.startswith("## ")]
    if len(debuts) <= SEUIL_JOURNAL:
        return f"LOG.md: {len(debuts)} entries, nothing to archive"
    coupe = debuts[-GARDER_ENTREES]
    entete, vieux, recent = lignes[:debuts[0]], lignes[debuts[0]:coupe], lignes[coupe:]
    _ecrire_archive(ETAT / "archives" / "JOURNAL_ANCIEN.md", "Log: older entries", "\n".join(vieux))
    nouveau = _compacter(_inserer_note(entete, NOTE_JOURNAL) + [""] + recent)
    journal.write_text("\n".join(nouveau), encoding="utf-8")
    return f"LOG.md: {len(debuts) - GARDER_ENTREES} entries archived"


def archiver(args):
    print(_archiver_plan())
    print(_archiver_journal())
    return 0


# ============================================================================
def main():
    if len(sys.argv) >= 2 and sys.argv[1] == "todo":
        sys.dont_write_bytecode = True
        sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))
        import taches
        return taches.main(sys.argv[2:], str(RACINE))
    if len(sys.argv) < 2 or sys.argv[1] not in ("screenshots", "map", "archive"):
        print(__doc__)
        return 1
    return {"screenshots": screenshots, "map": carte, "archive": archiver}[sys.argv[1]](sys.argv[2:])


if __name__ == "__main__":
    sys.exit(main())
