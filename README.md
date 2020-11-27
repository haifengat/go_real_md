# go_real_md

### 介绍
golang 接收CTP实时行情

### 软件架构


### 安装教程

### 使用说明
连接CTP接口接收行情数据，用收到的tick生成分钟数据保存在reids中。收盘时redis中的数据复制到min的postgres中。

#### 环境变量
变量|默认值|说明
-|-|-
tradeFront|tcp://180.168.146.187:10130|ctp交易前置
quoteFront|tcp://180.168.146.187:10131|ctp行情前置
loginInfo|9999/008107/1/simnow_client_test/0000000000000000|登录配置格式 broker/investor/pwd/appid/authcode
redisAddr|127.0.0.1:6379|redis库配置host:port
pgMin|127.0.0.1:5432|分钟pg库配置

### 测试
tradeFront=tcp://180.168.146.187:10130 quoteFront=tcp://180.168.146.187:10131 \
loginInfo="9999/008107/1/simnow_client_test/0000000000000000" \
redisAddr=172.19.129.98:16379 \
pgMin=postgresql://postgres:12345@172.19.129.98:20032/postgres?sslmode=disable \
go run main.go

### 生成镜像
```bash
# 先编码再做镜像(要用centos基础镜像)
go build -o realmd
docker build -t haifengat/go_real_md:`date +%Y%m%d` .
# hub.docker.com
docker push haifengat/go_real_md:`date +%Y%m%d` .
# harbor
docker tag haifengat/go_real_md:`date +%Y%m%d` harbor.do.io/haifengat/go_real_md:`date +%Y%m%d` && docker push harbor.do.io/haifengat/go_real_md:`date +%Y%m%d`
# aliyun
docker login --username=hubert28@qq.com registry-vpc.cn-shanghai.aliyuncs.com && \
docker tag haifengat/go_real_md:`date +%Y%m%d` registry-vpc.cn-shanghai.aliyuncs.com/haifengat/go_real_md:`date +%Y%m%d` \
&& docker push registry-vpc.cn-shanghai.aliyuncs.com/haifengat/go_real_md:`date +%Y%m%d`
```