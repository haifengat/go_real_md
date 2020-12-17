package src

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/haifengat/goctp"
	"github.com/sirupsen/logrus"
)

func (r *RealMd) startQuote() {
	r.q.RegOnFrontConnected(r.onMdConnected)
	r.q.RegOnRspUserLogin(r.onMdLogin)
	r.q.RegOnTick(r.onTick)
	logrus.Infoln("connected to quote...")
	r.q.ReqConnect(r.quoteFront)
}

func (r *RealMd) onTick(data *goctp.TickField) {
	if bs, err := json.Marshal(data); err == nil {
		// println(string(bs))
		go r.runTick(bs)
	} else {
		logrus.Infoln("ontick")
	}
}

func (r *RealMd) runTick(bsTick []byte) {
	mapTick := make(map[string]interface{})
	json.Unmarshal(bsTick, &mapTick)
	// strconv.ParseFloat(fmt.Sprintf("%.2f", 9.815), 64)  sprintf四舍五入采用 奇舍偶入的规则
	inst, updateTime, last, volume, oi := mapTick["InstrumentID"].(string), mapTick["UpdateTime"].(string), mapTick["LastPrice"].(float64), int(mapTick["Volume"].(float64)), mapTick["OpenInterest"].(float64)
	if last >= math.MaxFloat32 {
		return
	}
	// 合约状态过滤 == 会造成入库延时
	if status, loaded := r.mapInstrumentStatus.Load(inst); !loaded || status != goctp.InstrumentStatusContinous {
		return
	}
	last, _ = strconv.ParseFloat(fmt.Sprintf("%.4f", last), 64)
	// 取tick的分钟构造当前分钟时间
	var action = r.t.TradingDay
	// 夜盘
	hour, _ := strconv.Atoi(updateTime[0:2])
	if hour <= 3 {
		action = r.actionDayNext
	} else if hour >= 17 {
		action = r.actionDay
	}

	minDateTime := fmt.Sprintf("%s-%s-%s %s:00", action[0:4], action[4:6], action[6:], updateTime[0:5])

	r.showTime = minDateTime

	bar := Bar{}
	// 合约+时间
	key := fmt.Sprintf("%s_%s", inst, minDateTime)
	obj, _ := r.instMinTicks.LoadOrStore(key, 0)
	tickCnt := obj.(int)
	tickCnt++
	r.instMinTicks.Store(key, tickCnt)

	if obj, loaded := r.instLastMin.Load(inst); !loaded {
		// 首次赋值
		bar.ID = minDateTime
		bar.Open, bar.High, bar.Close, bar.Low = last, last, last, last
		bar.Volume = 0
		bar.PreVol = volume
		bar.OpenInterest = oi
		bar.TradingDay = r.t.TradingDay
	} else {
		bar = obj.(Bar)
		if strings.Compare(bar.ID, minDateTime) != 0 {
			bar.ID = minDateTime
			bar.Open, bar.High, bar.Close, bar.Low = last, last, last, last
			// 首个tick不计算成交量, 否则会导致隔夜的早盘第一个分钟的成交量非常大
			bar.PreVol = bar.PreVol + bar.Volume
			bar.Volume = volume - bar.PreVol
			bar.OpenInterest = oi
		} else { // 分钟数据更新
			const E = 0.000001
			if last-bar.High > E {
				bar.High = last
			}
			if last-bar.Low < E {
				bar.Low = last
			}
			bar.Close = last
			bar.Volume = volume - bar.PreVol
			bar.OpenInterest = oi

			// 此时间是否 push过
			if jsMin, err := json.Marshal(bar); err != nil {
				logrus.Errorf("map min to json error: %v", err)
			} else {
				// 当前分钟未被记录
				if tickCnt == 3 { // 控制分钟最小tick数量
					err := r.rdb.RPush(r.ctx, inst, jsMin).Err()
					if err != nil {
						logrus.Errorf("redis rpush error: %s %v", inst, err)
					}
					// 发布分钟数据
					r.rdb.Publish(r.ctx, "md."+inst, jsMin)
				} else if tickCnt > 3 {
					err := r.rdb.LSet(r.ctx, inst, -1, jsMin).Err()
					if err != nil {
						logrus.Errorf("redis lset error: %s %v", inst, err)
					}
					// 发布分钟数据
					r.rdb.Publish(r.ctx, "md."+inst, jsMin)
				}
			}
		}
	}
	r.instLastMin.Store(inst, bar)
}

// Bar 分钟K线
type Bar struct {
	ID           string `json:"_id"`
	TradingDay   string
	Open         float64
	High         float64
	Low          float64
	Close        float64
	Volume       int
	OpenInterest float64
	PreVol       int `json:"preVol"`
}

func (r *RealMd) onMdConnected() {
	logrus.Infoln("quote connected")
	r.q.ReqLogin(r.investorID, r.password, r.brokerID)
}

func (r *RealMd) onMdLogin(login *goctp.RspUserLoginField, info *goctp.RspInfoField) {
	logrus.Infoln("quote login:", info)
	// r.q.ReqSubscript("au2012")
	r.t.Instruments.Range(func(k, v interface{}) bool {
		// 取最新K线数据
		inst := k.(string)
		if jsonMin, err := r.rdb.LRange(r.ctx, inst, -1, -1).Result(); err == nil && len(jsonMin) > 0 {
			bar := Bar{}
			if json.Unmarshal([]byte(jsonMin[0]), &bar) == nil {
				r.instLastMin.Store(inst, bar)
			}
		}
		return true
	})
	i := 0
	// 订阅行情
	r.t.Instruments.Range(func(k, v interface{}) bool {
		r.q.ReqSubscript(k.(string))
		i++
		return true
	})
	logrus.Infof("subscript instrument count: %d", i)
	// r.waitLogin.Done() // negative WaitGroup counter
}
