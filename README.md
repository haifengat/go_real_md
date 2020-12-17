# go_real_md

### 介绍
golang 接收CTP实时行情

### 软件架构
1. 采用goctp接口订阅行情
2. 接收后合成分钟数据落入redis
3. 以md.{instrumentid}发布分钟数据，应用端可订阅后接收分钟数据。
4. 收盘后分钟数据保存至postgres数据库中

### 分钟处理
* 只处理处于可交易状态的品种（会过滤掉开/收时的tick）
* 处理actionDay
  * tradingday前一**交易日**为actionDay
  * actionDay下一**自然日**为actionDayNight
  * hour>=17 取 actionDay
  * hour<=3  取 actionDayNight
  * hour其他 取 tradingDay
* 分钟Volume
  * preVol前一分钟最后tick的Volume
  * 当前分钟的Volume = tick.Volume-preVol

### 安装教程
#### DockerFile
```dockerfile
# 先编码再做镜像(要用centos基础镜像)
# go build -o realmd
# docker build -t haifengat/go_real_md:`date +%Y%m%d` .
FROM centos:centos8.2.2004 AS final

WORKDIR /app
COPY ./realmd ./
COPY ./lib/*.so ./
# 交易日历，每年更新一次
RUN yum install -y wget; \
 wget http://data.haifengat.com/calendar.csv;
# COPY ./calendar.csv ./
ENV LD_LIBRARY_PATH /app

#USER app-runner
ENTRYPOINT ["./realmd"]
```

### 使用说明
#### 环境变量
变量|默认值|说明
-|-|-
tradeFront|tcp://180.168.146.187:10130|ctp交易前置
quoteFront|tcp://180.168.146.187:10131|ctp行情前置
loginInfo|9999/008107/1/simnow_client_test/0000000000000000|登录配置格式 broker/investor/pwd/appid/authcode
redisAddr|127.0.0.1:6379|redis库配置host:port
pgMin|127.0.0.1:5432|分钟pg库配置

### 生成镜像
```bash
# 先编码再做镜像(要用centos基础镜像)
go build -o realmd
docker build -t haifengat/go_real_md:`date +%Y%m%d` .
# hub.docker.com
docker push haifengat/go_real_md:`date +%Y%m%d`
# aliyun
docker login --username=hubert28@qq.com registry-vpc.cn-shanghai.aliyuncs.com && \
docker tag haifengat/go_real_md:`date +%Y%m%d` registry-vpc.cn-shanghai.aliyuncs.com/haifengat/go_real_md:`date +%Y%m%d` \
&& docker push registry-vpc.cn-shanghai.aliyuncs.com/haifengat/go_real_md:`date +%Y%m%d`
```

## 附
### 行情订阅后收不到ontick响应
原因：交易所状态处理问题
处理：已修复

### 接口断开重连，收不到login响应
原因：猜测为匿名函数被回收
解决：实际函数替代匿名函数

### 收盘时间的tick仍被处理
双tick仍无法避免，即15:00:00时收到2两个tick。例：y2105 20201214
解决：3tick

### concurrent map read and map write
原因是mapMin变量用map[string]interface{}保存分钟数据，在lastInstMin读取时冲突
解决：改为Bar{}
