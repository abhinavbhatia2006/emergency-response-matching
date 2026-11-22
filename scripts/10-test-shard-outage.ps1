# Exercise the optional two-copy outage and confirm that other shards stay readable.
param([switch]$Pause)
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    Write-Host 'Checking that losing both copies affects only their shard, then restoring both copies.'
    Invoke-StorageTest -Scenario outage -Pause:$Pause
}
