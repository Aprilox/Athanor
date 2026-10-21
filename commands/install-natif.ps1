# =============================================================================
#  Athanor, native Windows mode: installs what the pilot needs (no WSL).
#  1. with winget: Git for Windows (Git Bash), jq, Python, Node.js, GitHub CLI
#  2. Claude Code for Windows (the official installer, in %USERPROFILE%\.local\bin)
#  Opened by the app: Setup & health -> Install everything (native Windows mode).
# =============================================================================
$ErrorActionPreference = 'Continue'
try { [Console]::OutputEncoding = [Text.Encoding]::UTF8 } catch {}
$Kit = Split-Path -Parent $PSScriptRoot
New-Item -ItemType Directory -Force -Path (Join-Path $Kit 'logs') | Out-Null
try { Start-Transcript -Path (Join-Path $Kit 'logs\install-natif.log') -Append | Out-Null } catch {}

if (-not (Get-Command winget -ErrorAction SilentlyContinue)) {
  Write-Host 'winget is missing: install "App Installer" from the Microsoft Store, then run this again.' -ForegroundColor Yellow
  exit 1
}
function Paquet([string]$Id, [string]$Nom) {
  $present = winget list --id $Id -e --accept-source-agreements 2>$null | Select-String -SimpleMatch $Id
  if ($present) { Write-Host "ok  $Nom" -ForegroundColor Green; return }
  Write-Host "Installing $Nom..." -ForegroundColor Cyan
  winget install --id $Id -e --silent --accept-source-agreements --accept-package-agreements
  if ($LASTEXITCODE -ne 0) { Write-Host "  $Nom could not be installed (code $LASTEXITCODE)." -ForegroundColor Yellow }
}
Paquet 'Git.Git' 'Git for Windows (Git Bash)'
Paquet 'jqlang.jq' 'jq'
Paquet 'Python.Python.3.12' 'Python'
Paquet 'OpenJS.NodeJS.LTS' 'Node.js'
Paquet 'GitHub.cli' 'GitHub CLI (to link a project to GitHub)'

$claude = Join-Path $env:USERPROFILE '.local\bin\claude.exe'
if (Test-Path $claude) {
  # already there: brought up to date (an old version does not know the latest models)
  Write-Host 'Updating Claude Code for Windows...' -ForegroundColor Cyan
  & $claude update
  Write-Host ('ok  Claude Code for Windows ' + (& $claude --version)) -ForegroundColor Green
} else {
  Write-Host 'Installing Claude Code for Windows...' -ForegroundColor Cyan
  try { Invoke-RestMethod https://claude.ai/install.ps1 | Invoke-Expression } catch { Write-Host "  Claude Code could not be installed: $_" -ForegroundColor Yellow }
}

Write-Host ''
Write-Host 'Done. Close this window, then in Athanor: Setup & health -> Check again.' -ForegroundColor Green
Write-Host '(A program just installed is found once Athanor is restarted, if a check still says it is missing.)'
try { Stop-Transcript | Out-Null } catch {}
