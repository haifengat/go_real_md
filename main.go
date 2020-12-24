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
		if cmp == 0 { //当前为交易日
			// 8:45之前等待
			if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 08:45:00", curDate), time.Local); time.Now().Before(startTime) {
				logrus.Info("waiting for trading start at ", startTime)
				time.Sleep(startTime.Sub(time.Now()))
			} // 15:00前开启
			if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 15:00:00", curDate), time.Local); time.Now().Before(startTime) {
				if md, err := src.NewRealMd(); err != nil {
					logrus.Error("day 接口生成错误:", err)
					break
				} else {
					md.Run() // 交易所关闭后 or 夜盘结束 退出
				}
			}
			// 下一交易日在3天之后=》无夜盘
			if currentTradingDay, _ := time.ParseInLocation("20060102", curDate, time.Local); strings.Compare(tradingDays[i+1], currentTradingDay.AddDate(0, 0, 3).Format("20060102")) > 0 {
				continue
			}
			// 20:45:00前一直等待(前有效时间至20:30:00)
			if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 20:45:00", curDate), time.Local); time.Now().Before(startTime) {
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
			curDate = time.Now().Format("20060102")
		} else if cmp > 0 { // 不为交易日:周末
			// 等待下一交易日的 08:30:00
			nextDay, _ := time.ParseInLocation("20060102", day, time.Local)
			nextDay = nextDay.Add(8 * time.Hour).Add(30 * time.Minute)
			logrus.Info("wait for next tradingtime ", nextDay)
			time.Sleep(nextDay.Sub(time.Now())) // 周末后，卡在此处,解决：用ParseInLocation替代Parse
			curDate = time.Now().Format("20060102")
		}
	}
}
