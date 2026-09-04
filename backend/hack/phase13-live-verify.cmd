@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0phase13-live-verify.ps1" %*
exit /b %ERRORLEVEL%
