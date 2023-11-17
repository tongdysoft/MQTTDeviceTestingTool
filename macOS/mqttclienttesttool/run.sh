#!/bin/sh
lang=""
if [ `defaults read -g AppleLocale`x = "zh_CN"x ];then
lang=" -l cn"
fi
cd "`echo $0 | rev | cut -c8- | rev`"
# 如果需要添加参数，请在此行后面添加：
# If you need to add parameters, please add after this line:
./MqttClientTestTool_macOS64$lang
#
read -p "Press Command+Q to exit"
exit
