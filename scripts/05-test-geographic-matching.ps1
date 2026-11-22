# Check nearest-resource selection and rejection outside the matching radius.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    $url = Get-ComposeUrl
    Test-ApiReady $url
    Write-Host 'Checking a nearby cross-label match, saved distance and out-of-range rejection.'
    Invoke-Python @('-u', 'scripts/geographic-smoke.py', '--url', $url, '--timeout', '180')
}
