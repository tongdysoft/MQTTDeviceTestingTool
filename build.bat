SET name=MqttClientTestTool_v1.3.2_
DEL /Q bin\*
COPY InteractiveMode*.bat bin\
COPY README.md bin\
COPY icon.icns bin\app.icns
SET CGO_ENABLED=0
SET GOARCH=amd64
go generate
SET GOOS=windows
go build -o bin\%name%Windows64.exe .
DEL /Q *.syso
SET GOOS=linux
go build -o bin\%name%Linux64 .
SET GOOS=darwin
go build -o bin\%name%macOS64 .
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
RD disk1
7z a -mx9 -tzip %name%Linux32.zip %name%Linux32* README.md icon.png
7z a -mx9 -tzip %name%Linux64.zip %name%Linux64* README.md icon.png
7z a -mx9 -tzip %name%macOS64.zip %name%macOS64* README.md icon.icns
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
