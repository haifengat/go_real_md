package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"realmd/src"
	"sort"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

var tradingDays []string

// readCalendar 取交易日历
func readCalendar() {
	cal, err := os.Open("calendar.csv")
	defer cal.Close()
	if err != nil {
		logrus.Error(err)
	}
	reader := csv.NewReader(cal)
	lines, err := reader.ReadAll()
	for _, line := range lines {
		if len(line) == 0 {
			continue
		}
		if line[1] == "true" {
			tradingDays = append(tradingDays, line[0])
		}
	}
	sort.Strings(tradingDays)
}

func main() {
	readCalendar()
	// r, _ := src.NewRealMd()
	// r.Run()
	// return
	// 7*24
	curDate := time.Now().Format("20060102")
	for i, day := range tradingDays {
		cmp := strings.Compare(day, curDate)
		if cmp < 0 {
			continue
		}
		// 8:45之前等待
		if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 08:45:00", day), time.Local); time.Now().Before(startTime) {
			logrus.Info("waiting for trading start at ", startTime)
			time.Sleep(startTime.Sub(time.Now()))
		} // 15:00前开启
		if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 15:00:00", day), time.Local); time.Now().Before(startTime) {
			if md, err := src.NewRealMd(); err != nil {
				logrus.Error("day 接口生成错误:", err)
				break
			} else {
				md.Run() // 交易所关闭后 or 夜盘结束 退出
			}
		}
		// 当日有夜盘(下一交易日在3天内)
		if strings.Compare(tradingDays[i+1], time.Now().AddDate(0, 0, 3).Format("20060102")) <= 0 {
			if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 20:45:00", day), time.Local); time.Now().Before(startTime) {
				logrus.Info("waiting for night open at ", startTime)
				time.Sleep(startTime.Sub(time.Now()))
			}
			// 夜盘开启
			if md, err := src.NewRealMd(); err != nil {
				logrus.Error("night 接口生成错误:", err)
				break
			} else {
				md.Run() // 交易所关闭后 or 夜盘结束 退出
			}
		}
	}
}
