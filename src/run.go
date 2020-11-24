package src

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/haifengat/goctp"
	ctp "github.com/haifengat/goctp/lnx"
	"github.com/sirupsen/logrus"

	"github.com/go-redis/redis/v8"

	"database/sql"

	_ "github.com/lib/pq" // postgres
)

// RealMd 实时行情
type RealMd struct {
	tradeFront, quoteFront, loginInfo, brokerID, investorID, password, appID, authCode string

	mapInstMin   sync.Map
	mapPushedMin sync.Map

	rdb        *redis.Client // redis 连接
	expireTime time.Time     // 数据过期时间,登录后赋值
	ctx        context.Context

	actionDay      string // 交易日起始交易日期
	actionDayNight string // 交易日起始交易日期-下一日

	t *ctp.Trade
	q *ctp.Quote

	waitLogin sync.WaitGroup // 等待登陆成功
}

// NewRealMd realmd 实例
func NewRealMd() (*RealMd, error) {
	r := new(RealMd)

	// 环境变量读取,赋值
	var tmp string
	if tmp = os.Getenv("tradeFront"); tmp == "" {
		return nil, errors.New("未配置环境变量：tradeFront")
	}
	r.tradeFront = tmp
	if tmp = os.Getenv("quoteFront"); tmp == "" {
		return nil, errors.New("未配置环境变量: quoteFront")
	}
	r.quoteFront = tmp
	if tmp = os.Getenv("loginInfo"); tmp == "" {
		return nil, errors.New("未配置环境变量: loginInfo")
	}
	r.loginInfo = tmp

	fs := strings.Split(r.loginInfo, "/")
	r.brokerID, r.investorID, r.password, r.appID, r.authCode = fs[0], fs[1], fs[2], fs[3], fs[4]
	if !strings.HasPrefix(r.tradeFront, "tcp://") {
		r.tradeFront = "tcp://" + r.tradeFront
	}
	if !strings.HasPrefix(r.quoteFront, "tcp://") {
		r.quoteFront = "tcp://" + r.quoteFront
	}

	var redisAddr = ""
	if tmp = os.Getenv("redisAddr"); tmp == "" {
		return nil, errors.New("未配置环境变量: redisAddr")
	}
	redisAddr = tmp

	logrus.Info(redisAddr)
	r.rdb = redis.NewClient(&redis.Options{
		Addr:     redisAddr,
		Password: "", // no password set
		DB:       0,  // use default DB
		PoolSize: 100,
	})
	r.ctx = context.Background()
	pong, err := r.rdb.Ping(r.ctx).Result()
	if err != nil {
		logrus.Error(pong, err)
		return nil, err
	}

	r.t = ctp.NewTrade()
	r.q = ctp.NewQuote()
	r.ctx = context.Background()
	return r, nil
}

func (r *RealMd) onTick(data *goctp.TickField) {
	if bs, err := json.Marshal(data); err == nil {
		// println(string(bs))
		go r.runTick(bs)
	} else {
		logrus.Infoln("ontick")
	}
}

func (r *RealMd) runTick(bs []byte) {
	mapTick := make(map[string]interface{})
	json.Unmarshal(bs, &mapTick)
	// strconv.ParseFloat(fmt.Sprintf("%.2f", 9.815), 64)  sprintf四舍五入采用 奇舍偶入的规则
	inst, updateTime, last, volume, oi := mapTick["InstrumentID"].(string), mapTick["UpdateTime"].(string), mapTick["LastPrice"].(float64), int(mapTick["Volume"].(float64)), mapTick["OpenInterest"].(float64)
	last, _ = strconv.ParseFloat(fmt.Sprintf("%.4f", last), 64)
	// 取tick的分钟构造当前分钟时间
	if r.actionDay == "" { // 取第一个actionday不为空的数据
		ac := mapTick["ActionDay"].(string)
		if len(ac) == 0 {
			return
		}
		if hour, _ := strconv.Atoi(updateTime[0:2]); hour <= 3 { //夜盘时应用开启
			r.actionDayNight = ac
			if nextDay, err := time.Parse("20060102", r.actionDayNight); err == nil {
				r.actionDay = nextDay.AddDate(0, 0, -1).Format("20060102")
			}
		} else {
			r.actionDay = ac
			if day, err := time.Parse("20060102", r.actionDay); err == nil {
				r.actionDayNight = day.AddDate(0, 0, 1).Format("20060102")
			}
		}
	}
	var action = r.actionDay
	// 夜盘
	if hour, _ := strconv.Atoi(updateTime[0:2]); hour <= 3 {
		action = r.actionDayNight
	}
	minDateTime := fmt.Sprintf("%s-%s-%s %s:00", action[0:4], action[4:6], action[6:], updateTime[0:5])

	mapMin := make(map[string]interface{}, 0)
	// 合约
	if obj, loaded := r.mapInstMin.Load(inst); !loaded {
		// 首次赋值
		mapMin["_id"] = minDateTime
		mapMin["Open"], mapMin["High"], mapMin["Low"], mapMin["Close"] = last, last, last, last
		mapMin["Volume"] = 0
		mapMin["preVol"] = volume
		mapMin["OpenInterest"] = oi
		mapMin["TradingDay"] = mapTick["TradingDay"]
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
			if jsonBs, err := json.Marshal(mapMin); err != nil {
				logrus.Errorf("map min to json error: %v", err)
			} else {
				// 发布分钟数据
				r.rdb.Publish(r.ctx, "md."+inst, bs)
				// 当前分钟未被记录
				if curMin, ok := r.mapPushedMin.LoadOrStore(inst, minDateTime); !ok || curMin != minDateTime {
					r.mapPushedMin.Store(inst, minDateTime)
					err := r.rdb.RPush(r.ctx, inst, jsonBs).Err()
					if err != nil {
						logrus.Errorf("redis rpush error: %s %v", inst, err)
					} else if !ok { // 合约首次记录
						r.rdb.ExpireAt(r.ctx, inst, r.expireTime)
					}
				} else {
					err := r.rdb.LSet(r.ctx, inst, -1, jsonBs).Err()
					if err != nil {
						logrus.Errorf("redis lset error: %s %v", inst, err)
					}
				}
			}
		}
	}
	r.mapInstMin.Store(inst, mapMin)
}

