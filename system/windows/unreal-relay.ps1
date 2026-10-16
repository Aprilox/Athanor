# =============================================================================
#  Local relay to Unreal's MCP, when WSL's "mirrored" networking does not work.
#  To run with administrator rights (unreal-network.cmd takes care of it).
#  Windows listens on port 8001 and forwards to Unreal (127.0.0.1:8000). The firewall
#  accepts this port only from WSL's virtual network adapter: nothing is open to the local network.
#  -Retirer: removes the relay and the rule.
# =============================================================================
param([int]$PortRelais = 8001, [int]$PortUnreal = 8000, [switch]$Retirer)
$ErrorActionPreference = 'Continue'
$Regle = 'Athanor - Unreal MCP relay (WSL only)'
& netsh.exe interface portproxy delete v4tov4 listenaddress=0.0.0.0 listenport=$PortRelais 2>&1 | Out-Null
Get-NetFirewallRule -DisplayName $Regle -ErrorAction SilentlyContinue | Remove-NetFirewallRule
# rule of the previous name of the tool (Claude Autopilote)
Get-NetFirewallRule -DisplayName 'Claude Autopilote - relais MCP Unreal (WSL uniquement)' -ErrorAction SilentlyContinue | Remove-NetFirewallRule
if ($Retirer) { Write-Host 'Relay removed.'; exit 0 }
$carte = Get-NetAdapter -IncludeHidden -ErrorAction SilentlyContinue | Where-Object { $_.Name -like 'vEthernet (WSL*' } | Select-Object -First 1
if (-not $carte) { Write-Host "WSL network adapter not found: run WSL once (wsl), then try again." -ForegroundColor Yellow; exit 1 }
& netsh.exe interface portproxy add v4tov4 listenaddress=0.0.0.0 listenport=$PortRelais connectaddress=127.0.0.1 connectport=$PortUnreal | Out-Null
New-NetFirewallRule -DisplayName $Regle -Direction Inbound -Action Allow -Protocol TCP -LocalPort $PortRelais `
    -InterfaceAlias $carte.Name -RemoteAddress LocalSubnet -Profile Any | Out-Null
Write-Host "Relay active: port $PortRelais (adapter '$($carte.Name)' only) -> Unreal 127.0.0.1:$PortUnreal"
