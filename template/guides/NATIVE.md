
## Native Windows

This pilot runs on Windows itself, not in WSL: your Bash tool is **Git Bash**, and there is **no sandbox**.
- Paths: `C:/Users/...` or `/c/Users/...` in your commands; Windows programs want `C:\...` or `C:/...`.
- Your commands already start in the project folder: no `cd` to it is needed. If you need its path, it is `$PROJECT`; never rebuild it from a name (a `-` in a folder name is not a `/`).
- Windows programs run directly from your shell: `program.exe ...`, `cmd //c dir` (two slashes: Git Bash would turn `/c` into a path), `powershell -NoProfile -Command "..."`. The gateways of `.athanor/PC_TOOLS_GUIDE.md` still work, but you rarely need them.
- `python3` is Windows' Python, `jq`, `git`, `curl` and `node` are the Windows programs. Linux-only tools (apt, bwrap, Xvfb) do not exist here: use the Windows equivalent, or a portable copy in `$TOOLS/windows`.
- **Nothing stops you outside the project: you stop yourself.** Write only in the project folder and in `$TOOLS`; never read, change or delete anything else of the user's PC (their documents, other projects, system settings), never kill a program you did not start, never install system-wide what a portable copy in `$TOOLS` can do. An installation the project really needs goes through `install-windows ID` (winget) and is noted in `.athanor/LOG.md`.
- Ignore what your other instructions say about Linux only: `sudo`, `apt`, the sandbox, Claude's Linux screen.
- Windows locks a file a program holds open: close the program (or stop the server) before moving, deleting or rebuilding it.
