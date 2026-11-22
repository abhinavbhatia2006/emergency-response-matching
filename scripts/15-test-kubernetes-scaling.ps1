# Scale only stateless Deployments and check reachability and matching.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    $deployments = @('deployment/request-api', 'deployment/matching-worker')
    Test-ApiReady $KubernetesUrl
    $beforeJson = Invoke-Checked { kubectl @Kube get @deployments -o json }
    $baseline = @(($beforeJson | ConvertFrom-Json).items | Where-Object {
        $_.spec.replicas -eq 1 -and $_.status.readyReplicas -eq 1
    })
    if ($baseline.Count -ne 2) { throw 'Both Deployments must start with one Ready replica.' }

    try {
        Invoke-Checked { kubectl @Kube scale @deployments --replicas=2 }
        $deadline = (Get-Date).AddSeconds(120)
        do {
            Test-ApiReady $KubernetesUrl
            Write-Host "$(Get-Date -Format HH:mm:ss) API readiness confirmed during scale-up."

            $stateJson = Invoke-Checked { kubectl @Kube get @deployments -o json }
            $ready = @(($stateJson | ConvertFrom-Json).items | Where-Object {
                $_.spec.replicas -eq 2 -and $_.status.readyReplicas -eq 2
            })
            $endpointsJson = Invoke-Checked {
                kubectl @Kube get endpointslice -l kubernetes.io/service-name=request-api -o json
            }
            $apiEndpoints = @(($endpointsJson | ConvertFrom-Json).items | ForEach-Object { $_.endpoints } |
                Where-Object { $_.conditions.ready -eq $true })
            $scaled = $ready.Count -eq 2 -and $apiEndpoints.Count -eq 2
            if (-not $scaled) { Start-Sleep -Seconds 2 }
        } while (-not $scaled -and (Get-Date) -lt $deadline)

        if (-not $scaled) { throw 'Expected two Ready replicas of each Deployment and two Ready API endpoints.' }
        Invoke-Checked { kubectl @Kube get @deployments }
        Invoke-Checked { kubectl @Kube get endpointslice -l kubernetes.io/service-name=request-api -o wide }
        Invoke-Python @('-u', 'scripts/smoke.py', '--url', $KubernetesUrl, '--count', '12', '--timeout', '180')
        Write-Host 'PASS: Two Ready API endpoints and two Ready workers sustained a concurrent matching workload.'
    } finally {
        Write-Host 'Restoring both Deployments to one replica.'
        Invoke-Checked { kubectl @Kube scale @deployments --replicas=1 }
        foreach ($deployment in $deployments) {
            Invoke-Checked { kubectl @Kube rollout status $deployment --timeout=120s }
        }
    }
}
