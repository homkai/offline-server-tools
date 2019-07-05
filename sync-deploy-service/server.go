package main

import (
	"bufio"
	"bytes"
	"crypto/md5"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bluele/gcache"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	OptWrite = iota
	OptRemove
)


type FileMeta struct {
	FilePath string
	OptType int
	Md5Code string
	FileData []byte
}

type fileMd5Meta struct {
	Md5Code string
	ModTime time.Time
}

var upgrader  = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

var (
	serverConf ServerConf
	md5Cache gcache.Cache
	executingCmd *exec.Cmd
	defaultConn *websocket.Conn
	mut sync.Mutex
)


func serveWs(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	defaultConn = c
	if err != nil {
		log.Print("websocket upgrade err:", err)
		return
	}
	defer c.Close()
	for {
		mt, reader, err := c.NextReader()
		if err != nil {
			log.Printf("read err: %v", err)
			continue
		}
		if mt == websocket.CloseMessage {
			message, _ := ioutil.ReadAll(reader)
			log.Println("disconnect from client: " + string(message))
		}
		if mt == websocket.TextMessage {
			message, _ := ioutil.ReadAll(reader)
			log.Println("get TextMessage: " + string(message))
		}
		if mt == websocket.BinaryMessage {
			wsReqMsg := WsReqMessage{}
			err = gob.NewDecoder(reader).Decode(&wsReqMsg)
			if err != nil {
				log.Printf("read binary err: %v", err)
				continue
			}
			switch wsReqMsg.Type {
			case "diff":
				req := DiffReq{}
				err = gob.NewDecoder(bytes.NewBuffer(wsReqMsg.Data)).Decode(&req)
				if err != nil {
					log.Printf("read DiffReq err: %v", err)
					continue
				}
				log.Printf(PreLog + " [ws] serve diff")

				fileMetas := req.FileMetas
				var needSyncs []FileMeta
				for _, fileMeta := range fileMetas {
					if fileMeta.OptType == OptRemove {
						needSyncs = append(needSyncs, fileMeta)
						continue
					}
					filePath := filepath.Join(serverConf.BaseDir, formatFilePath(fileMeta.FilePath))
					// 对比md5
					md5Code, err := calcFileMd5(filePath)
					if err != nil || fileMeta.Md5Code != md5Code {
						needSyncs = append(needSyncs, fileMeta)
					} else {
						log.Printf(PreLog + " diff, skip sync file: %s", fileMeta.FilePath)
					}
				}
				syncFileMetasBytes, _ := json.Marshal(needSyncs)
				writeJsonLocked("diffRes", string(syncFileMetasBytes))
			case "sync":
				req := SyncReq{}
				err = gob.NewDecoder(bytes.NewBuffer(wsReqMsg.Data)).Decode(&req)
				if err != nil {
					log.Printf("read SyncReq err: %v", err)
					continue
				}
				log.Printf(PreLog + " [ws] serve sync")

				fileMetas := req.FileMetas
				for _, fileMeta := range fileMetas {
					filePath := filepath.Join(serverConf.BaseDir, formatFilePath(fileMeta.FilePath))
					// 删文件
					if fileMeta.OptType == OptRemove {
						_, err = os.Lstat(filePath)
						if err != nil {
							log.Printf("remove file stat err: %v", err)
							continue
						}
						err = os.Remove(filePath)
						if err != nil {
							w.WriteHeader(http.StatusBadRequest)
							log.Printf("remove file err: %v", err)
							continue
						}
						log.Println("file removed", filePath)
						continue
					}
					// 创建父文件夹
					fileDir := filepath.Dir(filePath)
					_, err = os.Lstat(fileDir)
					if os.IsNotExist(err) {
						err = os.MkdirAll(fileDir, os.ModePerm)
						if err != nil {
							log.Printf("mkdir parent file err: %v", err)
							continue
						}
					}
					// 写文件
					err = ioutil.WriteFile(filePath, fileMeta.FileData, os.ModePerm)
					if err != nil {
						log.Printf("WriteFile err: %v", err)
						continue
					}
					log.Printf(PreLog + " sync, write file success: %s", fileMeta.FilePath)
				}
				if req.DeployCmd != "" {
					go execDeploy(req.DeployCmd, req.DeployKillCmd)
				}
			}
		}
	}
}

func writeJsonLocked(typ string, data string) {
	mut.Lock()
	defer mut.Unlock()
	_ = defaultConn.WriteJSON(WsResMessage{
		typ,
		data,
	})
}

