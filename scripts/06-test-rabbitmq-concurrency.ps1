# Require a live RabbitMQ consumer, then exercise asynchronous assignments.
. "$PSScriptRoot/test-common.ps1"

function Get-RabbitQueue {
    Invoke-Checked {
        docker compose exec -T rabbitmq rabbitmqctl -q list_queues name consumers messages_ready messages_unacknowledged
    }
}

Invoke-ProjectStep {
    $url = Get-ComposeUrl
    Write-Host 'Checking the matching.requests queue and its active consumers.'
    $deadline = (Get-Date).AddSeconds(30)
    do {
        $queue = @(Get-RabbitQueue)
        $consumer = $queue -match '^\s*matching\.requests\s+[1-9]\d*\s'
        if (-not $consumer) { Start-Sleep -Seconds 2 }
    } while (-not $consumer -and (Get-Date) -lt $deadline)
    $queue | Write-Host
    if (-not $consumer) {
        throw 'The matching.requests queue has no active consumer. Check the matching worker logs.'
    }

    Write-Host 'Submitting 12 requests concurrently and waiting for automatic assignments.'
    $smokeTest = Start-Job -ArgumentList $PSScriptRoot, $url -ScriptBlock {
        param($ScriptDirectory, $ComposeUrl)
        . "$ScriptDirectory/test-common.ps1"
        Invoke-ProjectStep {
            Invoke-Python @('-u', 'scripts/smoke.py', '--url', $ComposeUrl, '--count', '12', '--timeout', '180', '--concurrency-only')
        }
    }
    try {
        while ($smokeTest.State -in 'NotStarted', 'Running') {
            Write-Host "[$(Get-Date -Format 'HH:mm:ss')] RabbitMQ queue"
            try {
                Get-RabbitQueue | Write-Host
            } catch {
                Write-Warning "Queue monitoring stopped: $_"
                break
            }
            Receive-Job -Job $smokeTest
            Wait-Job -Job $smokeTest -Timeout 1 | Out-Null
        }
        Receive-Job -Job $smokeTest -Wait
        if ($smokeTest.State -ne 'Completed') {
            throw 'The RabbitMQ concurrency test did not complete.'
        }
    } finally {
        Remove-Job -Job $smokeTest -Force
    }
}
