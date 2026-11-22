# Build and start the complete application in Docker Compose.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    $engine = Invoke-Checked { docker info --format '{{.OSType}}' }
    if ($engine -ne 'linux') { throw 'Switch Docker Desktop to Linux containers before continuing.' }

    if (-not (Test-Path -LiteralPath .env)) {
        Write-Host 'Creating local demo credentials and selecting host port 18080.'
        # The x mode creates the file exclusively and protects an existing .env.
        $createEnvironment = @'
from pathlib import Path
from secrets import token_hex

keys = ('INTERNAL_TOKEN', 'POSTGRES_PASSWORD', 'RABBITMQ_PASSWORD')
with Path('.env').open('x', encoding='ascii') as config:
    for key in keys:
        config.write(key + '=' + token_hex(32) + '\n')
    config.write('APP_PORT=18080\n')
'@
        Invoke-Python @('-c', $createEnvironment)
    }

    Write-Host 'Building the images and waiting for the API and its dependencies.'
    Invoke-Checked { docker compose config --quiet }
    Invoke-Checked { docker compose up -d --build --wait --wait-timeout 300 }
    Invoke-Checked { docker compose ps }

    $url = Get-ComposeUrl
    Test-ApiReady $url
    Write-Host "PASS: Compose dashboard is ready at $url"
}
