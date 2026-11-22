# Follow a single request and its resource through the real deployed API.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    $url = Get-ComposeUrl
    Write-Host 'Checking the dashboard, request creation/read, resource reservation and status updates.'
    Invoke-Python @('-u', 'scripts/smoke.py', '--url', $url, '--count', '1', '--timeout', '180')
}
