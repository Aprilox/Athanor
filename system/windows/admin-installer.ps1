# =============================================================================
#  Athanor: Windows installations with administrator rights.
#  Copied to C:\ProgramData\Athanor\ (editable only by an administrator)
#  and run by the scheduled task "Athanor - installations".
#  It reads the requests dropped by Claude (one winget id per file), validates them,
#  installs with winget, and writes the result. It also allows in the Windows firewall (local network
#  only) Unreal and the project's programs, so that Windows never shows the "Allow access"
#  window (which requires administrator rights). It executes nothing else.
# =============================================================================
param([Parameter(Mandatory = $true)][string]$Dossier)
$ErrorActionPreference = 'Continue'
$journal = Join-Path $PSScriptRoot 'journal.txt'
function Noter($t) { Add-Content -LiteralPath $journal -Value ("{0}  {1}" -f (Get-Date -Format 'yyyy-MM-dd HH:mm:ss'), $t) -Encoding UTF8 }
if (-not (Test-Path -LiteralPath $Dossier)) { exit 0 }

# ---- Firewall: inbound connections allowed only from the local network (LocalSubnet).
$Kit = Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $Dossier))
$Groupe = 'Athanor'
# All the engine's programs: editor, compiler (UnrealBuildTool, its dotnet, UBA), tools.
$exes = @()
foreach ($m in @(Get-ChildItem -Path "$env:ProgramFiles\Epic Games" -Directory -Filter 'UE_*' -ErrorAction SilentlyContinue)) {
    $b = Join-Path $m.FullName 'Engine\Binaries'
    if (Test-Path -LiteralPath $b) { $exes += @(Get-ChildItem -LiteralPath $b -Recurse -Filter '*.exe' -File -ErrorAction SilentlyContinue | Where-Object { $_.FullName -notmatch '\\(Linux|Mac|IOS|Android)\\' }) }
}
$dossiers = @("$Kit\tools\windows")
# Projects known to the app (one path per line, written by Athanor.exe)
$liste = Join-Path $Kit 'logs\projects.txt'
if (Test-Path -LiteralPath $liste) { $dossiers += @(Get-Content -LiteralPath $liste | Where-Object { $_ -and (Test-Path -LiteralPath $_) } | ForEach-Object { Join-Path $_ 'deliverable' }) }
foreach ($d in $dossiers) {
    if (Test-Path -LiteralPath $d) { $exes += @(Get-ChildItem -LiteralPath $d -Recurse -Filter '*.exe' -File -ErrorAction SilentlyContinue) }
}
$deja = @{}
Get-NetFirewallRule -Group $Groupe -ErrorAction SilentlyContinue | Get-NetFirewallApplicationFilter -ErrorAction SilentlyContinue | ForEach-Object { $deja[$_.Program.ToLower()] = 1 }
foreach ($e in $exes) {
    $p = $e.FullName
    if ($deja.ContainsKey($p.ToLower())) { continue }
    foreach ($proto in @('TCP', 'UDP')) {
        New-NetFirewallRule -DisplayName "Athanor - $($e.Name) ($proto)" -Group $Groupe -Direction Inbound -Action Allow -Program $p -Protocol $proto -Profile Any -RemoteAddress LocalSubnet -ErrorAction SilentlyContinue | Out-Null
    }
    $deja[$p.ToLower()] = 1; Noter "firewall: $p allowed (local network only)"
}

