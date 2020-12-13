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
