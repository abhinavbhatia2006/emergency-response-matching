# Check that the shard stays available when just its primary is stopped.
param([switch]$Pause)
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    Write-Host 'Stopping the shard 0 primary, then verifying reads and writes through the replica.'
    Invoke-StorageTest -Scenario failover -Pause:$Pause
}
