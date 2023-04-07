REM charset: GB2312, line break: CRLF, version: 1.1.0
SET vl=cn
SET vip=0.0.0.0
SET vpo=1883
SET vc=
SET vt=
SET vw=
SET vm=
SET vs=
SET vo=
SET vn=YES

:START
TITLE MQTT 客户端测试工具
COLOR 17
ECHO OFF
CLS
ECHO ================================================== 
ECHO MQTT 客户端测试工具
ECHO ==================================================
ECHO 程序将创建一个 MQTT 服务器，
ECHO 在创建完成后，可将设备接入进来进行监控。
ECHO 请输入编号以修改 MQTT 服务器对应的参数。
ECHO --------------------------------------------------
ECHO [0] 信息输出使用的语言: %vl%
ECHO [1] MQTT 服务器 IP 地址: %vip%
ECHO [2] MQTT 服务器 端口号: %vpo%
ECHO [3] 只允许客户端 ID 为这些的客户端: %vc%
ECHO [4] 只接收这些主题的信息: %vt%
ECHO [5] 只有消息内容包含这些关键词才会处理: %vw%
ECHO [6] 将收取到的消息保存到 .csv 文件路径: %vm%
ECHO [7] 将客户端状态变化保存到 .csv 文件路径: %vs%
ECHO [8] 将日志输出保存到 .log 文件路径: %vo%
ECHO [9] 单色模式输出(非普通 cmd 建议关闭): %vn%
ECHO --------------------------------------------------
ECHO [Y] 启动 MQTT 服务器
ECHO [N] 退出
ECHO ==================================================
SET /P v=请输入编号: 

IF "%v%" EQU "0" GOTO SET_VL
IF "%v%" EQU "1" GOTO SET_VIP
IF "%v%" EQU "2" GOTO SET_VPO
IF "%v%" EQU "3" GOTO SET_VC
IF "%v%" EQU "4" GOTO SET_VT
IF "%v%" EQU "5" GOTO SET_VW
IF "%v%" EQU "6" GOTO SET_VM
IF "%v%" EQU "7" GOTO SET_VS
IF "%v%" EQU "8" GOTO SET_VO
IF "%v%" EQU "9" GOTO SET_VN
IF "%v%" EQU "Y" GOTO RUN
IF "%v%" EQU "y" GOTO RUN
IF "%v%" EQU "N" GOTO PEND
IF "%v%" EQU "n" GOTO PEND
GOTO START

:SET_VL
CLS
ECHO 输出语言选择:
ECHO [0] 简体中文
ECHO [1] 英语
SET /P vl=请输入编号: 
IF "%vl%" EQU "0" SET vl=cn
IF "%vl%" EQU "1" SET vl=en
GOTO START

:SET_VIP
CLS
ECHO 当前值: %vip%
ECHO 请输入或选择 IP 地址:
ECHO [0] 0.0.0.0
ECHO [1] 127.0.0.1
ECHO [l] localhost
ECHO [c] 可查看当前网卡信息
ECHO [手动输入] IP 地址
SET /P vit=IP 地址: 
IF "%vit%" EQU "0" SET vit=0.0.0.0
IF "%vit%" EQU "1" SET vit=127.0.0.1
IF "%vit%" EQU "l" SET vit=localhost
IF "%vit%" EQU "c" GOTO SET_VIP_V
SET vip=%vit%
GOTO START

:SET_VIP_V
CLS
ipconfig
PAUSE
GOTO SET_VIP

:SET_VPO
CLS
ECHO 当前值: %vpo%
ECHO 请输入端口:
SET /P vpo=端口号（数字）: 
IF "%vpo%" EQU "" SET vpo=1883
GOTO START

:SET_VC
CLS
ECHO 当前值: %vc%
ECHO 只允许客户端 ID 为这些的客户端:
SET /P vc=客户端 ID (英文逗号分隔): 
GOTO START

:SET_VT
CLS
ECHO 当前值: %vt%
ECHO 只接收这些主题的信息:
SET /P vt=主题 (英文逗号分隔): 
GOTO START

:SET_VW
CLS
ECHO 当前值: %vw%
ECHO 只有消息内容包含这些关键词才会处理:
SET /P vw=关键词 (英文逗号分隔): 
GOTO START

:SET_VM
CLS
ECHO 当前值: %vm%
ECHO 将收取到的消息保存到 .csv 文件路径:
SET /P vm=文件路径 (.csv): 
GOTO START

:SET_VS
CLS
ECHO 当前值: %vs%
ECHO 将客户端状态变化保存到 .csv 文件路径:
SET /P vs=文件路径 (.csv): 
GOTO START

:SET_VO
CLS
ECHO 当前值: %vo%
ECHO 将日志输出保存到 .log 文件路径:
SET /P vo=文件路径 (.log): 
GOTO START

:SET_VN
CLS
ECHO 当前值: %vn%
ECHO 单色模式输出:
ECHO [0] 否
ECHO [1] 是
SET /P vn=请输入编号: 
IF "%vn%" EQU "0" SET vn=NO
IF "%vn%" EQU "1" SET vn=YES
GOTO START

:RUN
COLOR 07
COLOR
CLS
DATE /T
TIME /T
ECHO 正在启动...
ECHO 按 Ctrl+C 可退出程序。
SET el= -l "%vl%"
SET ip= -p "%vip%:%vpo%"
IF "%vc%" NEQ "" SET ec= -c "%vc%".
IF "%vt%" NEQ "" SET et= -t "%vt%"
IF "%vw%" NEQ "" SET ew= -w "%vw%"
IF "%vm%" NEQ "" SET em= -m "%vm%"
IF "%vs%" NEQ "" SET es= -s "%vs%"
IF "%vo%" NEQ "" SET eo= -o "%vo%"
IF "%vn%" EQU "NO" SET en=
IF "%vn%" EQU "YES" SET en= -n
SET px=32
if "%PROCESSOR_ARCHITECTURE%"=="AMD64" SET px=64
TITLE MQTT 客户端测试工具 - %vip%:%vpo%
ECHO ON
MqttClientTestTool_v1.1.0_Windows%px%.exe%el%%ip%%ec%%et%%ew%%em%%es%%eo%%en%
GOTO START

:PEND
COLOR
CLS
ECHO ON

:END
