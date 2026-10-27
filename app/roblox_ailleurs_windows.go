package main

// Windows: Roblox Studio is Windows' own (outil_passerelle.go); what roblox_ailleurs.go does
// elsewhere never runs here.

func (p *passerelle) robloxMCP(args []string) int {
	_, _ = p.erreur.Write([]byte("Roblox Studio MCP: on Windows the catalog's configuration runs mcp.bat itself\n"))
	return 2
}

func (p *passerelle) robloxStudioHorsWindows(sys string, args []string) int {
	return p.robloxStudio(args)
}

func studioRobloxOuvert() bool { return processusTourne("RobloxStudioBeta.exe") }

type vinegarTrouve struct{ donnees string }

func trouverVinegar() (vinegarTrouve, bool) { return vinegarTrouve{}, false }
