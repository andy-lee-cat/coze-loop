FROM golang:1.23.4

# ==================== 新增代理配置 ====================
ARG http_proxy
ARG https_proxy
ENV http_proxy=$http_proxy
ENV https_proxy=$https_proxy
# ========================================================

ARG RUN_MODE=dev
ENV RUN_MODE=${RUN_MODE}

ENV GOPROXY=https://goproxy.cn,https://proxy.golang.org,direct

WORKDIR /cozeloop

COPY . .

# 基础依赖源设置
RUN sh conf/docker/apt/source/apply.sh

# 安装依赖
RUN sh conf/docker/apt/install/tools.sh
RUN sh conf/docker/apt/install/nodejs.sh
RUN sh conf/docker/apt/install/air.sh

# 编译服务端
RUN bash conf/docker/build/backend.sh

# 编译前端
RUN sh conf/docker/build/frontend.sh