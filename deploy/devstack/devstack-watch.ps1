# WeKnora devstack 宿主机文件监听（PowerShell 7 / 5.1 均可）
#
# 为什么需要它：
#   Windows -> 容器的 bind mount（Docker Desktop 走 9p/virtiofs）在“覆盖写”时
#   只同步文件内容、不同步 mtime，且 inotify 事件不外传。结果是容器内
#   air / chokidar / find -newer 全部无法感知宿主机编辑器的保存。
#   唯一可靠的路径是让 Windows 侧自己监听，再显式通知容器。
#
# 用法（保持窗口常开，或放进启动项）：
#   pwsh -File deploy/devstack/devstack-watch.ps1
param(
    [string]$Path = (Split-Path -Parent (Split-Path -Parent (Split-Path -Parent $PSScriptRoot))),
    [string]$Container = 'wktc-app'
)

$ErrorActionPreference = 'Stop'
if (-not (Test-Path $Path)) { throw "path not found: $Path" }

Write-Host "[watch] root      = $Path"
Write-Host "[watch] container = $Container"
Write-Host "[watch] Ctrl+C to stop"

$fsw = New-Object System.IO.FileSystemWatcher
$fsw.Path = $Path
$fsw.Filter = '*.go'
$fsw.IncludeSubdirectories = $true
$fsw.NotifyFilter = [System.IO.NotifyFilters]'LastWrite, FileName, CreationTime'

# 事件回调运行在独立 runspace：函数与 $using: 都不可用，
# 因此只用 $Event 自身携带的信息（MessageData 传容器名）。
$action = {
    $full = $Event.SourceEventArgs.FullPath
    if ($full -notmatch '\\internal\\' -and $full -notmatch '\\cmd\\') { return }
    if ($full -match '\\third_party\\') { return }
    docker exec $Event.MessageData touch /tmp/reload-trigger 2>$null
    Write-Host "[watch] $(Get-Date -Format 'HH:mm:ss')  $([System.IO.Path]::GetFileName($full))" -ForegroundColor DarkGray
}

Register-ObjectEvent -InputObject $fsw -EventName Changed -Action $action -MessageData $Container | Out-Null
Register-ObjectEvent -InputObject $fsw -EventName Created -Action $action -MessageData $Container | Out-Null
$fsw.EnableRaisingEvents = $true

try {
    while ($true) { Start-Sleep -Seconds 1 }
}
finally {
    $fsw.EnableRaisingEvents = $false
    Get-EventSubscriber | Unregister-Event -ErrorAction SilentlyContinue
    Write-Host '[watch] stopped'
}
