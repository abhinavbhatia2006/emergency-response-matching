# Keep this window open to expose the Kubernetes dashboard on local port 18082.
. "$PSScriptRoot/test-common.ps1"

Write-Host "Opening $KubernetesUrl through service/request-api. Press Ctrl+C to stop forwarding."
Invoke-Checked { kubectl @Kube port-forward --address 127.0.0.1 service/request-api 18082:8080 }
