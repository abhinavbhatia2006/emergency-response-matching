# Run the backend test files in every Go package. Verbose output shows skips.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    Write-Host 'Checking Go code and running unit and integration tests.'
    Invoke-Checked { go vet ./... }
    Invoke-Checked { go test -count=1 -v ./... }
    Write-Host 'PASS: Go checks finished. Review any service-dependent SKIP lines.'
}
