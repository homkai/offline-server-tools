## 描述
实时同步开发代码、编译产出，到线下机器（开发机、测试机），触发部署脚本，并将远程服务stdout实时同步过来

## 背景与用途
本地开发因为办公网限制，无法连db及上下游服务；堡垒机开发通过远程桌面诸多不便还挺卡。
可以用这个工具，在你超高配的本地电脑开发，同步代码或者编译产出到线下机器，再触发部署、重启服务，还能实时看远程服务的控制台输出信息

## 使用方便
### 本地
- 修改syncds-client.yml（推荐将其放在代码库根目录并ignore）
- ./syncds.exe client
- 跨平台，windows、mac、linux
- 两种思路：同步源码还是同步编译产出？如果本地可以编译，本地编译可能更好使

### 目标服务器
- 修改syncds-server.yml
- ./syncds server
- 推荐后台启动 nohup syncds server 2>&1 &


## 特色
- 基于http协议(websocket)传输，服务端可以使用安全策略开放的http端口
- 将远程deploy命令的stdout实时同步到本地，方便根据日志开发调试，避免本地和开发机之间频繁切换
- 支持web页面列出服务器的同步目录，方便查看文件列表和更新时间等，访问server.yml的http://ip:port
- 同步前根据md5预检查是否需要传输文件，LFU缓存

## 编译
- 如果go编译不方便，有win10 x64、linux x64的可执行文件供备用，在bin文件夹下
- 编译依赖 go get github.com/gorilla/websocket github.com/bluele/gcache github.com/spf13/cobra gopkg.in/yaml.v2
- 编译 go build -o syncds\[.exe\] \*.go