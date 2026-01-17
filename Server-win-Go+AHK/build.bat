@echo off
chcp 65001 >nul
echo 正在构建 Bealink Server (无控制台窗口版本)...
echo.

REM 检查 rsrc 工具
where rsrc >nul 2>&1
if %errorlevel% neq 0 (
    echo 正在安装 rsrc 工具...
    go install github.com/akavel/rsrc@latest
)

REM 生成资源文件
echo 正在生成资源文件...
rsrc -ico assets\dark.ico -manifest app.manifest -o rsrc.syso
if %errorlevel% neq 0 (
    echo 错误: 生成资源文件失败
    pause
    exit /b 1
)

REM 编译
echo 正在编译...
go build -ldflags="-H windowsgui -s -w" -o bealinkserver.exe .
if %errorlevel% neq 0 (
    echo 错误: 编译失败
    pause
    exit /b 1
)

echo.
echo 构建完成！输出文件: bealinkserver.exe
echo.
pause
