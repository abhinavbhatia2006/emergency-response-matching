# Write during an outage and verify that both copies agree after recovery.
param([switch]$Pause)
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    Write-Host 'Updating a record while the primary is down and comparing the recovered versions.'
    Invoke-StorageTest -Scenario recovery -Pause:$Pause
}
