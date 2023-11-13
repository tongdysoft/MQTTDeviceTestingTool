REM charset: GB2312, line break: CRLF, version: 1.5.0
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
TITLE MQTT �ͻ��˲��Թ���
COLOR 17
ECHO OFF
CLS
ECHO ================================================== 
ECHO MQTT �ͻ��˲��Թ���
ECHO ==================================================
ECHO ���򽫴���һ�� MQTT ��������
ECHO �ڴ�����ɺ󣬿ɽ��豸����������м�ء�
ECHO �����������޸� MQTT ��������Ӧ�Ĳ�����
ECHO --------------------------------------------------
ECHO [ 0] ��Ϣ���ʹ�õ�����: %vl%
ECHO [ 1] MQTT ������ IP ��ַ: %vip%
ECHO [ 2] MQTT ������ �˿ں�: %vpo%
ECHO [ 3] �û�������Ȩ�������ļ�·��: %vu%
ECHO [ 4] CA ֤���ļ�·��: %ca%
ECHO [ 5] ��ǩ��֤���ļ�·��: %ce%
ECHO [ 6] ��ǩ��˽Կ�ļ�·��: %ck%
ECHO [ 7] ��ǩ��˽Կ�ļ�������: %cp%
ECHO [ 8] ֻ�����ͻ��� ID Ϊ��Щ�Ŀͻ���: %vc%
ECHO [ 9] ֻ������Щ�������Ϣ: %vt%
ECHO [10] ֻ����Ϣ���ݰ�����Щ�ؼ��ʲŻᴦ��: %vw%
ECHO [11] ����ȡ������Ϣ���浽 .csv �ļ�·��: %vm%
ECHO [12] ���ͻ���״̬�仯���浽 .csv �ļ�·��: %vs%
ECHO [13] ����־������浽 .log �ļ�·��: %vo%
ECHO [14] ��ɫģʽ���(����ͨ cmd ����ر�): %vn%
ECHO --------------------------------------------------
ECHO [Y] ���� MQTT ������
ECHO [H] ��ȡ����
ECHO [N] �˳�
ECHO ==================================================
SET /P v=��������: 

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
ECHO �������ѡ��:
ECHO [0] ��������
ECHO [1] Ӣ��
SET /P vl=��������: 
IF "%vl%" EQU "0" SET vl=cn
IF "%vl%" EQU "1" SET vl=en
GOTO START

:SET_VIP
CLS
ECHO ��ǰֵ: %vip%
ECHO �������ѡ�� IP ��ַ:
ECHO [0] ��������IP(0.0.0.0)
ECHO [1] ����(127.0.0.1)
ECHO [l] ����(localhost)
ECHO [c] �ɲ鿴��ǰ������Ϣ
ECHO [�ֶ�����] IP ��ַ
SET /P vit=IP ��ַ: 
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
ECHO ��ǰֵ: %vpo%
ECHO ������˿�:
SET /P vpo=�˿ںţ����֣�: 
IF "%vpo%" EQU "" SET vpo=1883
GOTO START

:SET_VU
CLS
ECHO ��ǰֵ: %vu%
ECHO �û�������Ȩ�������ļ�·��:
SET /P vu=�ļ�·�� (.json):
GOTO START

:SET_CA
CLS
ECHO ��ǰֵ: %ca%
ECHO CA ֤���ļ�·��:
SET /P ca=�ļ�·�� (.pem/.crt): 
GOTO START

:SET_CE
CLS
ECHO ��ǰֵ: %ce%
ECHO ��ǩ��֤���ļ�·��:
SET /P ce=�ļ�·�� (.pem/.crt): 
GOTO START

:SET_CK
CLS
ECHO ��ǰֵ: %ck%
ECHO ��ǩ��˽Կ�ļ�·��:
SET /P ck=�ļ�·�� (.key): 
GOTO START

:SET_CP
CLS
ECHO ��ǰֵ: %cp%
ECHO ��ǩ��˽Կ�ļ�������:
SET /P cp=����: 
GOTO START

:SET_VC
CLS
ECHO ��ǰֵ: %vc%
ECHO ֻ�����ͻ��� ID Ϊ��Щ�Ŀͻ���:
SET /P vc=�ͻ��� ID (Ӣ�Ķ��ŷָ�): 
GOTO START

:SET_VT
CLS
ECHO ��ǰֵ: %vt%
ECHO ֻ������Щ�������Ϣ:
SET /P vt=���� (Ӣ�Ķ��ŷָ�): 
GOTO START

:SET_VW
CLS
ECHO ��ǰֵ: %vw%
ECHO ֻ����Ϣ���ݰ�����Щ�ؼ��ʲŻᴦ��:
SET /P vw=�ؼ��� (Ӣ�Ķ��ŷָ�): 
GOTO START

:SET_VM
CLS
ECHO ��ǰֵ: %vm%
ECHO ����ȡ������Ϣ���浽 .csv �ļ�·��:
SET /P vm=�ļ�·�� (.csv): 
GOTO START

:SET_VS
CLS
ECHO ��ǰֵ: %vs%
ECHO ���ͻ���״̬�仯���浽 .csv �ļ�·��:
SET /P vs=�ļ�·�� (.csv): 
GOTO START

:SET_VO
CLS
ECHO ��ǰֵ: %vo%
ECHO ����־������浽 .log �ļ�·��:
SET /P vo=�ļ�·�� (.log): 
GOTO START

:SET_VN
CLS
ECHO ��ǰֵ: %vn%
ECHO ��ɫģʽ���:
ECHO [0] ��
ECHO [1] ��
SET /P vn=��������: 
IF "%vn%" EQU "0" SET vn=NO
IF "%vn%" EQU "1" SET vn=YES
GOTO START

:RUN
COLOR 07
COLOR
CLS
DATE /T
TIME /T
ECHO ��������...
ECHO �� Ctrl+C ���˳�����
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
TITLE MQTT �ͻ��˲��Թ��� - %vip%:%vpo%
ECHO �´���ͬ����������ʱ������ֱ�ӱ��沢ʹ����������������
ECHO ON
MqttClientTestTool_v1.5.0_Windows%px%.exe%el%%ip%%eca%%ece%%eck%%ecp%%ec%%et%%ew%%em%%es%%eo%%en%
GOTO START

:HELP
notepad README.md
GOTO START

:PEND
COLOR
CLS
ECHO ON

:END
