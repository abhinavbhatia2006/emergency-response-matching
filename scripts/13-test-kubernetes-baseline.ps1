# Confirm that the deployed UI and the API-to-storage and worker paths are usable.
. "$PSScriptRoot/test-common.ps1"

Invoke-ProjectStep {
    Write-Host 'Showing the Deployments, Pods, and Services for the Kubernetes application.'
    Invoke-Checked { kubectl @Kube get deployments,pods,services }
    Test-ApiReady $KubernetesUrl
    Invoke-Python @('-u', 'scripts/smoke.py', '--url', $KubernetesUrl, '--count', '6', '--timeout', '180')
}
