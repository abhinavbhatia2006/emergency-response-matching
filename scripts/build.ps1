param([switch]$Offline)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
Push-Location $root
try {
    $jdk = Get-ChildItem -LiteralPath (Join-Path $root '.tools') -Directory -Filter 'jdk-21*' -ErrorAction SilentlyContinue | Select-Object -First 1
    if ($jdk) { $env:JAVA_HOME = $jdk.FullName }
    $maven = Join-Path $root '.tools/apache-maven-3.9.11/bin/mvn.cmd'
    $arguments = @('-B', '-Dmaven.repo.local=.tools/m2', 'verify')
    if ($Offline) { $arguments += '-o' }
    if (Test-Path -LiteralPath $maven) { & $maven @arguments }
    else { & ./mvnw.cmd @arguments }
    if ($LASTEXITCODE -ne 0) { throw "Maven build failed with exit code $LASTEXITCODE" }
} finally { Pop-Location }
