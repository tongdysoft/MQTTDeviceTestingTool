SET NAME=MQTTDeviceTestingTool
SET PNAME=MQTTDeviceTestingTool
SET VERSION=1.0.0
RD /S /Q bin
MD bin
COPY README.md bin\
COPY %NAME%.desktop bin\
SET CGO_ENABLED=0

SET GOOS=windows

ECHO Compiling Windows x86
SET GOARCH=386
go generate
MD bin\%PNAME%_windows-x86
go build -o bin\%PNAME%_windows-x86\%NAME%.exe
COPY README.md bin\%PNAME%_windows-x86\
DEL /Q *.syso

ECHO Compiling Windows x64
SET GOARCH=amd64
go generate
MD bin\%PNAME%_windows-x64
go build -o bin\%PNAME%_windows-x64\%NAME%.exe
COPY README.md bin\%PNAME%_windows-x64\
DEL /Q *.syso

ECHO Compiling Windows ARM32
SET GOARCH=arm
go generate
MD bin\%PNAME%_windows-arm32
go build -o bin\%PNAME%_windows-arm32\%NAME%.exe
COPY README.md bin\%PNAME%_windows-arm32\
DEL /Q *.syso

ECHO Compiling Windows ARM64
SET GOARCH=arm64
go generate
MD bin\%PNAME%_windows-arm64
go build -o bin\%PNAME%_windows-arm64\%NAME%.exe
COPY README.md bin\%PNAME%_windows-arm64\
DEL /Q *.syso

SET GOOS=darwin

MD bin\%NAME%.app
MD bin\%NAME%.app\Contents
COPY Info.plist bin\%NAME%.app\Contents
MD bin\%NAME%.app\Contents\Resources
COPY ico\icon.icns bin\%NAME%.app\Contents\Resources
MD bin\%NAME%.app\Contents\MacOS
MD bin\%PNAME%_macos-x64
XCOPY bin\%NAME%.app bin\%PNAME%_macos-x64\%NAME%.app /E /I
MD bin\%PNAME%_macos-arm64
XCOPY bin\%NAME%.app bin\%PNAME%_macos-arm64\%NAME%.app /E /I
RD /S /Q bin\%NAME%.app

ECHO Compiling macOS x64
SET GOARCH=amd64
go build -o bin\%PNAME%_macos-x64\%NAME%
COPY README.md bin\%PNAME%_macos-x64\
COPY bin\%PNAME%_macos-x64\%NAME% bin\%PNAME%_macos-x64\%NAME%.app\Contents\MacOS\

ECHO Compiling macOS ARM64
SET GOARCH=arm64
go build -o bin\%PNAME%_macos-arm64\%NAME%
COPY README.md bin\%PNAME%_macos-arm64\
COPY bin\%PNAME%_macos-arm64\%NAME% bin\%PNAME%_macos-arm64\%NAME%.app\Contents\MacOS\

SET GOOS=linux

ECHO Compiling Linux x86
SET GOARCH=386
MD bin\%PNAME%_linux-x86
go build -o bin\%PNAME%_linux-x86\%NAME%
COPY README.md bin\%PNAME%_linux-x86\
COPY ico\icon.png bin\%PNAME%_linux-x86\%NAME%.png
COPY %NAME%.desktop bin\%PNAME%_linux-x86\

ECHO Compiling Linux x64
SET GOARCH=amd64
MD bin\%PNAME%_linux-x64
go build -o bin\%PNAME%_linux-x64\%NAME%
COPY README.md bin\%PNAME%_linux-x64\
COPY ico\icon.png bin\%PNAME%_linux-x64\%NAME%.png
COPY %NAME%.desktop bin\%PNAME%_linux-x64\

ECHO Compiling Linux ARM32
SET GOARCH=arm
MD bin\%PNAME%_linux-arm32
go build -o bin\%PNAME%_linux-arm32\%NAME%
COPY README.md bin\%PNAME%_linux-arm32\
COPY ico\icon.png bin\%PNAME%_linux-arm32\%NAME%.png
COPY %NAME%.desktop bin\%PNAME%_linux-arm32\

ECHO Compiling Linux ARM64
SET GOARCH=arm64
MD bin\%PNAME%_linux-arm64
go build -o bin\%PNAME%_linux-arm64\%NAME%
COPY README.md bin\%PNAME%_linux-arm64\
COPY ico\icon.png bin\%PNAME%_linux-arm64\%NAME%.png
COPY %NAME%.desktop bin\%PNAME%_linux-arm64\

CD bin
DEL *.md
DEL *.desktop
CD ..

SET VERSION=
SET CGO_ENABLED=
SET GOOS=
SET GOARCH=
SET PNAME=

ECHO Compiling Local
MD "%GOPATH%\bin"
go generate
go build -o "%GOPATH%\bin\%NAME%.exe"
DEL /Q *.syso
go clean
ECHO "%GOPATH%\bin\%NAME%.exe"
SET NAME=