func (r *RealMd) startQuote() {
	r.q.RegOnFrontConnected(func() {
		logrus.Infoln("quote connected")
		r.q.ReqLogin(r.investorID, r.password, r.brokerID)
	})
	r.q.RegOnRspUserLogin(func(login *goctp.RspUserLoginField, info *goctp.RspInfoField) {
		logrus.Infoln("quote login:", info)
		// r.q.ReqSubscript("au2012")
		for inst := range r.t.Instruments {
			r.q.ReqSubscript(inst)
		}
		logrus.Infof("subscript instrument count: %d", len(r.t.Instruments))
		r.waitLogin.Done()
	})
	r.q.RegOnTick(r.onTick)
	logrus.Infoln("connected to quote...")
	r.q.ReqConnect(r.quoteFront)
}

func (r *RealMd) startTrade() {
	logrus.Infoln("connected to trade...")
	r.t.RegOnFrontConnected(func() {
		logrus.Infoln("trade connected")
		go r.t.ReqLogin(r.investorID, r.password, r.brokerID, r.appID, r.authCode)
	})
	r.t.RegOnFrontDisConnected(func(reason int) {
		logrus.Infof("trade disconnected %d", reason)
	})
	r.t.RegOnRspUserLogin(func(login *goctp.RspUserLoginField, info *goctp.RspInfoField) {
		logrus.Infof("trade login info: %v", info)
		if info.ErrorID == 0 {
			// 过期时间
			d, _ := time.ParseInLocation("20060102", login.TradingDay, time.Local) // time.local保持时区一致
			t, _ := time.ParseDuration("18h30m")                                   // 交易日的 18:30 过期
			exTime := d.Add(t)
			rdsTime, _ := r.rdb.Time(r.ctx).Result()
			// 根据redis服务器时间计算出过期时间,避免时间差异导致数据直接过期
			r.expireTime = rdsTime.Add(exTime.Sub(time.Now()))
			logrus.Infof("redis time now is: %v, expire time is : %v", rdsTime, r.expireTime)
			go r.startQuote()
		}
	})
	r.t.RegOnRtnOrder(func(field *goctp.OrderField) {
		logrus.Infof("%v\n", field)
	})
	r.t.RegOnErrRtnOrder(func(field *goctp.OrderField, info *goctp.RspInfoField) {
		logrus.Infof("%v\n", info)
	})
	r.t.ReqConnect(r.tradeFront)
}

// Run 运行
func (r *RealMd) Run() {
	r.waitLogin.Add(1)
	go r.startTrade()
	logrus.Info("waiting for trade api logged and quote subscripted.")
	r.waitLogin.Wait()
	defer func() {
		logrus.Info("close api")
		r.t.Release()
		r.q.Release()
	}()
	for {
		var cntNotClose = 0
		var cntTrading = 0
		time.Sleep(1 * time.Minute) // 每分钟判断一次
		for _, status := range r.t.InstrumentStatuss {
			if status != goctp.InstrumentStatusClosed {
				cntNotClose++
			}
			if status == goctp.InstrumentStatusContinous {
				cntTrading++
			}
		}
		// 全关闭 or 3点前全都为非交易状态
		if cntNotClose == 0 {
			// 保存分钟数据到pg
			pgMin := os.Getenv("pgMin")
			if db, err := sql.Open("postgres", pgMin); err == nil {
				// 退出时关闭
				defer db.Close()
				if keys := r.rdb.Keys(r.ctx, "*"); keys.Err() == nil {
					insts, _ := keys.Result()
					sqlStr := `INSERT INTO future.future_min ("DateTime", "Instrument", "Open", "High", "Low", "Close", "Volume", "OpenInterest", "TradingDay") VALUES('%s', '%s', %.4f, %.4f, %.4f, %.4f, %d, %.4f, '%s');`
					// 开启入库事务
					tx, _ := db.Begin()
					for _, inst := range insts {
						if mins, err := r.rdb.LRange(r.ctx, inst, 0, -1).Result(); err == nil {
							for _, bytes := range mins {
								var bar = make(map[string]interface{})
								if err := json.Unmarshal([]byte(bytes), &bar); err == nil {
									s := fmt.Sprintf(sqlStr, bar["_id"], inst, bar["Open"], bar["High"], bar["Low"], bar["Close"], bar["Volume"], bar["OpenInterest"], bar["TradingDay"])
									tx.Exec(s)
								}
							}
						}
					}
					if err = tx.Commit(); err != nil {
						tx.Rollback()
						logrus.Error("分钟数据入库错误：", err)
					}
				} else {
					logrus.Error("取redis 合约错误：", err)
				}
			}
			break
		}
		if time.Now().Hour() <= 3 && cntTrading == 0 {
			break
		}
	}
}
