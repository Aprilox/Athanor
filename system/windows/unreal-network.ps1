# =============================================================================
#  Makes Unreal's MCP reachable from WSL. Easiest: double-click unreal-network.cmd
#  1. updates WSL and retries "mirrored" networking (the most direct solution)
#  2. if it still fails: installs a local relay reserved for WSL (administrator rights, once)
# =============================================================================
$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}
$env:WSL_UTF8 = '1'
$Kit = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$Distro = ([IO.File]::ReadAllText((Join-Path $Kit 'system\distro.txt'))).Trim()
$cfg = Join-Path $env:USERPROFILE '.wslconfig'
$echec = Join-Path $Kit 'system\mirrored_unavailable.txt'
$relais = Join-Path $Kit 'system\unreal_relay.txt'
function Ok($t) { Write-Host "OK  $t" -ForegroundColor Green }
function Alerte($t) { Write-Host "!   $t" -ForegroundColor Yellow }
function Sortie($cmd) { return ((& wsl.exe -d $Distro -- sh -c $cmd 2>&1 | ForEach-Object { "$_" } | Out-String) -replace "`0", '') }
function Reseau { return ((Sortie 'getent hosts claude.ai >/dev/null && echo RESEAU_OK') -match 'RESEAU_OK') }
function Mirrored { return ((Sortie 'wslinfo --networking-mode 2>/dev/null') -match 'mirrored') }
# Lines of .wslconfig without networkingMode, without a broken "[" line; always an array.
function Lignes {
    if (-not (Test-Path -LiteralPath $cfg)) { return ,@() }
    $l = @([IO.File]::ReadAllLines($cfg) | Where-Object { $_ -notmatch '^\s*networkingMode\s*=' -and $_ -notmatch '^\s*\[?\s*$' -or $_ -match '^\s*$' })
    $l = @($l | Where-Object { $_ -notmatch '^\s*\[\s*$' })
    return ,$l
}
function Ecrire([string[]]$l) { [IO.File]::WriteAllText($cfg, ((@($l) -join "`r`n").Trim() + "`r`n"), (New-Object Text.UTF8Encoding $false)) }

Write-Host '== 1. WSL update' -ForegroundColor White
Start-Process wsl.exe -ArgumentList '--update' -Verb RunAs -Wait
& wsl.exe --version 2>&1 | ForEach-Object { "$_" -replace "`0", '' } | Select-Object -First 2

Write-Host ''; Write-Host '== 2. New attempt at "mirrored" networking' -ForegroundColor White
[string[]]$l = Lignes
$i = [array]::FindIndex($l, [Predicate[string]]{ param($x) $x -match '^\s*\[wsl2\]' })
$nouv = New-Object System.Collections.Generic.List[string]
if ($i -ge 0) { for ($k = 0; $k -lt $l.Count; $k++) { $nouv.Add($l[$k]); if ($k -eq $i) { $nouv.Add('networkingMode=mirrored') } } }
else { foreach ($x in $l) { $nouv.Add($x) }; $nouv.Add('[wsl2]'); $nouv.Add('networkingMode=mirrored') }
Ecrire $nouv.ToArray()
& wsl.exe --shutdown 2>&1 | Out-Null; Start-Sleep -Seconds 4
if ((Mirrored) -and (Reseau)) {
    Ok '"mirrored" networking active: WSL reaches Unreal directly on 127.0.0.1:8000.'
    Remove-Item -LiteralPath $echec -ErrorAction SilentlyContinue
    if (Test-Path -LiteralPath $relais) {   # relay no longer needed
        Start-Process powershell.exe -Verb RunAs -Wait -ArgumentList '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$PSScriptRoot\unreal-relay.ps1`"", '-Retirer'
        Remove-Item -LiteralPath $relais -ErrorAction SilentlyContinue
    }
    exit 0
}
Alerte '"mirrored" mode still fails: back to normal networking.'
Ecrire ([string[]](Lignes))
Set-Content -LiteralPath $echec -Value (Get-Date -Format 'yyyy-MM-dd HH:mm') -Encoding ASCII
& wsl.exe --shutdown 2>&1 | Out-Null; Start-Sleep -Seconds 4
if (-not (Reseau)) { Alerte "WSL has no Internet any more: check $cfg (no networkingMode line), then wsl --shutdown."; exit 1 }

Write-Host ''; Write-Host '== 3. Local relay reserved for WSL (administrator rights, once)' -ForegroundColor White
& wsl.exe -d $Distro -- true 2>&1 | Out-Null   # the WSL network adapter must exist
Start-Process powershell.exe -Verb RunAs -Wait -ArgumentList '-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$PSScriptRoot\unreal-relay.ps1`""
$ok = (& netsh.exe interface portproxy show v4tov4 2>&1 | Out-String) -match '8001'
if ($ok) {
    Set-Content -LiteralPath $relais -Value '8001' -Encoding ASCII
    Ok 'Relay installed: the pilot will use http://<Windows address as seen from WSL>:8001/mcp.'
} else { Alerte 'Relay not installed (administrator rights refused?).' }
