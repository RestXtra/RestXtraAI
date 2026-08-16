# RestXtra AI 开发热更新脚本（Windows PowerShell）
# 后端 :8787 + 流量代理 :8788 + 前端热更新 :5173，一键启动，Ctrl-C 一并退出。
# 用法：powershell -ExecutionPolicy Bypass -File .\dev.ps1
# 之后改前端 → 刷新 :5173 秒级生效；改后端 → Ctrl-C 重启即可。
# 只有发布时才需要全量内嵌：npm run build:static + go build -tags embedui。

$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent $MyInvocation.MyCommand.Path
Set-Location $Root

Write-Host ""
Write-Host "  RestXtra AI 开发模式" -ForegroundColor Cyan
Write-Host "  后端 API :8787 · 流量代理 :8788 · 前端 http://localhost:5173  (Ctrl-C 退出)" -ForegroundColor Cyan
Write-Host ""

$backend = Start-Job -Name restxtra-backend -ScriptBlock {
    Set-Location $using:Root
    go run ./cmd/restxtra -addr :8787 -proxy :8788
}
$frontend = Start-Job -Name restxtra-frontend -ScriptBlock {
    Set-Location "$using:Root\web"
    npm run dev
}

function Receive-Streams {
    foreach ($job in @($backend, $frontend)) {
        Receive-Job $job -Keep 2>$null | ForEach-Object {
            $tag = if ($job.Name -eq "restxtra-backend") { "backend " } else { "frontend" }
            Write-Host ("  [{0}] {1}" -f $tag, $_) -ForegroundColor Gray
        }
    }
}

try {
    # 等后端就绪
    $ready = $false
    for ($i = 0; $i -lt 40; $i++) {
        Start-Sleep -Milliseconds 500
        try { if ((Invoke-WebRequest -Uri "http://127.0.0.1:8787/api/health" -UseBasicParsing -TimeoutSec 1).StatusCode -eq 200) { $ready = $true; break } } catch {}
    }
    if ($ready) {
        Write-Host "  后端已就绪 → 打开 http://localhost:5173" -ForegroundColor Green
    } else {
        Write-Host "  等待后端超时（看下方输出排查）" -ForegroundColor Yellow
    }

    while ($true) {
        Receive-Streams
        if ($backend.State -in @("Failed", "Completed") -and $frontend.State -in @("Failed", "Completed")) { break }
        Start-Sleep -Milliseconds 600
    }
} finally {
    Write-Host ""
    Write-Host "  正在停止…" -ForegroundColor Yellow
    Stop-Job $backend, $frontend -ErrorAction SilentlyContinue
    Remove-Job $backend, $frontend -Force -ErrorAction SilentlyContinue
    Write-Host "  已停止。" -ForegroundColor Yellow
}
