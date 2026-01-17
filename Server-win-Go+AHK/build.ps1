# Bealink Server 构建脚本 (PowerShell)
# 构建无控制台窗口的 Windows 可执行文件

Write-Host "正在构建 Bealink Server (无控制台窗口版本)..." -ForegroundColor Green
Write-Host ""

# 检查 rsrc 工具
$rsrcPath = "$env:USERPROFILE\go\bin\rsrc.exe"
if (-not (Test-Path $rsrcPath)) {
    Write-Host "正在安装 rsrc 工具..." -ForegroundColor Yellow
    go install github.com/akavel/rsrc@latest
    if ($LASTEXITCODE -ne 0) {
        Write-Host "错误: 安装 rsrc 工具失败" -ForegroundColor Red
        exit 1
    }
}

# 添加 Go bin 到 PATH
$env:PATH += ";$env:USERPROFILE\go\bin"

# 生成资源文件
Write-Host "正在生成资源文件..." -ForegroundColor Yellow
rsrc -ico assets\light.ico -manifest app.manifest -o rsrc.syso
if ($LASTEXITCODE -ne 0) {
    Write-Host "错误: 生成资源文件失败" -ForegroundColor Red
    exit 1
}

# 删除旧的 exe
if (Test-Path "bealinkserver.exe") {
    Remove-Item "bealinkserver.exe" -Force
}

# 编译
Write-Host "正在编译..." -ForegroundColor Yellow
go build -ldflags="-H windowsgui -s -w" -o bealinkserver.exe .
if ($LASTEXITCODE -ne 0) {
    Write-Host "错误: 编译失败" -ForegroundColor Red
    exit 1
}

Write-Host ""
Write-Host "构建完成！输出文件: bealinkserver.exe" -ForegroundColor Green
$file = Get-Item "bealinkserver.exe"
Write-Host "文件大小: $([math]::Round($file.Length/1MB, 2)) MB" -ForegroundColor Cyan
Write-Host ""
