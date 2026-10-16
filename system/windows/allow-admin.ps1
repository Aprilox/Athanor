# =============================================================================
#  Allows (or removes) Claude's Windows installations with administrator rights.
#  Run by install.ps1 with administrator rights (a single Windows prompt).
#  -Retirer: deletes the task, the firewall rules and the folder C:\ProgramData\Athanor.
# =============================================================================
param([string]$Kit, [string]$Utilisateur, [switch]$Retirer)
$ErrorActionPreference = 'Stop'
$Tache = 'Athanor - installations'
$Dest = Join-Path $env:ProgramData 'Athanor'
# Previous name of the tool (Claude Autopilote): its task, folder and firewall rules are removed in both modes.
Unregister-ScheduledTask -TaskName 'Claude Autopilote - installations' -Confirm:$false -ErrorAction SilentlyContinue
Remove-Item -LiteralPath (Join-Path $env:ProgramData 'ClaudeAutopilote') -Recurse -Force -ErrorAction SilentlyContinue
Remove-NetFirewallRule -Group 'Claude Autopilote' -ErrorAction SilentlyContinue
if ($Retirer) {
    Remove-NetFirewallRule -Group 'Athanor' -ErrorAction SilentlyContinue
    Unregister-ScheduledTask -TaskName $Tache -Confirm:$false -ErrorAction SilentlyContinue
    Remove-Item -LiteralPath $Dest -Recurse -Force -ErrorAction SilentlyContinue
    Write-Host 'Administrator installations removed.'; exit 0
}
if (-not $Kit) { $Kit = Split-Path -Parent (Split-Path -Parent $PSScriptRoot) }
if (-not $Utilisateur) { $Utilisateur = "$env:USERDOMAIN\$env:USERNAME" }
$Kit = (Resolve-Path -LiteralPath $Kit).Path
$File = Join-Path $Kit 'tools\windows\admin-requests'
New-Item -ItemType Directory -Force -Path $Dest, $File | Out-Null
Copy-Item -LiteralPath (Join-Path $Kit 'system\windows\admin-installer.ps1') -Destination (Join-Path $Dest 'admin-installer.ps1') -Force
# Folder editable only by administrators and SYSTEM; users can only read.
& icacls.exe $Dest /inheritance:r /grant:r '*S-1-5-32-544:(OI)(CI)F' '*S-1-5-18:(OI)(CI)F' '*S-1-5-32-545:(OI)(CI)RX' | Out-Null
$action = New-ScheduledTaskAction -Execute 'powershell.exe' -Argument ("-NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File `"{0}`" -Dossier `"{1}`"" -f (Join-Path $Dest 'admin-installer.ps1'), $File)
$principal = New-ScheduledTaskPrincipal -UserId $Utilisateur -LogonType Interactive -RunLevel Highest
$reglages = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -ExecutionTimeLimit (New-TimeSpan -Hours 2) -MultipleInstances Queue
Register-ScheduledTask -TaskName $Tache -Action $action -Principal $principal -Settings $reglages -Force | Out-Null
# The user (without elevation) can run the task, not modify it.
$sid = (New-Object Security.Principal.NTAccount($Utilisateur)).Translate([Security.Principal.SecurityIdentifier]).Value
$service = New-Object -ComObject 'Schedule.Service'; $service.Connect()
$service.GetFolder('\').GetTask($Tache).SetSecurityDescriptor("D:(A;;FA;;;BA)(A;;FA;;;SY)(A;;GRGX;;;$sid)", 0)
# First firewall update right away (Unreal and project programs, local network only).
& (Join-Path $Dest 'admin-installer.ps1') -Dossier $File
Write-Host "Task '$Tache' created: Claude can install winget software with administrator rights,"
Write-Host "and Windows will no longer ask for network access for Unreal or the game (local network only)."
