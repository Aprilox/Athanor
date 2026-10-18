# Guide to the PC's tools (Windows, Unreal, Android)

Read only when the project needs Windows software, Unreal Engine or a mobile platform. The list of software detected on this PC and their commands is in `.athanor/PC_TOOLS.md`.

## Installing Windows software

- **Windows software** (SDKs, engines, editors the project needs and that are missing): you install them yourself, without waiting for anyone, in this order:
  1. **portable**, in `$TOOLS/windows/` (official zip archive, command-line tools): nothing is written anywhere else on the PC; launch them with `pc-exec PATH.exe ...`; if a Windows tool needs an environment variable (ANDROID_HOME, JAVA_HOME...), use `windows-var NAME PATH`;
  2. otherwise with **`install-windows ID`** (winget, no window; `install-windows --search NAME` to find the ID). It first tries for the user, then with administrator rights through the authorised task: no UAC prompt, nothing else to do. A downloaded installer (`setup.exe`, `.msi`) runs elevated the same way: `install-windows --file setup.exe /S` (pass its silent flags);
  3. a program that needs administrator rights (driver, service, installer): `pc-exec --admin program.exe ...`. If a program refuses to start without elevation, `pc-exec` retries it elevated by itself. Never expect anyone to click a Windows prompt: if a command says administrator rights are "not allowed yet", note it in `TODO_FOR_YOU.md` and continue with a portable alternative;
  3. if the installation still fails twice: **do not block**. Write in `TODO_FOR_YOU.md` (at the project root) what the user must install and why, in one or two lines, then continue with an alternative or with another part of the milestone.

## Recording installations

- **Record every installation in `$TOOLS/INSTALLS.md`**: name, version, location, what it is for, how to reinstall it (one command). Do the same for what you install inside the project (dependencies, engines, downloaded assets), section "In the project".
- Already installed: git, Python 3 (Pillow), Node.js and npm, ImageMagick, ffmpeg, jq, ripgrep, curl, zip.

## PC tools

- **PC tools**: the list of those available and how to use them is in `.athanor/PC_TOOLS.md` (Unreal Engine, Blender, Docker...). These are the user's real Windows programs, with their graphics card: always go through the commands indicated (`unreal-editor`, `unreal-cmd`, `unreal-uat`, `blender`, `tool`), never through `cmd.exe` or `powershell.exe` (blocked). Their scripts (Blender or Unreal Python) must also write only inside the project.

## Unreal Engine

- **Unreal Engine**: when the Unreal MCP is active, the pilot opens the editor on the project's `.uproject` before each cycle and you have its tools (`mcp__unreal__...`). Create the Unreal project in this folder (for example `Game/Game.uproject`), from one of the engine's templates, and enable in the `.uproject` the required plugins, including the MCP one (its exact name is in the engine's `Engine/Plugins` folder, which you can read): the pilot will open it at the next cycle and start its MCP server by itself. The Unreal, Blender and Android SDK folders are readable (not writable): use them for templates, documentation and examples. Build the Windows game with `unreal-uat BuildCookRun ...` into `deliverable/`.
- **Roblox Studio**: when its MCP server is switched on for the project, you have Studio's tools (`mcp__roblox-studio__...`: scripts, instances, play tests, assets). They act on the open Roblox Studio: before each cycle, the pilot opens the project's first `.rbxl`/`.rbxlx` place, unless a Studio is already open (the user's own included: then you work in it). `roblox-studio [--wait] PLACE.rbxl` opens it yourself. Call `list_roblox_studios` first to get the `studio_id` of the right place; Studio can take a minute to start, so if none is listed, wait 30 s and try again once. Still none (Studio closed, its MCP server off, or no place in the project): ask the user with a task (`athanor todo add`), and go on with the milestones that do not need it.

## Android then iOS

- **Mobile game with Unreal**: Android first, **then iOS from the same project**. For Android you need, on Windows, the Android SDK (cmdline-tools, platform-tools, build-tools, platforms, NDK at the versions required by the documentation of the Unreal version) and a JDK: install them portable in `$TOOLS/windows/android-sdk` and `$TOOLS/windows/jdk-...` with `pc-exec .../sdkmanager.bat`, declare them in the project's `Config/DefaultEngine.ini` (section `[/Script/AndroidPlatformEditor.AndroidSDKSettings]`: SDKPath, NDKPath, JavaPath) and with `windows-var`. Build the `.apk` with `unreal-uat BuildCookRun ... -platform=Android -cookflavor=ASTC` and install it on the connected phone with `adb install -r`. To stay **portable to iOS**: no Android-specific code or plugin (go through Unreal's cross-platform functions: purchases, achievements, saves), an interface that respects safe areas and several screen formats, standard Metal-compatible mobile rendering, and keep `docs/IOS_PORT.md` up to date (settings, icons, privacy texts, what will have to be done on a Mac).
