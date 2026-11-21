# Credits

Third-party files shipped inside Athanor's interface.

| Files | Project | Version | Licence | Source |
|---|---|---|---|---|
| `app/static/xterm/xterm.js`, `app/static/xterm/xterm.css` | xterm.js | 6.0.0 (xterm.css: scrollbar fade shortened to 300 ms) | MIT (`app/static/xterm/LICENSE`) | npm `@xterm/xterm`, https://github.com/xtermjs/xterm.js |
| `app/static/xterm/addon-fit.js` | xterm.js fit add-on | 0.11.0 | MIT (same authors, same licence) | npm `@xterm/addon-fit` |

The files are copied as published, without their source maps; they load only on a server's terminal page and never from the network.

## Terminal colour themes

The terminal's Solarized, Nord, Dracula, Gruvbox and One Dark themes (`PALETTES_TERMINAL` in `app/static/app.js`) are written by hand after the colours each project publishes (Solarized by Ethan Schoonover, Nord by Arctic Ice Studio, Dracula by Zeno Rocha, Gruvbox by Pavel Pertsev, One Dark by the Atom team, all under the MIT licence). A few colours are lightened so that every one reads at 4.5:1 at least on its background (`TestPalettesTerminal`). The names are the projects' own; Athanor is not affiliated with them.
