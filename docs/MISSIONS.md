# Missions

*Français : [MISSIONS.fr.md](MISSIONS.fr.md)*

A mission links what Athanor manages: a **Studio project**, its **servers** and **API keys** of the vault, with one permission choice for the servers; or **servers only**, without a project. The [README](../README.md#missions) describes the form and the frame; this page says what a mission is made of and what becomes of the missions of earlier versions.

## What a mission is

- **One form.** *New mission* (on the home page or in *Missions*), or the home assistant, which answers with the mission's summary and *Review and launch*, opening the same form already filled from your sentence. What to do, the project (a new name creates it in Studio, the name of one of yours adds the mission to it as its next request), the server and its choice (*Needs me* = the project's *Manual* mode, *Autonomous with the AI* = its *Auto* mode, *Full autonomy* = its *Ignore permissions* mode, confirmed once with Athanor's password when it is chosen), the API key. A plan, how and when to put it online, the folder, working modes, GitHub and the project's own settings are folded under *More options*. *Launch* is one click.
- **Servers and their roles.** With a project, the site goes online on the servers marked so (several: copies of the same site); the others (a database, a worker…) get the role the servers' plan gives them, are prepared with it and never deployed to. Without a project, the mission is one Servers task on all of them and the keys: its frame follows that task, *Launch again* starts it again with the mission as it is now.
- **Two plans, both with *Improve with Claude*.** The project's plan (its steps, for the architect) and the plan for the servers (what each one does, how they are linked, the web server: nginx named there is the one used, Caddy is never installed against it).
- **Nothing new underneath.** Launching writes what you would set by hand: the project in Studio, its *Deployment* tab (server, key, address, plan, moment), its permission mode, then starts its pilot. Change them on the project's tabs and the mission's frame shows the change; change them in the mission (*Edit*) and the project's tabs show it.
- **Its state is read, never copied.** The frame reads the project (in progress, paused, stopped, finished, its milestones) and its deployment (being prepared, going online, online, failed and why) each time it is drawn.
- **Stop, resume, edit, delete in any state.** *Pause* and *Stop* are the project's own stops (a stop also cancels its server tasks); *Resume* starts its pilot again; *Edit* changes the server, the key and the choice, and a changed text becomes a new request of the project. *Delete* stops a mission that works first, then removes the mission; its project and folder go only when you tick it, and never a key, a server or a vault entry.
- **The server from the frame.** *Terminal* opens the server's terminal; *Stop the server's work* cancels the preparation or the first deployment in progress; *Launch the server's work again* makes the first deployment, or deploys the project's last commit again.

## Missions of earlier versions

Missions made by Athanor 3.0.0 and earlier show in *Missions* as read-only frames (*No longer followed*): a mission opens on its **journal**, and *Delete* removes it (its project and folder only when ticked). Nothing was lost in the update and nothing is rewritten when they are read; their projects keep working on their own.

## MCP clients

For a client connected to Athanor's MCP server ([MCP.md](MCP.md)), `missions` lists the missions it may see and `proposer_mission` answers that only the owner launches a mission, in Athanor's window: use `ajouter_demande` to add a request to a project.

## Where they are kept

One file per mission in Athanor's folder, `hub/missions/<id>.json` (readable by your account only): its text (a vault value written in it is masked), its project, server and key by id, its choice and its history. Athanor removes it when you delete its mission.
