# Run dashboard tests and produce a validated production build.
param([switch]$Install)
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    if ($Install -or -not (Test-Path -LiteralPath web/node_modules)) {
        Invoke-Checked { npm.cmd --prefix web ci }
    }
    Write-Host 'Running Vitest and checking that TypeScript and Vite build the dashboard.'
    Invoke-Checked { npm.cmd --prefix web test }
    Invoke-Checked { npm.cmd --prefix web run build }
    Write-Host 'PASS: Frontend tests and production build.'
}
