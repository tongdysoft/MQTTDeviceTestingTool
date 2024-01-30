package main

import (
	"bufio"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/logrusorgru/aurora"
	mqtt "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/packets"
)

func logInit(listen string, soft string) {
	if len(logData) > 0 && autoDelete(logData) {
		file, err := os.OpenFile(logData, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOGDATA"), err))
		} else {
			logDataF = file
			logPrint("C", fmt.Sprintf("%s: %s", lang("LOGDATA"), logData))
			logDataE = true
			logFileStr(false, listen, soft, lang("START"))
		}
	}
	if len(logStatus) > 0 && autoDelete(logStatus) {
		file, err := os.OpenFile(logStatus, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOGSTAT"), err))
		} else {
			logStatusF = file
			logPrint("C", fmt.Sprintf("%s: %s", lang("LOGSTAT"), logStatus))
			logStatusE = true
			logFileStr(true, listen, lang("START"), soft)
		}
	}
	if len(logFile) > 0 && autoDelete(logFile) {
		file, err := os.OpenFile(logFile, os.O_WRONLY|os.O_CREATE, 0666)
		if err != nil {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOG"), err))
		} else {
			logFileF = file
			logPrint("C", fmt.Sprintf("%s: %s", lang("LOG"), logFile))
			logFileE = true
		}
	}
}

func autoDelete(filePath string) bool {
	err := os.Remove(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			logPrint("X", fmt.Sprintf("%s %s: %s", lang("LOGFAIL"), lang("LOG"), err))
			return false
		}
		return true
	}
	return true
}

func logPrint(iconChar string, text string, titleMore ...string) {
	var timeStr string = time.Now().Format(timeFormat)
	var more string = ""
	if len(titleMore) > 0 {
		more = fmt.Sprintf("[%s]", strings.Join(titleMore, "]["))
	}
	var log0 string = fmt.Sprintf("[%s][%s]%s", iconChar, timeStr, more)
	var log1 string = fmt.Sprintf(" %s\n", text)
	if logFileE {
		write := bufio.NewWriter(logFileF)
		write.WriteString(log0)
		write.WriteString(log1)
		write.Flush()
	}
	if monochrome {
		fmt.Print(log0 + log1)
		return
	}
	switch iconChar {
	case "M": // 信息
		if isWindows {
			color.New(color.BgWhite, color.FgBlack).Print(log0)
			color.New(color.BgBlack, color.FgWhite).Print(log1)
		} else {
			fmt.Print(aurora.BgWhite(aurora.Black(log0)))
			fmt.Print(aurora.BgBlack(aurora.White(log1)))
		}
	case "C": // 配置
		if isWindows {
			color.New(color.BgMagenta, color.FgWhite).Print(log0)
			color.New(color.BgBlack, color.FgMagenta).Print(log1)
		} else {
			fmt.Print(aurora.BgMagenta(aurora.White(log0)))
			fmt.Print(aurora.BgBlack(aurora.Magenta(log1)))
		}
	case "L": // 连接
		if isWindows {
			color.New(color.BgGreen, color.FgBlack).Print(log0)
			color.New(color.BgBlack, color.FgGreen).Print(log1)
		} else {
			fmt.Print(aurora.BgGreen(aurora.Black(log0)))
			fmt.Print(aurora.BgBlack(aurora.Green(log1)))
		}
	case "D": // 断开
		if isWindows {
			color.New(color.BgRed, color.FgWhite).Print(log0)
			color.New(color.BgBlack, color.FgRed).Print(log1)
		} else {
			fmt.Print(aurora.BgRed(aurora.White(log0)))
			fmt.Print(aurora.BgBlack(aurora.Red(log1)))
		}
	case "S": // 订阅
		if isWindows {
			color.New(color.BgCyan, color.FgBlack).Print(log0)
			color.New(color.BgBlack, color.FgCyan).Print(log1)
		} else {
			fmt.Print(aurora.BgCyan(aurora.Black(log0)))
			fmt.Print(aurora.BgBlack(aurora.Cyan(log1)))
		}
	case "U": // 取消订阅
		if isWindows {
			color.New(color.BgYellow, color.FgBlack).Print(log0)
			color.New(color.BgBlack, color.FgYellow).Print(log1)
		} else {
			fmt.Print(aurora.BgYellow(aurora.Black(log0)))
			fmt.Print(aurora.BgBlack(aurora.Yellow(log1)))
		}
	case "X": // 错误
		if isWindows {
			color.New(color.BgRed, color.FgWhite).Print(log0)
			color.New(color.BgBlack, color.FgRed).Print(log1)
		} else {
			fmt.Print(aurora.BgRed(aurora.White(log0)))
			fmt.Print(aurora.BgBlack(aurora.Red(log1)))
		}
	default:
		if isWindows {
			color.New(color.Reset).Print(log0)
			fmt.Print(log1)
		} else {
			fmt.Print(log0)
			fmt.Print(log1)
		}
	}
}

func pkFilters(pk packets.Subscriptions) string {
	var filters []string
	for _, filter := range pk {
		filters = append(filters, string(filter.Filter))
	}
	return strings.Join(filters, ",")
}

func logFileStr(isStatus bool, infos ...string) {
	var timeStr string = ""
	tn := time.Now()
	if fileTimestamp {
		timeStr = fmt.Sprintf("%d", tn.UnixNano())
	} else {
		timeStr = time.Now().Format(timeFormat)
	}
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

func strCL(cl *mqtt.Client) string {
	var userName string = string(cl.Properties.Username)
	if len(userName) > 0 {
		userName += "@"
	}
	return fmt.Sprintf("%s%s(%s)", userName, cl.ID, cl.Net.Remote)
}

func in(strArr *[]string, str *string) bool {
	sort.Strings(*strArr)
	index := sort.SearchStrings(*strArr, *str)
	if index < len(*strArr) && (*strArr)[index] == *str {
		return true
	}
	return false
}
