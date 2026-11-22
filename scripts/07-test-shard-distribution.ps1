# Check that each record is on both copies of its assigned shard only.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    Write-Host 'Comparing the hash of each request key with records on all six storage nodes.'
    Invoke-StorageTest -Scenario distribution
}
