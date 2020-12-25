# 先编码再做镜像(要用centos基础镜像)
# go build -o realmd
# docker build -t haifengat/go_real_md:`date +%Y%m%d` .
# FROM centos:centos8.2.2004 AS final
# FROM ubuntu:20.04
# RUN apt update; \
#   apt install tzdata; \
#   rm /etc/localtime; \
#   ln -s /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
FROM busybox:glibc
WORKDIR /app
COPY ./Shanghai /usr/share/zoneinfo/Asia/
RUN ln -fs /usr/share/zoneinfo/Asia/Shanghai /etc/localtime
COPY ./realmd ./
COPY ./lib/*.so ./
# 交易日历，每年更新一次
# RUN yum install -y wget; \
#  wget http://data.haifengat.com/calendar.csv;
COPY ./calendar.csv ./
ENV LD_LIBRARY_PATH /app

COPY lib64 /lib64
#USER app-runner
ENTRYPOINT ["./realmd"]
