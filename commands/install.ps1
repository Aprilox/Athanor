# =============================================================================
#  Athanor (WSL2): Windows installation, to run once.
#  Easiest: double-click install.cmd
#  1. installs or checks WSL2 (the Linux built into Windows)
#  2. sets WSL networking to "mirrored" to reach Unreal (MCP) on this PC
#  3. runs install.sh in WSL (Claude Code, sandbox, tools)
#  4. offers administrator rights for Claude's software installations
# =============================================================================
$ErrorActionPreference = 'Stop'
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}
$env:WSL_UTF8 = '1'          # wsl.exe output in UTF-8 (else UTF-16)
$Kit = Split-Path -Parent $PSScriptRoot
Set-Location -LiteralPath $Kit
New-Item -ItemType Directory -Force -Path (Join-Path $Kit 'logs') | Out-Null
try { Start-Transcript -Path (Join-Path $Kit 'logs\install-windows.log') -Append | Out-Null } catch {}

function Titre($t) { Write-Host ''; Write-Host "== $t" -ForegroundColor White }
function Ok($t) { Write-Host "OK  $t" -ForegroundColor Green }
function Alerte($t) { Write-Host "!   $t" -ForegroundColor Yellow }
function OuiNon($q, [bool]$defOui) {
    $inv = if ($defOui) { '[Y/n]' } else { '[y/N]' }
    $r = Read-Host "$q $inv"; if ($null -eq $r) { $r = '' }
    if ($r.Trim() -eq '') { return $defOui }; return ($r -match '^[oOyY]')
}
function Wsl([string[]]$a) {
    # wsl.exe error messages must not stop the script (Windows PowerShell 5.1).
    $ErrorActionPreference = 'Continue'
    $s = & wsl.exe @a 2>&1 | ForEach-Object { "$_" } | Out-String
    return ($s -replace "`0", '')
}
function WslArret { $ErrorActionPreference = 'Continue'; & wsl.exe --shutdown 2>&1 | Out-Null; Start-Sleep -Seconds 4 }

# ----------------------------------------------------------------------------- 1. WSL2
Titre '1. WSL2'
if (-not (Get-Command wsl.exe -ErrorAction SilentlyContinue)) { Alerte 'wsl.exe not found: Windows 10 (2004+) or 11 required.'; exit 1 }
$liste = Wsl @('--list', '--verbose')
$lignes = @($liste -split "`r?`n" | Where-Object { $_ -match '\S' } | Select-Object -Skip 1)
$distros = @()
foreach ($l in $lignes) {
    if ($l -match '^\s*(\*?)\s*(\S+)\s+(\S+)\s+(\d)\s*$') {
        $distros += [pscustomobject]@{ Nom = $Matches[2]; Defaut = ($Matches[1] -eq '*'); Version = [int]$Matches[4] }
    }
}
$distros = @($distros | Where-Object { $_.Nom -notmatch '^docker-desktop' })
if ($distros.Count -eq 0) {
    Alerte "No Linux distribution in WSL."
    if (OuiNon 'Install WSL2 with Ubuntu now? (administrator rights, then PC restart)' $true) {
        Start-Process wsl.exe -ArgumentList '--install', '-d', 'Ubuntu' -Verb RunAs -Wait
        Write-Host ''
        Write-Host "Then: 1) restart the PC  2) open 'Ubuntu' in the Start menu and create your Linux user  3) run install.cmd again" -ForegroundColor Cyan
    }
    exit 0
}
$choix = $distros | Where-Object { $_.Nom -match '^Ubuntu' } | Select-Object -First 1
if (-not $choix) { $choix = $distros | Where-Object { $_.Defaut } | Select-Object -First 1 }
if (-not $choix) { $choix = $distros[0] }
if ($distros.Count -gt 1) {
    Write-Host 'Distributions found:'
    for ($i = 0; $i -lt $distros.Count; $i++) { Write-Host ("  {0}) {1} (WSL{2})" -f ($i + 1), $distros[$i].Nom, $distros[$i].Version) }
    $r = Read-Host "Which one to use? [$($choix.Nom)]"
    $n = 0; if ([int]::TryParse("$r", [ref]$n) -and $n -ge 1 -and $n -le $distros.Count) { $choix = $distros[$n - 1] }
}
$Distro = $choix.Nom
if ($choix.Version -ne 2) {
    Alerte "$Distro runs on WSL1: the sandbox requires WSL2."
    if (OuiNon "Convert $Distro to WSL2? (a few minutes)" $true) { & wsl.exe --set-version $Distro 2 } else { exit 1 }
}
[IO.File]::WriteAllText((Join-Path $Kit 'system\distro.txt'), $Distro, (New-Object Text.ASCIIEncoding))
Ok "Distribution: $Distro (WSL2)"

