package main

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type UploadConfig struct {
	AppTitle string `json:"appTitle"`
	TemplateUrl string `json:"templateUrl"`
	TargetFileName string `json:"targetFileName"`
	Command string `json:"command"`
}

var configs map[string]UploadConfig

func setupRouter() *gin.Engine {
	r := gin.Default()
	r.Use(Cors())

	r.StaticFile("/", "public/index.html")
	// 支持列出目录，方便给一个文件夹地址
	r.StaticFS("/templates/", http.Dir("public/templates"))
	r.StaticFS("/results/", http.Dir("public/results"))

	r.GET("/config/:app", func(c *gin.Context) {
		loadConfig()
		app := c.Param("app")
		config, ok := configs[app]
		if !ok {
			c.String(http.StatusBadRequest, "app empty")
			return
		}
		c.JSON(http.StatusOK, config)
	})

	r.POST("/upload", func(c *gin.Context) {
		app := c.PostForm("app")
		if app == "" {
			c.String(http.StatusBadRequest, "app empty")
			return
		}
		config, ok := configs[app]
		if !ok {
			c.String(http.StatusBadRequest, "app invalid")
			return
		}

		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.String(http.StatusBadRequest, "上传文件出错")
			return
		}
		uploadExt := header.Filename[strings.LastIndex(header.Filename, "."):]
		configExt := config.TargetFileName[strings.LastIndex(config.TargetFileName, "."):]
		if uploadExt != configExt {
			c.String(http.StatusBadRequest, "请上传基于模板的 " + configExt + " 类型文件")
			return
		}

		// 转储文件，替换random
		targetFileName := produceFileName(config.TargetFileName)
		out, err := os.Create(targetFileName)
		if err != nil {
			log.Println(err)
			c.String(http.StatusBadRequest, "写文件出错")
			return
		}
		defer out.Close()
		_, err = io.Copy(out, file)
		if err != nil {
			log.Println(err)
			c.String(http.StatusBadRequest, "写文件出错")
			return
		}

		command := strings.Replace(config.Command, "{targetFileName}", targetFileName, -1)
		log.Println("command:", command)
		stdout, err := execCommand(command)
		if err != nil {
			c.String(http.StatusBadRequest, "执行出错：" + err.Error())
			return
		}
		log.Println("stdout:", stdout)

		c.String(http.StatusOK, stdout)
	})

	return r
}

func Cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method

		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "POST, GET, OPTIONS")

		if method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
		}
		c.Next()
	}
}

func execCommand(command string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	stdout, err := cmd.Output()

	if err != nil {
		log.Println("exec error:", err)
		return "", err
	}

	return string(stdout), nil
}

func loadConfig() {
	if content, err :=ioutil.ReadFile("config.json");err == nil {
		configs = nil
		if err := json.Unmarshal(content, &configs);err != nil {
			log.Fatal("config.json is invalid", err)
		}
	}
}

func produceFileName(fileName string) string {
	random := strconv.Itoa(rand.Intn(999999))
	return strings.Replace(fileName, "{random}", random, -1)
}

func main() {
	r := setupRouter()
	port :=  "8205"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}
	r.Run(":" + port)
}
