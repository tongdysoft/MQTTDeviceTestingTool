@ECHO OFF
REM charset: GB2312, line break: CRLF, version: 1.3.2
SET px=32
if "%PROCESSOR_ARCHITECTURE%"=="AMD64" SET px=64
TITLE MQTT 客户端测试工具
MqttClientTestTool_v1.5.1_Windows%px%.exe -l cn