# ----------------------------------------------------------------------------- 2. mirrored networking
Titre '2. WSL networking (to drive Unreal through its MCP)'
$cfg = Join-Path $env:USERPROFILE '.wslconfig'
$echecMirrored = Join-Path $Kit 'system\mirrored_unavailable.txt'
$texte = if (Test-Path -LiteralPath $cfg) { [IO.File]::ReadAllText($cfg) } else { '' }
$build = [Environment]::OSVersion.Version.Build
if ($texte -match '(?im)^\s*networkingMode\s*=\s*mirrored') {
    Ok 'Networking already in "mirrored" mode'
    $essai = Wsl @('-d', $Distro, '--', 'sh', '-c', 'getent hosts claude.ai >/dev/null && echo RESEAU_OK')
    if ($essai -notmatch 'RESEAU_OK') {
        Alerte '"mirrored" mode is set but WSL has no network: back to normal networking.'
        $l = @([IO.File]::ReadAllLines($cfg) | Where-Object { $_ -notmatch '^\s*networkingMode\s*=' })
        [IO.File]::WriteAllText($cfg, (($l -join "`r`n") + "`r`n"), (New-Object Text.UTF8Encoding $false))
        Set-Content -LiteralPath $echecMirrored -Value (Get-Date -Format 'yyyy-MM-dd HH:mm') -Encoding ASCII
        WslArret
    }
} elseif (Test-Path -LiteralPath $echecMirrored) {
    Alerte '"mirrored" mode already tried without success on this PC: normal networking kept (Unreal MCP not directly reachable).'
} elseif ($build -lt 22621) {
    Alerte "Windows 11 22H2 or newer is required for 'mirrored' mode (current build $build). The Unreal MCP will not be reachable from WSL."
} elseif (OuiNon "Switch WSL networking to 'mirrored' mode? (WSL sees this PC's programs on 127.0.0.1; modifies $cfg)" $true) {
    $sauve = $false
    if ($texte -ne '') { Copy-Item -LiteralPath $cfg -Destination "$cfg.sauvegarde" -Force; $sauve = $true }
    if ($texte -match '(?im)^\s*\[wsl2\]') {
        $texte = [regex]::Replace($texte, '(?im)^(\s*\[wsl2\][^\r\n]*\r?\n)', "`$1networkingMode=mirrored`r`n", 1)
    } else {
        $texte = $texte.TrimEnd() + "`r`n[wsl2]`r`nnetworkingMode=mirrored`r`n"
    }
    [IO.File]::WriteAllText($cfg, $texte.TrimStart(), (New-Object Text.UTF8Encoding $false))
    if ($sauve) { Ok "Setting written (previous version: $cfg.sauvegarde)" } else { Ok 'Setting written' }
    if (OuiNon 'Restart WSL now to apply it? (Docker Desktop will restart too)' $true) {
        WslArret
        $essai = Wsl @('-d', $Distro, '--', 'sh', '-c', 'getent hosts claude.ai >/dev/null && wslinfo --networking-mode 2>/dev/null')
        if ($essai -match 'mirrored') { Ok 'WSL restarted, "mirrored" networking active' }
        else {
            Alerte "'mirrored' mode does not work on this PC (WSL ends up without network). Back to normal networking."
            $lignes2 = @([IO.File]::ReadAllLines($cfg) | Where-Object { $_ -notmatch '^\s*networkingMode\s*=' })
            [IO.File]::WriteAllText($cfg, (($lignes2 -join "`r`n") + "`r`n"), (New-Object Text.UTF8Encoding $false))
            Set-Content -LiteralPath $echecMirrored -Value (Get-Date -Format 'yyyy-MM-dd HH:mm') -Encoding ASCII
            WslArret
            Alerte 'Unreal (MCP) will not be directly reachable from WSL; everything else works.'
        }
    }
    else { Alerte 'The setting will take effect at the next WSL restart (wsl --shutdown).' }
}

