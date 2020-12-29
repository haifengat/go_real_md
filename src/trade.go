package src

import (
	"strings"
	"sync"
	"time"

	"github.com/haifengat/goctp"
	"github.com/sirupsen/logrus"
)

func (r *RealMd) startTrade() {
	logrus.Infoln("connected to trade...")
	r.t.RegOnFrontConnected(r.onConnected)
	r.t.RegOnFrontDisConnected(r.onDisConnected)
	r.t.RegOnRspUserLogin(r.onLogin)
	// r.t.RegOnRtnOrder(func(field *goctp.OrderField) {
	// 	logrus.Infof("%v\n", field)
	// })
	// r.t.RegOnErrRtnOrder(func(field *goctp.OrderField, info *goctp.RspInfoField) {
	// 	logrus.Infof("%v\n", info)
	// })
	// 状态更新进行封装 ***************
	r.t.RegOnRtnInstrumentStatus(r.onRtnStatus)
	r.t.ReqConnect(r.tradeFront)
}

func (r *RealMd) onLogin(login *goctp.RspUserLoginField, info *goctp.RspInfoField) {
	logrus.Infof("trade login info: %v", info)
	if info.ErrorID == 0 {
		r.actionDay = "" // 初始化
		r.instLastMin = sync.Map{}
		// r.minPushed = sync.Map{}
		// r.mapInstrumentStatus = sync.Map{} // 会导致收不到行情：登录事件时交易状态已更新
		// 初始化 actionday
		if t, err := time.Parse("20060102", login.TradingDay); err == nil {
			preDay, _ := r.rdb.HGet(r.ctx, "tradingday", "curday").Result()
			if strings.Compare(preDay, login.TradingDay) != 0 {
				r.rdb.FlushAll(r.ctx)
				r.rdb.HSet(r.ctx, "tradingday", "curday", login.TradingDay)
			}
			if t.Weekday() == time.Monday { // 周一
				r.actionDay = t.AddDate(0, 0, -3).Format("20060102")     // 上周五
				r.actionDayNext = t.AddDate(0, 0, -2).Format("20060102") // 上周六
			} else {
				r.actionDay = t.AddDate(0, 0, -1).Format("20060102") // 上一天
				r.actionDayNext = login.TradingDay                   // 本日
			}
		} else {
			logrus.Error("日期字段错误：", login.TradingDay)
		}
	}
	// 登录响应
	r.chLogin <- true
}

func (r *RealMd) onConnected() {
	logrus.Infoln("trade connected")
	go r.t.ReqLogin(r.investorID, r.password, r.brokerID, r.appID, r.authCode)
}

func (r *RealMd) onDisConnected(reason int) {
	logrus.Error("trade disconnected: ", reason)
}

func (r *RealMd) onRtnStatus(field *goctp.InstrumentStatus) {
}
