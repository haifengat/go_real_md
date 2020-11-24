FROM centos:centos8.2.2004 AS final

WORKDIR /app
COPY ./realmd ./
COPY ./lib/*.so ./
# 更新的数据
# RUN yum install -y wget; \
#  wget http://data.haifengat.com/calendar.csv;
COPY ./calendar.csv ./
ENV LD_LIBRARY_PATH /app

#USER app-runner
ENTRYPOINT ["./realmd"]
