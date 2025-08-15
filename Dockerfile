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

# ==================== 新增：安装和配置 Locale ====================
RUN apt-get update && apt-get install -y locales && \
    # 生成 en_US.UTF-8 和 zh_CN.UTF-8 两种 locale
    sed -i -e 's/# en_US.UTF-8 UTF-8/en_US.UTF-8 UTF-8/' /etc/locale.gen && \
    sed -i -e 's/# zh_CN.UTF-8 UTF-8/zh_CN.UTF-8 UTF-8/' /etc/locale.gen && \
    locale-gen
# 设置全局默认的环境变量为 UTF-8
ENV LANG en_US.UTF-8
ENV LANGUAGE en_US:en
ENV LC_ALL en_US.UTF-8
# ================================================================

# 安装依赖
RUN sh conf/docker/apt/install/tools.sh
RUN sh conf/docker/apt/install/nodejs.sh
RUN sh conf/docker/apt/install/air.sh

# 编译服务端
RUN bash conf/docker/build/backend.sh

# 编译前端
RUN sh conf/docker/build/frontend.sh

# 我自己安装的插件
RUN apt-get update && apt-get install -y tree