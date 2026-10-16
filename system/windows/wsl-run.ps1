# Runs a kit script in WSL (used by the .cmd files).
# Usage: wsl-run.ps1 SCRIPT.sh [arguments...]
$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}
$env:WSL_UTF8 = '1'
$Kit = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$f = Join-Path $Kit 'system\distro.txt'
if (-not (Test-Path -LiteralPath $f)) { Write-Host 'Run install.cmd first.' -ForegroundColor Yellow; exit 1 }
$Distro = ([IO.File]::ReadAllText($f)).Trim([char]0xFEFF, ' ', "`r", "`n", "`t")
if ($args.Count -lt 1) { Write-Host 'Usage: wsl-run.ps1 SCRIPT.sh'; exit 2 }
$script = $args[0]; $reste = @(); if ($args.Count -gt 1) { $reste = $args[1..($args.Count - 1)] }
& wsl.exe -d $Distro --cd $Kit -- bash "./commands/$script" @reste
$code = $LASTEXITCODE
if ($code -ne 0 -and $code -ne $null) {
    $liste = (& wsl.exe --list --quiet 2>&1 | Out-String) -replace "`0", ''
    if ($liste -notmatch [regex]::Escape($Distro)) { Write-Host "Distribution '$Distro' not found. Distributions present:`n$liste`nRun install.cmd again." -ForegroundColor Yellow }
}
exit $code
