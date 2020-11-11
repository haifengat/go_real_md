package main

import (
	"encoding/csv"
	"fmt"
	"os"
	"realmd/realmd"
	"sort"
	"strings"
	"sync"
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
	// 7*24
	curDate := time.Now().Format("20060102")
	var wgTradingPause sync.WaitGroup // 等待交易停止
	for i, day := range tradingDays {
		cmp := strings.Compare(day, curDate)
		if cmp == 0 { //当前为交易日
			// 8:45之前等待
			if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 08:45:00", curDate), time.Local); time.Now().Before(startTime) {
				logrus.Infof("waiting for trading start at %v", startTime)
				time.Sleep(startTime.Sub(time.Now()))
			}
			// 15:00前开启
			if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 15:45:00", curDate), time.Local); time.Now().Before(startTime) {
				wgTradingPause.Add(1)
				go func() {
					realmd.NewRealMd().Run() // 交易所关闭后会退出
					wgTradingPause.Done()
				}()
				logrus.Info("waiting for trading close...")
				wgTradingPause.Wait()
			}
			// 有夜盘(下一交易日在当前日的3天(含)内) ==> 等待夜盘开启
			if cur, _ := time.Parse("20060102", curDate); strings.Compare(tradingDays[i+1], cur.AddDate(0, 0, 3).Format("20060102")) > 0 {
				continue
			}
			// 20:45:00前一直等待(前有效时间至20:30:00)
			if startTime, _ := time.ParseInLocation("20060102 15:04:05", fmt.Sprintf("%s 20:45:00", curDate), time.Local); time.Now().Before(startTime) {
				logrus.Infof("waiting for night open at %v", startTime)
				time.Sleep(startTime.Sub(time.Now()))
			}
			// 夜盘开启
			wgTradingPause.Add(1)
			go func() {
				realmd.NewRealMd().Run() // 交易所关闭后 or 夜盘结束 退出
				wgTradingPause.Done()
			}()
			logrus.Info("waiting for trading night close...")
			wgTradingPause.Wait()
			curDate = time.Now().Format("20060102")
		} else if cmp > 0 { // 不为交易日
			// 等待下一交易日日的 08:30:00
			nextDay, _ := time.Parse("20060102", day)
			nextDay = nextDay.Add(8 * time.Hour).Add(30 * time.Minute)
			logrus.Infof("wait for next tradingtime %s.", nextDay.Format("20060102 15:04:05"))
			time.Sleep(nextDay.Sub(time.Now()))
			curDate = time.Now().Format("20060102")
		}
	}
}
