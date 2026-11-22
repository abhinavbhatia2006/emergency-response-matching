# Require a live RabbitMQ consumer, then exercise asynchronous assignments.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    $url = Get-ComposeUrl
    Write-Host 'Checking the matching.requests queue and its active consumers.'
    $deadline = (Get-Date).AddSeconds(30)
    do {
        $queue = @(Invoke-Checked {
            docker compose exec -T rabbitmq rabbitmqctl -q list_queues name consumers messages_ready messages_unacknowledged
        })
        $consumer = $queue -match '^\s*matching\.requests\s+[1-9]\d*\s'
        if (-not $consumer) { Start-Sleep -Seconds 2 }
    } while (-not $consumer -and (Get-Date) -lt $deadline)
    $queue | Write-Host
    if (-not $consumer) {
        throw 'The matching.requests queue has no active consumer. Check the matching worker logs.'
    }

    Write-Host 'Submitting 12 requests concurrently and waiting for automatic assignments.'
    Invoke-Python @('-u', 'scripts/smoke.py', '--url', $url, '--count', '12', '--timeout', '180', '--concurrency-only')
}
