SET NAME=MqttClientTestTool
SET NAMEV=%NAME%_v1.5.3_
MD bin
DEL /Q bin\*
COPY InteractiveMode*.bat bin\
COPY OneKeyStart*.bat bin\
COPY README.md bin\
SET CGO_ENABLED=0
SET GOARCH=amd64
go generate
SET GOOS=windows
go build -o bin\%NAMEV%Windows64.exe .
DEL /Q *.syso
SET GOOS=linux
go build -o bin\%NAMEV%Linux64 .
COPY "macOS\mqttclienttesttool\AppIcon.xcassets\AppIcon.appiconset\MQTT client test tool.png" bin\icon.png
COPY mqttclienttesttool.desktop bin\
SET GOOS=darwin
MD bin\%NAMEV%macOSI64.app
MD bin\%NAMEV%macOSI64.app\Contents
MD bin\%NAMEV%macOSI64.app\Contents\MacOS
MD bin\%NAMEV%macOSI64.app\Contents\Resources
COPY Info.plist bin\%NAMEV%macOSI64.app\Contents\
COPY icon.icns bin\%NAMEV%macOSI64.app\Contents\Resources\
go build -o bin\%NAMEV%macOSI64.app\Contents\MacOS\%NAME%_macOS64 .
SET GOARCH=arm64
MD bin\%NAMEV%macOSM64.app
MD bin\%NAMEV%macOSM64.app\Contents
MD bin\%NAMEV%macOSM64.app\Contents\MacOS
MD bin\%NAMEV%macOSM64.app\Contents\Resources
COPY Info.plist bin\%NAMEV%macOSM64.app\Contents\
COPY icon.icns bin\%NAMEV%macOSM64.app\Contents\Resources\
go build -o bin\%NAMEV%macOSM64.app\Contents\MacOS\%NAME%_macOS64 .
SET GOOS=windows
go build -o bin\%NAMEV%WindowsARM64.exe .
DEL /Q *.syso
SET GOARCH=386
go generate
SET GOOS=windows
go build -o bin\%NAMEV%Windows32.exe .
DEL /Q *.syso
SET GOOS=linux
go build -o bin\%NAMEV%Linux32 .
@REM xz -z -e -9 -T 0 -v bin/*
cd bin
dir /b *Windows32*.exe InteractiveMode*.bat README.md >l.txt
MAKECAB /F l.txt /D compressiontype=lzx /D compressionmemory=21 /D maxdisksize=1024000000
MOVE disk1\1.cab %NAMEV%Windows32.cab
dir /b *Windows64*.exe InteractiveMode*.bat README.md >l.txt
MAKECAB /F l.txt /D compressiontype=lzx /D compressionmemory=21 /D maxdisksize=1024000000
MOVE disk1\1.cab %NAMEV%Windows64.cab
dir /b *WindowsARM64*.exe InteractiveMode*.bat README.md >l.txt
MAKECAB /F l.txt /D compressiontype=lzx /D compressionmemory=21 /D maxdisksize=1024000000
MOVE disk1\1.cab %NAMEV%WindowsARM64.cab
RD disk1
7z a -mx9 -tzip %NAMEV%Linux32.zip %NAMEV%Linux32* README.md icon.png mqttclienttesttool.desktop
7z a -mx9 -tzip %NAMEV%Linux64.zip %NAMEV%Linux64* README.md icon.png mqttclienttesttool.desktop
7z a -mx9 -tzip %NAMEV%macOSI64.zip %NAMEV%macOSI64.app README.md
7z a -mx9 -tzip %NAMEV%macOSM64.zip %NAMEV%macOSM64.app README.md
RD /S /Q %NAMEV%macOSI64.app
RD /S /Q %NAMEV%macOSM64.app
DEL /Q *32
DEL /Q *64
DEL /Q *.exe
DEL /Q *.bat
DEL /Q *.md
DEL /Q *.icns
DEL /Q *.txt
DEL /Q *.png
DEL /Q *.desktop
DEL /Q setup.*
cd ..
SET NAME=
SET NAMEV=
SET CGO_ENABLED=
SET GOARCH=
SET GOOS=
