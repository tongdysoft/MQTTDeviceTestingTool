package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"
)

func logInit() {
	if len(logData) > 0 {
		file, err := os.OpenFile(logData, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOGDATA"), err))
		} else {
			logDataF = file
			logPrint("i", fmt.Sprintf("%s: %s", lang("LOGDATA"), logData))
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
			logPrint("i", fmt.Sprintf("%s: %s", lang("LOGSTAT"), logStatus))
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
			logPrint("i", fmt.Sprintf("%s: %s", lang("LOG"), logFile))
			logFileE = true
			logFileStr(true, lang("START"), "", "")
		}
	}
}

func logPrint(iconChar string, text string) {
	var timeStr string = time.Now().Format(timeFormat)
	var log string = fmt.Sprintf("[%s][%s] %s\n", iconChar, timeStr, text)
	fmt.Print(log)
	if logFileE {
		write := bufio.NewWriter(logFileF)
		write.WriteString(log)
		write.Flush()
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
