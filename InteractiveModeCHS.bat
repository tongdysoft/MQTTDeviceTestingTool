REM charset: GB2312, line break: CRLF, version: 1.3.2
SET vl=cn
SET vip=0.0.0.0
SET vpo=1883
SET vu=
SET vc=
SET vt=
SET vw=
SET vm=
SET vs=
SET vo=
SET ca=
SET ce=
SET ck=
SET cp=
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
ECHO [ 0] 信息输出使用的语言: %vl%
ECHO [ 1] MQTT 服务器 IP 地址: %vip%
ECHO [ 2] MQTT 服务器 端口号: %vpo%
ECHO [ 3] 用户和主题权限配置文件路径: %vu%
ECHO [ 4] CA 证书文件路径: %ca%
ECHO [ 5] 自签名证书文件路径: %ce%
ECHO [ 6] 自签名私钥文件路径: %ck%
ECHO [ 7] 自签名私钥文件的密码: %cp%
ECHO [ 8] 只允许客户端 ID 为这些的客户端: %vc%
ECHO [ 9] 只接收这些主题的信息: %vt%
ECHO [10] 只有消息内容包含这些关键词才会处理: %vw%
ECHO [11] 将收取到的消息保存到 .csv 文件路径: %vm%
ECHO [12] 将客户端状态变化保存到 .csv 文件路径: %vs%
ECHO [13] 将日志输出保存到 .log 文件路径: %vo%
ECHO [14] 单色模式输出(非普通 cmd 建议关闭): %vn%
ECHO --------------------------------------------------
ECHO [Y] 启动 MQTT 服务器
ECHO [H] 获取帮助
ECHO [N] 退出
ECHO ==================================================
SET /P v=请输入编号: 

IF "%v%" EQU "0" GOTO SET_VL
IF "%v%" EQU "1" GOTO SET_VIP
IF "%v%" EQU "2" GOTO SET_VPO
IF "%v%" EQU "3" GOTO SET_VU
IF "%v%" EQU "4" GOTO SET_CA
IF "%v%" EQU "5" GOTO SET_CE
IF "%v%" EQU "6" GOTO SET_CK
IF "%v%" EQU "7" GOTO SET_CP
IF "%v%" EQU "8" GOTO SET_VC
IF "%v%" EQU "9" GOTO SET_VT
IF "%v%" EQU "10" GOTO SET_VW
IF "%v%" EQU "11" GOTO SET_VM
IF "%v%" EQU "12" GOTO SET_VS
IF "%v%" EQU "13" GOTO SET_VO
IF "%v%" EQU "14" GOTO SET_VN
IF "%v%" EQU "Y" GOTO RUN
IF "%v%" EQU "y" GOTO RUN
IF "%v%" EQU "H" GOTO HELP
IF "%v%" EQU "h" GOTO HELP
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
ECHO [0] 允许所有IP(0.0.0.0)
ECHO [1] 本机(127.0.0.1)
ECHO [l] 本机(localhost)
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

:SET_VU
CLS
ECHO 当前值: %vu%
ECHO 用户和主题权限配置文件路径:
SET /P vu=文件路径 (.json):
GOTO START

:SET_CA
CLS
ECHO 当前值: %ca%
ECHO CA 证书文件路径:
SET /P ca=文件路径 (.pem/.crt): 
GOTO START

:SET_CE
CLS
ECHO 当前值: %ce%
ECHO 自签名证书文件路径:
SET /P ce=文件路径 (.pem/.crt): 
GOTO START

:SET_CK
CLS
ECHO 当前值: %ck%
ECHO 自签名私钥文件路径:
SET /P ck=文件路径 (.key): 
GOTO START

:SET_CP
CLS
ECHO 当前值: %cp%
ECHO 自签名私钥文件的密码:
SET /P cp=密码: 
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
IF "%vc%" NEQ "" SET ec= -c "%vc%"
IF "%vt%" NEQ "" SET et= -t "%vt%"
IF "%vw%" NEQ "" SET ew= -w "%vw%"
IF "%ca%" NEQ "" SET eca= -ca "%ca%"
IF "%ce%" NEQ "" SET ece= -ce "%ce%"
IF "%ck%" NEQ "" SET eck= -ck "%ck%"
IF "%cp%" NEQ "" SET ecp= -cp "%cp%"
IF "%vm%" NEQ "" SET em= -m "%vm%"
IF "%vs%" NEQ "" SET es= -s "%vs%"
IF "%vo%" NEQ "" SET eo= -o "%vo%"
IF "%vn%" EQU "NO" SET en=
IF "%vn%" EQU "YES" SET en= -n
SET px=32
if "%PROCESSOR_ARCHITECTURE%"=="AMD64" SET px=64
TITLE MQTT 客户端测试工具 - %vip%:%vpo%
ECHO 下次以同样配置启动时，可以直接保存并使用以下命令启动：
ECHO ON
MqttClientTestTool_v1.3.2_Windows%px%.exe%el%%ip%%eca%%ece%%eck%%ecp%%ec%%et%%ew%%em%%es%%eo%%en%
GOTO START

:HELP
notepad README.md
GOTO START

:PEND
COLOR
CLS
ECHO ON

:END
