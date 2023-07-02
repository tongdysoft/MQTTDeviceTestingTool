SET name=MqttClientTestTool_v1.3.2_
MD bin
DEL /Q bin\*
COPY InteractiveMode*.bat bin\
COPY README.md bin\
SET CGO_ENABLED=0
SET GOARCH=amd64
go generate
SET GOOS=windows
go build -o bin\%name%Windows64.exe .
DEL /Q *.syso
SET GOOS=linux
go build -o bin\%name%Linux64 .
SET GOOS=darwin
MD bin\%name%macOSI64.app
MD bin\%name%macOSI64.app\Contents
MD bin\%name%macOSI64.app\Contents\MacOS
MD bin\%name%macOSI64.app\Contents\Resources
COPY Info.plist bin\%name%macOSI64.app\Contents\
COPY icon.icns bin\%name%macOSI64.app\Contents\Resources\
go build -o bin\%name%macOSI64.app\Contents\MacOS\MqttClientTestTool_macOS64 .
SET GOARCH=arm64
MD bin\%name%macOSM64.app
MD bin\%name%macOSM64.app\Contents
MD bin\%name%macOSM64.app\Contents\MacOS
MD bin\%name%macOSM64.app\Contents\Resources
COPY Info.plist bin\%name%macOSM64.app\Contents\
COPY icon.icns bin\%name%macOSM64.app\Contents\Resources\
go build -o bin\%name%macOSM64.app\Contents\MacOS\MqttClientTestTool_macOS64 .
SET GOOS=windows
go build -o bin\%name%WindowsARM64.exe .
DEL /Q *.syso
SET GOARCH=386
go generate
SET GOOS=windows
go build -o bin\%name%Windows32.exe .
DEL /Q *.syso
SET GOOS=linux
go build -o bin\%name%Linux32 .
@REM xz -z -e -9 -T 0 -v bin/*
cd bin
dir /b *Windows32*.exe InteractiveMode*.bat README.md >l.txt
MAKECAB /F l.txt /D compressiontype=lzx /D compressionmemory=21 /D maxdisksize=1024000000
MOVE disk1\1.cab %name%Windows32.cab
dir /b *Windows64*.exe InteractiveMode*.bat README.md >l.txt
MAKECAB /F l.txt /D compressiontype=lzx /D compressionmemory=21 /D maxdisksize=1024000000
MOVE disk1\1.cab %name%Windows64.cab
dir /b *WindowsARM64*.exe InteractiveMode*.bat README.md >l.txt
MAKECAB /F l.txt /D compressiontype=lzx /D compressionmemory=21 /D maxdisksize=1024000000
MOVE disk1\1.cab %name%WindowsARM64.cab
RD disk1
7z a -mx9 -tzip %name%Linux32.zip %name%Linux32* README.md icon.png
7z a -mx9 -tzip %name%Linux64.zip %name%Linux64* README.md icon.png
7z a -mx9 -tzip %name%macOSI64.zip %name%macOSI64.app README.md
7z a -mx9 -tzip %name%macOSM64.zip %name%macOSM64.app README.md
RD /S /Q %name%macOSI64.app
RD /S /Q %name%macOSM64.app
DEL /Q *32
DEL /Q *64
DEL /Q *.exe
DEL /Q *.bat
DEL /Q *.md
DEL /Q *.icns
DEL /Q *.txt
DEL /Q setup.*
cd ..
SET name=
SET CGO_ENABLED=
SET GOARCH=
SET GOOS=
