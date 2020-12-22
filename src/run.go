package src

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/haifengat/goctp"
	ctp "github.com/haifengat/goctp/lnx"
	"github.com/lib/pq" // postgres
	"github.com/sirupsen/logrus"

	"github.com/go-redis/redis/v8"

	"database/sql"
)

// RealMd 实时行情
type RealMd struct {
	tradeFront, quoteFront, loginInfo, brokerID, investorID, password, appID, authCode string

	instLastMin         sync.Map // 合约:map[string]interface{},最后1分钟数据
	mapInstrumentStatus sync.Map // 合约交易状态

	rdb *redis.Client   // redis 连接
	ctx context.Context // redis 上下文

	actionDay     string // 交易日起始交易日期
	actionDayNext string // 交易日起始交易日期-下一日

	t *ctp.Trade
	q *ctp.Quote

	chLogin  chan bool // 等待登陆成功
	showTime string    // 显示当前tick时间
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
		Addr:         redisAddr,
		Password:     "",  // no password set
		DB:           0,   // use default DB
		PoolSize:     100, // 连接池最大socket连接数，默认为4倍CPU数， 4 * runtime.NumCPU
		MinIdleConns: 10,  //在启动阶段创建指定数量的Idle连接，并长期维持idle状态的连接数不少于指定数量；
		//超时
		DialTimeout:  5 * time.Second, //连接建立超时时间，默认5秒。
		ReadTimeout:  3 * time.Second, //读超时，默认3秒， -1表示取消读超时
		WriteTimeout: 3 * time.Second, //写超时，默认等于读超时
		PoolTimeout:  3 * time.Second, //当所有连接都处在繁忙状态时，客户端等待可用连接的最大等待时长，默认为读超时+1秒
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
	r.chLogin = make(chan bool)
	return r, nil
}

func (r *RealMd) inserrtPg() (err error) {
	pgMin := os.Getenv("pgMin")
	var db *sql.DB
	if db, err = sql.Open("postgres", pgMin); err != nil {
		logrus.Error("pgMin 配置错误:", err)
		return
	}
	// 退出时关闭
	defer db.Close()
	time.Sleep(10 * time.Second) // 给数据入库留出时间
	logrus.Info("当前交易日已收盘,redis分钟数据入postgres库.")
	var keys = []string{}
	if keys, err = r.rdb.Keys(r.ctx, "*").Result(); err != nil {
		logrus.Error("取redis 合约错误：", err)
		return
	}
	// 使用事务
	var txn *sql.Tx
	if txn, err = db.Begin(); err != nil {
		logrus.Error("begin 错误:", err)
		return
	}
	i := 0
	defer func(i *int) {
		if err = txn.Commit(); err != nil {
			txn.Rollback()
			logrus.Error("分钟入库tnx.commit错误:", err)
		} else {
			logrus.Info("入库:", *i)
		}
	}(&i)
	// 使用copy
	var stmt *sql.Stmt
	if stmt, err = txn.Prepare(pq.CopyInSchema("future", "future_min", "DateTime", "Instrument", "Open", "High", "Low", "Close", "Volume", "OpenInterest", "TradingDay")); err != nil {
		logrus.Error("prepare 错误:", err)
		return
	}
	for _, inst := range keys {
		// 合约过滤
		if _, ok := r.t.Instruments.Load(inst); !ok {
			continue
		}
		var mins = []string{}
		if mins, err = r.rdb.LRange(r.ctx, inst, 0, -1).Result(); err != nil {
			logrus.Error("取redis数据错误:", inst, err)
			return
		}
		for _, bsMin := range mins {
			var bar = make(map[string]interface{})
			if err = json.Unmarshal([]byte(bsMin), &bar); err != nil {
				logrus.Error("解析bar错误:", bar, " ", err)
				continue
			}
			// 过滤空指针的数据(double.MAX)
			if bar["High"].(float64) >= math.MaxFloat32 {
				continue
			}
			// 入库
			if _, err = stmt.Exec(bar["_id"], inst, bar["Open"], bar["High"], bar["Low"], bar["Close"], int(bar["Volume"].(float64)), bar["OpenInterest"], bar["TradingDay"]); err != nil {
				logrus.Errorf("分钟入库smtp.exec(fields)错误: %d, %s, %v, %v", i, inst, bar, err)
				// return 遇到错误，只提示不处理
			}
			i++
		}
	}
	if _, err = stmt.Exec(); err != nil {
		logrus.Error("分钟入库smtp.exec错误:", err)
		return
	}
	if err = stmt.Close(); err != nil {
		logrus.Error("分钟入库smtp.close错误:", err)
		return
	}
	return
}

// Run 运行
func (r *RealMd) Run() {
	// r.inserrtPg()
	// return
	// r.waitLogin.Add(1)
	go r.startTrade()
	logrus.Info("waiting for trade api logged and quote subscripted.")
	// r.waitLogin.Wait()
	<-r.chLogin // 等待登录结束
	go r.startQuote()
	defer func() {
		logrus.Info("close api")
		r.t.Release()
		r.q.Release()
	}()
	for {
		var cntNotClose = 0
		var cntTrading = 0
		time.Sleep(1 * time.Minute) // 每分钟判断一次
		r.t.InstrumentStatuss.Range(func(k, v interface{}) bool {
			status := v.(goctp.InstrumentStatus)
			if status.InstrumentStatus != goctp.InstrumentStatusClosed {
				cntNotClose++
			}
			if status.InstrumentStatus == goctp.InstrumentStatusContinous {
				cntTrading++
			}
			return true
		})
		// 全关闭 or 3点前全都为非交易状态
		if cntNotClose == 0 {
			if err := r.inserrtPg(); err == nil { // 保存分钟数据到pg
				r.rdb.FlushDB(r.ctx) // 清除当日数据
			} else {
				go func() {
					logrus.Error("入库错误，30分钟后清库：", err)
					time.Sleep(30 * time.Minute)
					r.rdb.FlushDB(r.ctx) // 清除当日数据
				}()
			}
			break
		}
		if time.Now().Hour() <= 3 && cntTrading == 0 {
			logrus.Info("夜盘结束!")
			break
		}
		if cntTrading > 0 {
			logrus.Info(r.showTime)
		}
	}
}
