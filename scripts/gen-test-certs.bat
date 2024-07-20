@echo off
chcp 65001 >nul
setlocal
rem 用 MSYS2 / Git for Windows 的 bash 运行同目录的 gen-test-certs.sh。
rem 含中文或空格的参数在 cmd 中可能受代码页影响；必要时请在 MSYS2 终端直接运行 .sh。
set "SCRIPT_DIR=%~dp0"
set "BASH_EXE="
if exist "%SystemDrive%\msys64\usr\bin\bash.exe" set "BASH_EXE=%SystemDrive%\msys64\usr\bin\bash.exe"
if not defined BASH_EXE if exist "%ProgramFiles%\Git\bin\bash.exe" set "BASH_EXE=%ProgramFiles%\Git\bin\bash.exe"
if not defined BASH_EXE (
  for %%I in (bash.exe) do if not "%%~$PATH:I"=="" set "BASH_EXE=%%~$PATH:I"
)
if not defined BASH_EXE (
  echo 未找到 bash。请安装 MSYS2 或 Git for Windows，或在 MSYS2 终端中直接运行 scripts/gen-test-certs.sh。
  exit /b 2
)
"%BASH_EXE%" --noprofile --norc "%SCRIPT_DIR%gen-test-certs.sh" %*
exit /b %ERRORLEVEL%
