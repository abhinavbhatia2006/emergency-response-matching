param([string]$Context = 'kind-emergency')
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
function K { & kubectl --context $Context @args; if ($LASTEXITCODE -ne 0) { throw 'kubectl failed' } }
try {
    if (Test-Path -LiteralPath (Join-Path $root '.tools/kind.exe')) { $env:PATH = (Join-Path $root '.tools') + [IO.Path]::PathSeparator + $env:PATH }
    if ($Context -ne 'kind-emergency') { throw 'This script deploys only to the local kind-emergency context' }
    K apply -f infra/kubernetes/namespace.yaml
    K apply --server-side -f https://raw.githubusercontent.com/cloudnative-pg/cloudnative-pg/release-1.28/releases/cnpg-1.28.4.yaml
    K rollout status deployment/cnpg-controller-manager -n cnpg-system --timeout=180s
    $values = @{}
    foreach ($line in Get-Content .env) { if ($line -match '^([A-Z_]+)=(.*)$') { $values[$Matches[1]]=$Matches[2] } }
    $secret = @{apiVersion='v1';kind='Secret';metadata=@{name='application-secrets';namespace='emergency'};type='Opaque';stringData=$values}
    $secret | ConvertTo-Json -Depth 6 | kubectl --context $Context apply -f -
    if ($LASTEXITCODE -ne 0) { throw 'Application secret creation failed' }
    $database = @{apiVersion='v1';kind='Secret';metadata=@{name='database-credentials';namespace='emergency'};type='kubernetes.io/basic-auth';stringData=@{username=$values.DB_USER;password=$values.DB_PASSWORD}}
    $database | ConvertTo-Json -Depth 6 | kubectl --context $Context apply -f -
    if ($LASTEXITCODE -ne 0) { throw 'Database secret creation failed' }
    K apply -f infra/kubernetes/postgres/
    K apply -f infra/kubernetes/rabbitmq.yaml
    foreach ($i in 0..2) { K wait --for=condition=Ready "cluster/shard-$i" -n emergency --timeout=300s }
    K rollout status deployment/rabbitmq -n emergency --timeout=180s
    foreach ($service in 'storage-coordinator','request-service','resource-service','matching-service','frontend') {
        & kind load docker-image "emergency/${service}:dev" --name emergency
        if ($LASTEXITCODE -ne 0) { throw "Image load failed: $service" }
    }
    & docker build -t emergency/health-reporter:dev -f infra/health-reporter/Dockerfile .
    if ($LASTEXITCODE -ne 0) { throw 'Health reporter build failed' }
    & kind load docker-image emergency/health-reporter:dev --name emergency
    if ($LASTEXITCODE -ne 0) { throw 'Health reporter image load failed' }
    K apply -f infra/kubernetes/services/
    K apply -f infra/kubernetes/network-policy.yaml
    K apply -f infra/kubernetes/health-reporter.yaml
    foreach ($service in 'storage-coordinator','request-service','resource-service','matching-service','frontend') { K rollout status "deployment/$service" -n emergency --timeout=180s }
    Write-Output 'Deployment ready. Run: kubectl --context kind-emergency -n emergency port-forward svc/frontend 8088:8080'
} finally { Pop-Location }