func execDeploy(deployCmd string, deployKillCmd string) {
	if executingCmd != nil {
		err := executingCmd.Process.Kill()
		if err != nil {
			writeJsonLocked("syncRes", "kill failed, err:" + err.Error())
			log.Println("kill failed, err:" + err.Error())
			if deployKillCmd != "" {
				_ = exec.Command("sh", "-c", deployKillCmd).Start()
			}
		} else {
			writeJsonLocked("syncRes", "kill success")
			log.Println("kill success")
		}
	}

	// fix start failed after kill
	time.Sleep(time.Duration(2) * time.Second)

	executingCmd = exec.Command("sh", "-c", deployCmd)
	stdout, _ := executingCmd.StdoutPipe()
	stderr, _ := executingCmd.StderrPipe()
	err := executingCmd.Start()
	if err != nil {
		writeJsonLocked("syncRes", "cmd start failed, err:" + err.Error())
		log.Println("cmd start failed, err:" + err.Error())
		return
	}
	writeJsonLocked("syncRes", "cmd start success")
	log.Println("cmd start success")

	stdoutScanner := bufio.NewScanner(stdout)
	stdoutScanner.Split(bufio.ScanLines)
	for stdoutScanner.Scan() {
		line := stdoutScanner.Text()
		writeJsonLocked("deployStdout", line)
		fmt.Printf( "[stdout] %s\n", line)
	}

	stderrScanner := bufio.NewScanner(stderr)
	stderrScanner.Split(bufio.ScanLines)
	for stderrScanner.Scan() {
		line := stderrScanner.Text()
		writeJsonLocked("deployStderr", line)
		fmt.Printf("[stderr] %s\n", line)
	}

	err = executingCmd.Wait()
	if err != nil {
		writeJsonLocked("syncRes", "cmd exec failed, err:" + err.Error())
		log.Printf("cmd exec failed, err:" + err.Error())
	}
}


func serveDir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		return
	}
	urlPath := r.URL.Path
	if strings.HasPrefix(urlPath, "/") {
		urlPath = strings.Replace(urlPath, "/", "", 1)
	}
	filePath := filepath.Join(serverConf.BaseDir, urlPath)
	log.Println("filePath", filePath)
	stat, err := os.Lstat(filePath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, "file or dir not found: %s", filePath)
		return
	}
	if !stat.IsDir() {
		http.ServeFile(w, r, filePath)
		return
	}
	GenDirIndex(w, filePath, urlPath)
}

func calcFileMd5(filePath string) (string, error) {
	fileStat, err := os.Lstat(filePath)
	if err == nil && !fileStat.IsDir() {
		md5Meta := fileMd5Meta{}
		md5MetaI, err :=  md5Cache.Get(filePath)
		if err == nil && md5MetaI != nil {
			md5Meta = md5MetaI.(fileMd5Meta)
		}
		if err != nil || !fileStat.ModTime().Equal(md5Meta.ModTime) {
			existFile, err := os.Open(filePath)
			if err != nil {
				return "", nil
			}
			log.Printf("calc md5 of file %s", filePath)
			md5hash := md5.New()
			_, _ = io.Copy(md5hash, existFile)
			md5Code := hex.EncodeToString(md5hash.Sum(nil))
			md5Meta.Md5Code = md5Code
			md5Meta.ModTime = fileStat.ModTime()
			err = md5Cache.Set(filePath, md5Meta)
			if err != nil {
				return "", err
			}
		}
		return md5Meta.Md5Code, nil
	}
	return "", nil
}

func handleInterrupt() {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, os.Kill)

	<-interrupt
	log.Println("interrupt")
	if executingCmd != nil {
		err := executingCmd.Process.Kill()
		if err != nil {
			writeJsonLocked("syncRes", "interrupt, kill failed, err:" + err.Error())
			log.Println("interrupt, kill failed, err:" + err.Error())
		} else {
			writeJsonLocked("syncRes", "interrupt, kill success")
			log.Println("interrupt, kill success")
		}
	}
	os.Exit(2)
}

func StartServer(conf ServerConf) {
	serverConf = conf
	md5Cache = gcache.New(100).LFU().Build()

	go handleInterrupt()
	http.HandleFunc("/", serveDir)
	http.HandleFunc("/ws", serveWs)

	log.Printf("server run at %s", serverConf.Server)
	err := http.ListenAndServe(serverConf.Server, nil)
	if err != nil {
		log.Fatalf("server run at %s, failed. please check the config-file/server", serverConf.Server)
	}
}