# ---- Requests dropped by Claude (one file each):
#   WINGET_ID                       install a winget package (machine scope, silent)
#   run: C:\path\program.exe args   run a program elevated, WITHOUT any UAC prompt. Only programs inside a known
#                                   project folder or tools\windows are accepted (.exe .msi .bat .cmd .ps1).
$projets = @()
if (Test-Path -LiteralPath $liste) { $projets = @(Get-Content -LiteralPath $liste | Where-Object { $_ -and (Test-Path -LiteralPath $_) }) }
$racinesOk = @("$Kit\tools\windows") + $projets
function CheminAutorise($chemin) {
    try { $full = [IO.Path]::GetFullPath($chemin) } catch { return $false }
    foreach ($r in $racinesOk) { $rf = [IO.Path]::GetFullPath($r).TrimEnd('\') + '\'; if ($full.ToLower().StartsWith($rf.ToLower())) { return $true } }
    return $false
}
foreach ($f in Get-ChildItem -LiteralPath $Dossier -Filter 'demande-*.txt' -File) {
    $res = $f.FullName -replace 'demande-', 'resultat-'
    $texte = ([IO.File]::ReadAllText($f.FullName)).Trim()
    Remove-Item -LiteralPath $f.FullName -Force
    if ($texte -match '^run:\s*(.+)$') {
        # Elevated run: first token = program (quoted or not), the rest = arguments.
        $ligne = $Matches[1].Trim()
        if ($ligne -match '^"([^"]+)"\s*(.*)$') { $prog = $Matches[1]; $arguments = $Matches[2] } else { $parts = $ligne -split '\s+', 2; $prog = $parts[0]; $arguments = if ($parts.Count -gt 1) { $parts[1] } else { '' } }
        $arguments = "$arguments".Trim()
        if (-not (Test-Path -LiteralPath $prog -PathType Leaf) -or -not (CheminAutorise $prog)) {
            Noter "REFUSED run outside the allowed folders: $prog"; Set-Content -LiteralPath $res -Value "code=2`nrefused: only programs inside a project or tools\windows can run elevated" -Encoding UTF8; continue
        }
        $ext = [IO.Path]::GetExtension($prog).ToLower()
        if ($ext -notin @('.exe', '.msi', '.bat', '.cmd', '.ps1')) {
            Noter "REFUSED extension $ext : $prog"; Set-Content -LiteralPath $res -Value "code=2`nrefused: only .exe .msi .bat .cmd .ps1" -Encoding UTF8; continue
        }
        Noter "elevated run: $prog $arguments"
        $sortieF = [IO.Path]::GetTempFileName()
        try {
            # Start-Process refuses an empty -ArgumentList, so it is only added when there is something to pass.
            $sp = @{ Wait = $true; PassThru = $true; WindowStyle = 'Hidden'; RedirectStandardOutput = $sortieF }
            switch ($ext) {
                '.msi' { $sp.FilePath = 'msiexec.exe'; $sp.ArgumentList = "/i `"$prog`" /qn /norestart $arguments".Trim() }
                '.ps1' { $sp.FilePath = 'powershell.exe'; $sp.ArgumentList = "-NoProfile -ExecutionPolicy Bypass -File `"$prog`" $arguments".Trim() }
                { $_ -in '.bat', '.cmd' } { $sp.FilePath = 'cmd.exe'; $sp.ArgumentList = "/c `"`"$prog`" $arguments`"" }
                default { $sp.FilePath = $prog; if ($arguments) { $sp.ArgumentList = $arguments } }
            }
            $p = Start-Process @sp
            $code = $p.ExitCode
        } catch { $code = 125; Set-Content -LiteralPath $sortieF -Value $_.Exception.Message }
        $fin = (Get-Content -LiteralPath $sortieF -ErrorAction SilentlyContinue | Select-Object -Last 30) -join "`n"
        Remove-Item -LiteralPath $sortieF -Force -ErrorAction SilentlyContinue
        Noter "  -> code $code"
        Set-Content -LiteralPath $res -Value "code=$code`n$fin" -Encoding UTF8
        continue
    }
    $id = $texte
    if ($id -notmatch '^[A-Za-z0-9._+-]{2,100}$') {
        Noter "REFUSED invalid identifier: $id"; Set-Content -LiteralPath $res -Value "code=2`ninvalid identifier" -Encoding UTF8; continue
    }
    Noter "installing $id"
    $sortie = & winget install --id $id --exact --source winget --silent --scope machine --accept-package-agreements --accept-source-agreements --disable-interactivity 2>&1 | Out-String
    $code = $LASTEXITCODE
    if ($code -ne 0 -and $sortie -match 'scope|portée') {
        $sortie += & winget install --id $id --exact --source winget --silent --accept-package-agreements --accept-source-agreements --disable-interactivity 2>&1 | Out-String
        $code = $LASTEXITCODE
    }
    if ($sortie -match 'already installed|déjà installé|No newer package|Aucune mise à niveau') { $code = 0 }
    Noter "  -> code $code"
    $fin = ($sortie -split "`r?`n" | Where-Object { $_ -match '\S' -and $_ -notmatch '^[\s\-\\|/█▒]+$' } | Select-Object -Last 15) -join "`n"
    Set-Content -LiteralPath $res -Value "code=$code`n$fin" -Encoding UTF8
}
