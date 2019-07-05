package main

import (
	"bytes"
	"github.com/spf13/cobra"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
)


const tplClientConfig = `
# ip、端口 与server.yml一致
server: 127.0.0.1:8003
# 同步范围的root文件夹，建议将配置文件放在代码库根目录，base-dir: ./
base-dir: ./
# 监听触发间隔时间，毫秒
interval-ms: 3000
# 监听哪些文件、文件夹，相对与base-dir的路径
include-paths:
  - ./xx-app/target/xx-app.jar
  - ./xx-app/src/main/resouce
  - ./test
  - file.txt

# 选填，用正则来进一步从include-paths范围内限定文件，可留空
include-file-regexp: \.(yml|properties|jar)$
# 选填，用正则排除path，如idea编辑器的临时文件 ___jb_tmp___, ___jb_old___
exclude-path-regexp: (__)$
# 选填，触发deploy的path正则，如果不填则所有文件改动都触发deploy
deploy-path-regexp: \.jar$
# 部署脚本、重启服务命令，支持本地实时滚动deploy命令的stdout、stderr
# deploy-cmd: "ps -ef|grep xx-app.jar|awk '{print $2}'|xargs kill -9; java -jar xx-app/target/xx-app.jar"
# deploy-cmd: "java -agentlib:jdwp=transport=dt_socket,server=y,suspend=n,address=8644 -jar target/bard-admin-0.0.1-SNAPSHOT.jar"
deploy-cmd: "java -jar xx-app/target/xx-app.jar"
`

const tplServerConfig = `
# ip要能client跟server在同一个局域网或外网，基于http服务传输，可以填安全允许的http端口
server: 127.0.0.1:8003
# 接收同步过来文件的root文件夹
base-dir: ./
# 是否开启http服务http://server，列出base-dir目录，方便查看文件列表及更新时间等
show-dir-list: true
`

const fileNameClientConfig = "syncds-client.yml"
const fileNameServerConfig = "syncds-server.yml"


func main() {

	var name string
	var isStart bool
	var isInit bool

	var cmdClient = &cobra.Command{
		Use:   "client",
		Short: "to start a client",
		Long: `to start a client. watch local files change. sync files and send deploy command to server`,
		Args: cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := os.Stat(fileNameClientConfig)
			if os.IsNotExist(err) && !isInit {
				log.Fatalf("config file %s not existed. please run `syncds client --name=%s --init` first.", fileNameClientConfig, name)
			}
			if isInit {
				if err == nil {
					log.Printf("file %s existed", fileNameClientConfig)
				} else {
					errWrite := ioutil.WriteFile(fileNameClientConfig, []byte(tplClientConfig), os.ModePerm)
					if errWrite != nil {
						log.Fatalf("conn't write file `%s` here, %v", fileNameClientConfig, errWrite)
					}
				}
				log.Printf("syncds client config `%s` initialized. please check the options.", fileNameClientConfig)
			} else if isStart {
				var conf ClientConf
				conf.getConf()
				StartClient(conf)
				log.Printf("syncds client start with name %s", name)
			}
		},
	}
	cmdClient.Flags().StringVarP(&name, "name", "n", "", "uniq serve name")
	cmdClient.Flags().BoolVarP(&isStart, "start", "s", true, "start serving")
	cmdClient.Flags().BoolVarP(&isInit, "init", "i", false, "init config")
	_ = cmdClient.MarkFlagRequired("name")

	var cmdServer = &cobra.Command{
		Use:   "server",
		Short: "to start a server",
		Long: `to start a server. listen to client. receive the change file and exec the deploy command`,
		Args: cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			_, err := os.Stat(fileNameServerConfig)
			if os.IsNotExist(err) && !isInit {
				log.Fatalf("config file %s not existed. please run `syncds server --name=%s --init` first.", fileNameServerConfig, name)
			}
			if isInit {
				if err == nil {
					log.Printf("file %s existed", fileNameServerConfig)
				} else {
					errWrite := ioutil.WriteFile(fileNameServerConfig, []byte(tplServerConfig), os.ModePerm)
					if errWrite != nil {
						log.Fatalf("conn't write file `%s` here, %v", fileNameServerConfig, errWrite)
					}
				}
				log.Printf("syncds server config `%s` initialized. please check the options.", fileNameServerConfig)
			} else if isStart {
				var conf ServerConf
				conf.getConf()
				StartServer(conf)
				log.Printf("syncds server start with name %s", name)
			}
		},
	}
	cmdServer.Flags().StringVarP(&name, "name", "n", "", "uniq serve name")
	cmdServer.Flags().BoolVarP(&isStart, "start", "s", true, "start serving")
	cmdServer.Flags().BoolVarP(&isInit, "init", "i", false, "init config")
	_ = cmdServer.MarkFlagRequired("name")

	var cmdStop = &cobra.Command{
		Use:   "stop",
		Short: "to stop a client or server",
		Long: `stop a client or server running before`,
		Args: cobra.MinimumNArgs(0),
		Run: func(cmd *cobra.Command, args []string) {
			kill := "ps -ef | grep 'syncds' | grep '" + name + "' | awk '{print $2}' | xargs kill -9"
			log.Println(kill)

			var stdout bytes.Buffer
			var stderr bytes.Buffer
			command := exec.Command("sh", "-c", kill)
			command.Stdout = &stdout
			command.Stderr = &stderr
			_ = command.Run()

			log.Printf("stop with result: %s %s", stdout.String(), stderr.String())
		},
	}
	cmdStop.Flags().StringVarP(&name, "name", "n", "", "uniq serve name")
	_ = cmdStop.MarkFlagRequired("name")

	var rootCmd = &cobra.Command{Use: "syncds"}
	rootCmd.AddCommand(cmdClient, cmdServer, cmdStop)
	err := rootCmd.Execute()
	if err != nil {
		log.Fatal("rootCmd err", err)
	}
}