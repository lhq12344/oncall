@echo off
setlocal
powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0phase13-verify.ps1" %*
exit /b %ERRORLEVEL%
