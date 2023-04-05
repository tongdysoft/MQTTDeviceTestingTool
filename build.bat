SET name=MqttClientTestTool_v1.1.0_
DEL /Q bin\*
COPY 中文交互模式.bat bin\
COPY README.md bin\
COPY rc.icns bin\app.icns
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
7z a -mx9 -tzip %name%Windows.zip %name%Windows* 中文交互模式.bat README.md
7z a -mx9 -tzip %name%Linux.zip %name%Linux* README.md
7z a -mx9 -tzip %name%macOS.zip %name%macOS* README.md app.icns
DEL /Q *32
DEL /Q *64
DEL /Q *.exe
DEL /Q *.bat
DEL /Q *.md
DEL /Q *.icns
cd ..
SET name=
SET CGO_ENABLED=
SET GOARCH=
SET GOOS=
