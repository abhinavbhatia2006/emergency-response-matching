# Load the images built in step 01 into a dedicated local kind deployment.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    if (-not (Test-Path -LiteralPath .env)) {
        throw 'Run 01-setup-docker.ps1 first to prepare the local credentials and images.'
    }

    $cgroupVersion = Invoke-Checked { docker info --format '{{.CgroupVersion}}' }
    if ($cgroupVersion -ne '2') {
        throw "Kubernetes 1.37 requires cgroup v2, but Docker reports '$cgroupVersion'. Use the Linux engine with cgroup v2. On Docker Desktop with WSL 2, update WSL, restart Docker Desktop, and check docker info before retrying."
    }

    $clusters = @(Invoke-Checked { kind get clusters })
    if ($clusters -notcontains $DemoCluster) {
        $nodeImage = 'kindest/node:v1.37.0@sha256:a1ed56cfb0e7b93589bdf97c8cd566405a265939e3620fc4f5de89adff580ae5'
        Invoke-Checked { kind create cluster --name $DemoCluster --image $nodeImage --wait 180s }
    }
    Invoke-Checked { kubectl --context $DemoContext get nodes }
    Invoke-Checked { kubectl --context $DemoContext get storageclass }

    $images = @(
        'emergency-response-matching/coordinator:local'
        'emergency-response-matching/storage-node:local'
        'emergency-response-matching/request-api:local'
        'emergency-response-matching/matching-worker:local'
    )
    Invoke-Checked { kind load docker-image --name $DemoCluster @images }

    Invoke-Checked { kubectl --context $DemoContext apply -f deploy/kubernetes/namespace.yaml }
    $existingApp = Invoke-Checked { kubectl @Kube get deployment request-api --ignore-not-found -o name }
    $existingSecret = Invoke-Checked { kubectl @Kube get secret response-secrets --ignore-not-found -o name }
    if (-not $existingSecret) {
        Invoke-Checked { kubectl @Kube create secret generic response-secrets --from-env-file=.env }
    }

    Invoke-Checked { kubectl --context $DemoContext apply --dry-run=server -k deploy/kubernetes }
    Invoke-Checked { kubectl --context $DemoContext apply -k deploy/kubernetes }

    if ($existingApp) {
        # Pods using the same :local tag must restart to pick up reloaded images.
        $applicationWorkloads = @('deployment/request-api', 'deployment/matching-worker', 'statefulset/coordinator')
        foreach ($shard in 0..2) {
            $applicationWorkloads += "statefulset/shard-$shard-primary", "statefulset/shard-$shard-replica"
        }
        Invoke-Checked { kubectl @Kube rollout restart @applicationWorkloads }
    }

    $workloads = Invoke-Checked { kubectl @Kube get deployment,statefulset -o name }
    foreach ($workload in $workloads) {
        Invoke-Checked { kubectl @Kube rollout status $workload --timeout=300s }
    }

    $claimsJson = Invoke-Checked { kubectl @Kube get pvc -o json }
    $claims = @(($claimsJson | ConvertFrom-Json).items | Where-Object { $_.status.phase -eq 'Bound' })
    if ($claims.Count -ne 8) { throw "Expected eight Bound PVCs, found $($claims.Count)." }

    Invoke-Checked { kubectl @Kube get deployment,statefulset,pods,svc,pvc }
    Write-Host 'PASS: Kubernetes workloads rolled out and all eight persistent claims are Bound.'
    Write-Host 'Run 12-forward-kubernetes.ps1 in a separate PowerShell window.'
}
