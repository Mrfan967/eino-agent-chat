@echo off
chcp 65001 >nul
setlocal

cd /d "%~dp0"

set "APP_NAME=awesomeProject.exe"
if "%PORT%"=="" set "PORT=8080"
if "%ZHIPU_API_KEY%"=="" set "ZHIPU_API_KEY=5f1faa9546f04e1fa2f20a231cc2deaa.83s9Pt9iAbGN6TO7"
if "%MOONSHOT_API_KEY%"=="" set "MOONSHOT_API_KEY=sk-5rKOvSmwV015EurXmJaSLSdsnk8tOEFdQkCJkLpfJrBiELIb"
if "%ALIYUN_API_KEY%"=="" set "ALIYUN_API_KEY=sk-ODY1LTEyMTkzODMwNjUwLTE3NzM3MTYwMTQ4MTU="
if "%PGVECTOR_DSN%"=="" set "PGVECTOR_DSN=postgres://postgres:postgres@localhost:5432/postgres?sslmode=disable"

echo ----------------------------------------
echo Starting %APP_NAME% on Windows Server
echo Project dir: %cd%
echo Port: %PORT%
echo ----------------------------------------

where go >nul 2>nul
if errorlevel 1 (
    echo Error: Go is not installed or not in PATH
    pause
    exit /b 1
)

if "%MOONSHOT_API_KEY%"=="" if "%ZHIPU_API_KEY%"=="" if "%OPENAI_API_KEY%"=="" if "%ALIYUN_API_KEY%"=="" (
    echo Error: no model API key found
    echo Set at least one of: MOONSHOT_API_KEY, ZHIPU_API_KEY, OPENAI_API_KEY, ALIYUN_API_KEY
    pause
    exit /b 1
)

echo Building binary...
go build -o "%APP_NAME%" .
if errorlevel 1 (
    echo Error: build failed
    pause
    exit /b 1
)

echo Build complete, starting service...
echo Visit: http://your-server-ip:%PORT%

set "PORT=%PORT%"
"%cd%\%APP_NAME%"
