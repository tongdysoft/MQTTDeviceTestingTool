SET name=bin\MqttClientTestTool_v1.1.0_
DEL /Q bin\*
SET CGO_ENABLED=0
SET GOARCH=amd64
go generate
SET GOOS=windows
go build -o %name%Windows64.exe .
DEL /Q *.syso
SET GOOS=linux
go build -o %name%Linux64 .
SET GOOS=darwin
go build -o %name%macOS64 .
SET GOARCH=386
go generate
SET GOOS=windows
go build -o %name%Windows32.exe .
DEL /Q *.syso
SET GOOS=linux
go build -o %name%Linux32 .
SET name=
SET CGO_ENABLED=
SET GOARCH=
SET GOOS=
xz -z -e -9 -T 0 -v bin/*
