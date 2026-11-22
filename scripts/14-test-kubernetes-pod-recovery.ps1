# Delete the sole matching worker and prove its replacement performs new work.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    Test-ApiReady $KubernetesUrl
    $workersJson = Invoke-Checked { kubectl @Kube get pods -l app=matching-worker -o json }
    $workers = @(($workersJson | ConvertFrom-Json).items | Where-Object { -not $_.metadata.deletionTimestamp })
    if ($workers.Count -ne 1) {
        throw 'Expected one active matching worker. Set the matching-worker Deployment to one replica first.'
    }
    $oldWorker = $workers[0].metadata
    Invoke-Checked { kubectl @Kube wait --for=condition=Ready "pod/$($oldWorker.name)" --timeout=120s }
    Write-Host "Deleting worker $($oldWorker.name) with UID $($oldWorker.uid)."
    Invoke-Checked { kubectl @Kube delete pod $oldWorker.name --timeout=90s }
    Invoke-Checked { kubectl @Kube rollout status deployment/matching-worker --timeout=120s }

    $deadline = (Get-Date).AddSeconds(120)
    do {
        $workersJson = Invoke-Checked { kubectl @Kube get pods -l app=matching-worker -o json }
        $replacement = @(($workersJson | ConvertFrom-Json).items | Where-Object {
            $_.metadata.uid -ne $oldWorker.uid -and -not $_.metadata.deletionTimestamp
        })
        if (-not $replacement.Count) { Start-Sleep -Seconds 2 }
    } while (-not $replacement.Count -and (Get-Date) -lt $deadline)
    if ($replacement.Count -ne 1) { throw 'Expected one replacement worker with a new UID.' }

    $newWorker = $replacement[0].metadata
    Invoke-Checked { kubectl @Kube wait --for=condition=Ready "pod/$($newWorker.name)" --timeout=120s }
    Write-Host "Replacement worker $($newWorker.name) is Ready with UID $($newWorker.uid)."
    Invoke-Python @('-u', 'scripts/geographic-smoke.py', '--url', $KubernetesUrl, '--timeout', '180')
}