# Check: WSL must have Internet access for what follows.
$net = Wsl @('-d', $Distro, '--', 'sh', '-c', 'getent hosts claude.ai >/dev/null && echo RESEAU_OK')
if ($net -notmatch 'RESEAU_OK') {
    Alerte "WSL has no Internet access. Check $cfg (remove the networkingMode line), then: wsl --shutdown, and run install.cmd again."
    exit 1
}
Ok 'WSL has Internet access'

# ----------------------------------------------------------------------------- 3. install.sh
Titre "3. Installation in $Distro (your Linux password will be asked)"
$ErrorActionPreference = 'Continue'
& wsl.exe -d $Distro --cd $Kit -- bash ./commands/install.sh
Write-Host "(details of the Linux installation: logs\install-wsl.log)"
if ($LASTEXITCODE -ne 0) { Alerte "install.sh reported a problem (code $LASTEXITCODE)." }

# ----------------------------------------------------------------------------- 4. administrator rights
Titre '4. Windows software installations by Claude'
Write-Host 'Claude installs what it needs on its own (SDKs, engines...). Some software requires administrator rights.'
Write-Host 'If you accept, Windows asks for your consent ONCE; afterwards Claude installs without bothering you,'
Write-Host 'only software from the official winget catalogue, each one recorded in tools\INSTALLS.md.' -ForegroundColor DarkGray
Write-Host 'The same authorisation configures the firewall (Unreal and the game, local network only): no more "Allow access" window.' -ForegroundColor DarkGray
if (OuiNon 'Allow Claude to install software with administrator rights?' $true) {
    $arguments = @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$(Join-Path $Kit 'system\windows\allow-admin.ps1')`"", '-Kit', "`"$Kit`"", '-Utilisateur', "`"$env:USERDOMAIN\$env:USERNAME`"")
    try {
        Start-Process powershell.exe -ArgumentList $arguments -Verb RunAs -Wait
        & schtasks.exe /query /tn 'Athanor - installations' 2>$null | Out-Null
        if ($LASTEXITCODE -eq 0) { Ok 'Allowed (to remove: run install.cmd again and answer no, or delete the task "Athanor - installations")' } else { Alerte 'Authorisation not created.' }
    } catch { Alerte "Authorisation refused: Claude will only install what does not require administrator rights." }
} else {
    if ((& schtasks.exe /query /tn 'Athanor - installations' 2>$null) -and $LASTEXITCODE -eq 0) {
        if (OuiNon 'An authorisation already exists. Remove it?' $false) {
            Start-Process powershell.exe -ArgumentList @('-NoProfile', '-ExecutionPolicy', 'Bypass', '-File', "`"$(Join-Path $Kit 'system\windows\allow-admin.ps1')`"", '-Retirer') -Verb RunAs -Wait
        }
    }
}

# ----------------------------------------------------------------------------- 5. next
# Starting with Windows and relaunching the pilots are set in the app (Options): a project
# is a user folder, the tool contains none. Old startup task: removed.
& schtasks.exe /query /tn 'Athanor WSL' 2>$null | Out-Null
if ($LASTEXITCODE -eq 0) { & schtasks.exe /Delete /F /TN 'Athanor WSL' | Out-Null; Ok "Old task 'Athanor WSL' removed (replaced by the app option)." }

Write-Host ''
Write-Host 'Installation complete. Go back to the Athanor app:' -ForegroundColor Cyan
Write-Host '  1. Installation & health -> Check everything (and the token: Repair on the Token line)'
Write-Host '  2. Test the sandbox'
Write-Host '  3. + New project, then Start'
Write-Host '  (Without the app: commands\configure.cmd, then commands\start.cmd D:\path\to\project)'

