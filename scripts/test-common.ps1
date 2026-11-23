# Shared command checks and addresses for the numbered steps.
$ErrorActionPreference = 'Stop'
$ProjectRoot = Split-Path -Parent $PSScriptRoot
$DemoCluster = 'sahara-demo-v137'
$DemoContext = "kind-$DemoCluster"
$DemoNamespace = 'response-matching'
$Kube = @('--context', $DemoContext, '-n', $DemoNamespace)
$KubernetesUrl = 'http://127.0.0.1:18082'

function Invoke-ProjectStep {
    param([scriptblock]$Step)
    Push-Location $ProjectRoot
    try {
        & $Step
    } finally {
        Pop-Location
    }
}

function Invoke-Checked {
    param([scriptblock]$Command)
    & $Command
    if ($LASTEXITCODE -ne 0) {
        throw "Command failed (exit $LASTEXITCODE): $Command"
    }
}

function Invoke-Python {
    param([string[]]$Arguments)
    foreach ($interpreter in 'python', 'py') {
        if (-not (Get-Command $interpreter -ErrorAction SilentlyContinue)) { continue }
        $versionArguments = @()
        if ($interpreter -eq 'py') { $versionArguments = @('-3') }
        try {
            $version = & $interpreter @versionArguments --version 2>$null
        } catch {
            continue
        }
        if ($LASTEXITCODE -eq 0 -and $version -match '^Python 3\.') {
            Invoke-Checked { & $interpreter @versionArguments @Arguments }
            return
        }
    }
    throw 'Python 3 is required. Check python --version or py -3 --version.'
}

function Get-ComposeUrl {
    # Use the running container's actual host port.
    $binding = Invoke-Checked { docker compose port request-api 8080 }
    if ($binding -notmatch '^127\.0\.0\.1:\d+$') {
        throw "Expected the Compose API to publish a local IPv4 port. Mapping: $binding"
    }
    "http://$binding"
}

function Test-ApiReady {
    param([string]$Url)
    $reply = Invoke-Checked { curl.exe --noproxy '*' --fail --silent --show-error --max-time 15 "$Url/readyz" }
    if (($reply | ConvertFrom-Json).status -ne 'ready') {
        throw "Expected a ready API at $Url/readyz."
    }
}

function Invoke-StorageTest {
    param([string]$Scenario, [switch]$Pause)
    $url = Get-ComposeUrl
    Test-ApiReady $url
    $testArguments = @('-u', 'scripts/failure-smoke.py', '--env-file', '.env', '--url', $url, '--scenario', $Scenario)
    if ($Pause) { $testArguments += '--pause' }
    Invoke-Python $testArguments
}
