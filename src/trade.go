package src

import (
	"encoding/json"
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
		// 更新所有合约状态
		r.t.Instruments.Range(func(key, value interface{}) bool {
			pid := value.(*goctp.InstrumentField).ProductID
			if len(pid) == 0 {
				return true
			}
			if status, loaded := r.mapInstrumentStatus.Load(pid); loaded {
				r.mapInstrumentStatus.Store(key, status)
			}
			return true
		})
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
	// 保存品种的状态：启动时instruments还未收到，需要记录此时状态以便后续处理
	r.mapInstrumentStatus.Store(field.InstrumentID, field.InstrumentStatus)
	// 更新对应合约的交易状态
	r.t.Instruments.Range(func(key, value interface{}) bool {
		if strings.Compare(value.(*goctp.InstrumentField).ProductID, field.InstrumentID) == 0 {
			r.mapInstrumentStatus.Store(key, field.InstrumentStatus)

			// 非交易状态
			if field.InstrumentStatus != goctp.InstrumentStatusContinous {
				// 取最后一个K线数据，如果时间为结束时间，则删除
				if jsonMin, err := r.rdb.LRange(r.ctx, key.(string), -1, -1).Result(); err == nil && len(jsonMin) > 0 {
					var bar = Bar{}
					if err := json.Unmarshal([]byte(jsonMin[0]), &bar); err == nil {
						// 时间为结束时间
						if strings.Compare(strings.Split(bar.ID, " ")[1], field.EnterTime) == 0 {
							// 删除此分钟的数据
							r.rdb.RPop(r.ctx, key.(string))
						}
					}
				}
			}
		}
		return true
	})
}
