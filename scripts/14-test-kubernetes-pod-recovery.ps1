# Delete the sole matching worker and prove its replacement performs new work.
. "$PSScriptRoot/test-common.ps1"

function Get-MatchingWorkerPods {
    $workersJson = Invoke-Checked { kubectl @Kube get pods -l app=matching-worker -o json }
    foreach ($pod in ($workersJson | ConvertFrom-Json).items) {
        $phase = $pod.status.phase
        if ($pod.metadata.deletionTimestamp) { $phase = 'Terminating' }
        $ready = $pod.status.conditions | Where-Object { $_.type -eq 'Ready' -and $_.status -eq 'True' }
        [pscustomobject]@{
            Name = $pod.metadata.name
            Status = $phase
            Ready = [bool]$ready
            UID = $pod.metadata.uid
        }
    }
}

Invoke-ProjectStep {
    Test-ApiReady $KubernetesUrl
    $workers = @(Get-MatchingWorkerPods | Where-Object Status -ne 'Terminating')
    if ($workers.Count -ne 1) {
        throw 'Expected one active matching worker. Set the matching-worker Deployment to one replica first.'
    }
    $oldWorker = $workers[0]
    Invoke-Checked { kubectl @Kube wait --for=condition=Ready "pod/$($oldWorker.Name)" --timeout=120s }
    Write-Host "Deleting worker $($oldWorker.Name) with UID $($oldWorker.UID)."
    Invoke-Checked { kubectl @Kube delete pod $oldWorker.Name --wait=false }

    Write-Host 'Watching for the original pod to disappear and its replacement to become ready.'
    $deadline = (Get-Date).AddSeconds(120)
    do {
        $pods = @(Get-MatchingWorkerPods)
        Write-Host "[$(Get-Date -Format 'HH:mm:ss')] Matching worker pods"
        if ($pods.Count) {
            $pods | Format-Table Name, Status, Ready, UID -AutoSize | Out-Host
        } else {
            Write-Host 'Waiting for a matching worker pod.'
        }
        $replacement = @($pods | Where-Object {
            $_.UID -ne $oldWorker.UID -and $_.Status -ne 'Terminating'
        })
        $oldWorkerGone = $oldWorker.UID -notin $pods.UID
        $recovered = $oldWorkerGone -and $replacement.Count -eq 1 -and $replacement[0].Ready
        if (-not $recovered) { Start-Sleep -Seconds 1 }
    } while (-not $recovered -and (Get-Date) -lt $deadline)
    if (-not $recovered) {
        throw 'Expected the original worker to disappear and one replacement worker with a new UID to become Ready.'
    }

    Invoke-Checked { kubectl @Kube rollout status deployment/matching-worker --timeout=120s }
    $newWorker = $replacement[0]
    Invoke-Checked { kubectl @Kube wait --for=condition=Ready "pod/$($newWorker.Name)" --timeout=120s }
    Write-Host "Replacement worker $($newWorker.Name) is Ready with UID $($newWorker.UID)."
    Invoke-Python @('-u', 'scripts/geographic-smoke.py', '--url', $KubernetesUrl, '--timeout', '180')
}
