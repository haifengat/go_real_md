package src

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"

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
	// 行情结束价保存
	avg := mapTick["AveragePrice"].(float64)
	if avg < math.MaxFloat32 && avg > 0 {
		r.rdb.HSet(r.ctx, "avg", inst, avg)
	}

	minDateTime := fmt.Sprintf("%s-%s-%s %s:00", action[0:4], action[4:6], action[6:], updateTime[0:5])

	r.showTime = minDateTime

	mapMin := make(map[string]interface{})
	// 合约
	if obj, loaded := r.mapInstMin.Load(inst); !loaded {
		// 首次赋值
		mapMin["_id"] = minDateTime
		mapMin["Open"], mapMin["High"], mapMin["Low"], mapMin["Close"] = last, last, last, last
		mapMin["Volume"] = 0
		mapMin["preVol"] = volume
		mapMin["OpenInterest"] = oi
		mapMin["TradingDay"] = r.t.TradingDay // mapTick["TradingDay"]
	} else {
		mapMin = obj.(map[string]interface{})
		if mapMin["_id"] != minDateTime {
			mapMin["_id"] = minDateTime
			mapMin["Open"], mapMin["High"], mapMin["Low"], mapMin["Close"] = last, last, last, last
			// 首个tick不计算成交量, 否则会导致隔夜的早盘第一个分钟的成交量非常大
			mapMin["preVol"] = mapMin["preVol"].(int) + mapMin["Volume"].(int)
			mapMin["Volume"] = volume - mapMin["preVol"].(int)
			// mapMin["preVol"] = volume // 如何将前 1 tick数据保存?
			mapMin["OpenInterest"] = oi
		} else { // 分钟数据更新
			const E = 0.000001
			if last-mapMin["High"].(float64) > E {
				mapMin["High"] = last
			}
			if last-mapMin["Low"].(float64) < E {
				mapMin["Low"] = last
			}
			mapMin["Close"] = last
			mapMin["Volume"] = volume - mapMin["preVol"].(int)
			mapMin["OpenInterest"] = oi

			// 此时间是否 push过
			if jsMin, err := json.Marshal(mapMin); err != nil {
				logrus.Errorf("map min to json error: %v", err)
			} else {
				// 发布分钟数据
				r.rdb.Publish(r.ctx, "md."+inst, jsMin)
				// 当前分钟未被记录
				if curMin, ok := r.mapPushedMin.LoadOrStore(inst, minDateTime); !ok || curMin != minDateTime {
					r.mapPushedMin.Store(inst, minDateTime)
					err := r.rdb.RPush(r.ctx, inst, jsMin).Err()
					if err != nil {
						logrus.Errorf("redis rpush error: %s %v", inst, err)
					}
				} else {
					err := r.rdb.LSet(r.ctx, inst, -1, jsMin).Err()
					if err != nil {
						logrus.Errorf("redis lset error: %s %v", inst, err)
					}
				}
			}
		}
	}
	r.mapInstMin.Store(inst, mapMin)
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
			var min = make(map[string]interface{})
			if json.Unmarshal([]byte(jsonMin[0]), &min) == nil {
				min["preVol"] = int(min["preVol"].(float64))
				min["Volume"] = int(min["Volume"].(float64))
				r.mapInstMin.Store(inst, min)
				r.mapPushedMin.Store(inst, min["_id"])
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
