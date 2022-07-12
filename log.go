package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/logrusorgru/aurora"
)

func logInit() {
	if len(logData) > 0 {
		file, err := os.OpenFile(logData, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOGDATA"), err))
		} else {
			logDataF = file
			logPrint("C", fmt.Sprintf("%s: %s", lang("LOGDATA"), logData))
			logDataE = true
			logFileStr(true, lang("START"), "", "")
		}
	}
	if len(logStatus) > 0 {
		file, err := os.OpenFile(logStatus, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOGSTAT"), err))
		} else {
			logStatusF = file
			logPrint("C", fmt.Sprintf("%s: %s", lang("LOGSTAT"), logStatus))
			logStatusE = true
			logFileStr(true, lang("START"), "", "")
		}
	}
	if len(logFile) > 0 {
		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOG"), err))
		} else {
			logFileF = file
			logPrint("C", fmt.Sprintf("%s: %s", lang("LOG"), logFile))
			logFileE = true
			logFileStr(true, lang("START"), "", "")
		}
	}
}

func logPrint(iconChar string, text string) {
	var timeStr string = time.Now().Format(timeFormat)
	var log0 string = fmt.Sprintf("[%s][%s]", iconChar, timeStr)
	var log1 string = fmt.Sprintf(" %s\n", text)
	if logFileE {
		write := bufio.NewWriter(logFileF)
		write.WriteString(log0)
		write.WriteString(log1)
		write.Flush()
	}
	switch iconChar {
	case "M": // 信息
		fmt.Print(aurora.BgMagenta(log0))
		fmt.Print(aurora.Magenta(log1))
	case "C": // 配置
		fmt.Print(aurora.BrightWhite(log0))
		fmt.Print(aurora.BrightWhite(log1))
	case "L": // 连接
		fmt.Print(aurora.BgGreen(log0))
		fmt.Print(aurora.Green(log1))
	case "D": // 断开
		fmt.Print(aurora.BgYellow(log0))
		fmt.Print(aurora.Yellow(log1))
	case "S": // 订阅
		fmt.Print(aurora.BgCyan(log0))
		fmt.Print(aurora.Cyan(log1))
	case "U": // 取消订阅
		fmt.Print(aurora.BgBlue(log0))
		fmt.Print(aurora.Blue(log1))
	case "X": // 错误
		fmt.Print(aurora.BgRed(log0))
		fmt.Print(aurora.Red(log1))
	default:
		fmt.Print(log0)
		fmt.Print(log1)
	}
}

func logFileStr(isStatus bool, infos ...string) {
	var timeStr string = time.Now().Format(timeFormat)
	var infoArr []string = append([]string{timeStr}, infos...)
	var info string = "\"" + strings.Join(infoArr, "\",\"") + "\"\n"
	if isStatus {
		if logStatusE {
			write := bufio.NewWriter(logStatusF)
			write.WriteString(info)
			write.Flush()
		}
	} else {
		if logDataE {
			write := bufio.NewWriter(logDataF)
			write.WriteString(info)
			write.Flush()
		}
	}
}
