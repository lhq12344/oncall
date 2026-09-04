@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0phase13-production-evidence-verify.ps1" %*
exit /b %ERRORLEVEL%
