# Old missions

*Français : [MISSIONS.fr.md](MISSIONS.fr.md)*

Before version 3.0, a mission was one sentence that Studio and Servers carried out together. Athanor no longer creates nor follows missions: a project now has its own deployment, set in its *Deployment* tab ([README](../README.md#a-projects-deployment), [DEPLOY.md](DEPLOY.md)). This page says what becomes of the missions an earlier version left.

## What replaced them

- **One sentence on the home page** still does the work: the home assistant proposes a **project** card (Studio alone, or with its deployment) or a **server** card, and nothing starts before *Launch*.
- **The project carries its deployment**: the server, the address, the APIs of the vault, your plan and when to deploy are in *Studio › Deployment*; the permission mode is in *Manage › This project's settings*.
- **No orchestrator** stands between you and the project any more: the project's pilot builds, Athanor prepares the server and deploys, under the same guardrails as before.

## The Old missions page

*Old missions* is in the menu only while one is left. It lists each mission with the state it was left in, its sentence and its date; a mission opens on its **journal**, read only (*See more* shows the older lines). A mission that was not finished says *No longer followed: its project continues on its own*: nothing is resumed, paused or retried from this page.

Nothing was lost in the update, and nothing is rewritten when it is read: the mission files stay as they were, and no password is asked again.

## Their projects

A project that a mission created or drove is a Studio project like any other: *Start* it, add requests to it, set its deployment.

- A project that was set up with several sites keeps them: its *Deployment* tab shows and changes one, and *Other sites of this project* lists the others by their address, each with *Make it the deployment*.
- A site that a mission put online says so until you make it the project's deployment. Until then nothing deploys there by itself.
- A production site of that time still asks you before each deployment (“Deploy example.com?”, in the bell, on *For you* and on your phone).

## Deleting a mission

*Delete the mission*, on the list (the cross) or on the mission's page, first shows what goes: the mission, its journal and its follow-up files. *Also remove the project “…” from Studio and send its folder to the recycle bin* is unticked by default, and greyed, with the reason, for a folder Athanor may not delete. The GitHub repository stays, and so does what the mission put online on the servers; no key, server or vault entry is removed.

## MCP clients

For a client connected to Athanor's MCP server ([MCP.md](MCP.md)), `missions` lists the old missions it may see and `proposer_mission` answers that missions are no longer created: use `ajouter_demande` to add a request to a project.

## Where they are kept

One file per mission in Athanor's folder, `hub/missions/<id>.json` (readable by your account only): the sentence, the card, the lanes, the history and the journal. Athanor only reads these files, and removes one when you delete its mission.
