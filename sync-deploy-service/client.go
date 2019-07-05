package main

import (
	"bytes"
	"crypto/md5"
	"encoding/gob"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)


type (
	Event *fsnotify.Event
	TimeEventMap map[time.Time]Event
	WsReqMessage struct {
		Type string
		Data []byte
	}
	WsResMessage struct {
		Type string
		Data string
	}
	DiffReq struct {
		FileMetas []FileMeta
	}
	SyncReq struct {
		FileMetas []FileMeta
		DeployCmd string
		DeployKillCmd string
	}
)


var (
	clientConf ClientConf
	messageChan = make(chan WsReqMessage, 10)
	done = make(chan struct{})
)

//func main() {
//	var conf ClientConf
//	conf.getConf()
func StartClient(conf ClientConf) {
	clientConf = conf

	go watch(done)
	go connectWs(done)
	select {
	case <-done:
		log.Printf(PreError + " shutdown! please check and reboot client")
	}
}

func watch(done chan struct{}) {
	refreshDuration := time.Duration(clientConf.IntervalMs) * time.Millisecond

	var paths []string
	for _, includePath := range clientConf.IncludePaths {
		path := filepath.Join(clientConf.BaseDir, includePath)
		paths = append(paths, path)
	}

	// 监听base-dir，然后再根据include、exclude筛选
	rw, err := New(clientConf.BaseDir, func(relativeBasePath string, isDir bool) bool {
		isMatchExclude, _ := regexp.MatchString(clientConf.ExcludePathRegexp, relativeBasePath)
		if isMatchExclude {
			if clientConf.Debug {
				log.Printf(PreLog + " isMatch %t, isMatchExclude", false)
			}
			return false
		}
		if !isDir {
			// baseDir子层
			if strings.ContainsAny(relativeBasePath, "/\\") {
				if clientConf.IncludeFileRegexp == "" {
					return true
				}
				isMatchInclude, _ := regexp.MatchString(clientConf.IncludeFileRegexp, relativeBasePath)
				if clientConf.Debug {
					log.Printf(PreLog + " isMatch %t, isMatchInclude file", isMatchInclude)
				}
				return isMatchInclude
			}
			// baseDir这一层，验证匹配includePaths是否有对应文件
			for _, includePath := range clientConf.IncludePaths {
				cleanIncludePath := filepath.Clean(includePath);
				if clientConf.Debug {
					log.Printf(PreLog + " isMatch %t, isMatchInclude dir", cleanIncludePath == relativeBasePath)
				}
				if cleanIncludePath == relativeBasePath {
					return true
				}
			}
			return false
		}
		for _, includePath := range clientConf.IncludePaths {
			includePath = filepath.Clean(includePath);
			if strings.HasPrefix(includePath, relativeBasePath) {
				return true
			}
		}
		if clientConf.Debug {
			log.Printf(PreLog + " isMatch %t, includePaths dir", false)
		}
		return false
	}, clientConf.Debug)
	if err != nil {
		log.Println(PreError, "init rw err:", err)
	}
	defer rw.Close()

	var mut sync.Mutex
	events := make(TimeEventMap)
	baseAbsPath, _ := filepath.Abs(clientConf.BaseDir)

	// Collect the events for the last n seconds, repeatedly
	// Runs in the background
	CollectFileChangeEvents(rw, &mut, events, done, refreshDuration)
	log.Printf(PreLog + " start watch at: %s", baseAbsPath)

	// Serve events
	ticker := time.NewTicker(refreshDuration)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			log.Printf(PreError + " watch done")
			return
		case <-ticker.C:
			if len(events) == 0 {
				continue
			}
			mut.Lock()
			var fileChanges []FileMeta
			// Remove old keys
			RemoveOldEvents(&events, refreshDuration)
			for _, ev := range events {
				eventAbsPath, _ := filepath.Abs(ev.Name)
				filePath := strings.Replace(eventAbsPath, baseAbsPath, "", 1)
				log.Printf(PreLog + " change filePath: %s, op: %v", filePath, ev.Op)
				// Avoid sending several events for the same filename
				existed := false
				for i := 0; i < len(fileChanges); i++ {
					if fileChanges[i].FilePath == filePath {
						existed = true
						break
					}
				}
				if existed {
					continue
				}
				var optType int
				if ev.Op == fsnotify.Remove {
					optType = OptRemove
				} else {
					optType = OptWrite
					fileInfo, err := os.Lstat(ev.Name)
					if err != nil {
						log.Println("read file err", filePath)
					}
					// 只监听文件，跳过文件夹
					if err == nil && fileInfo.IsDir() {
						log.Println(PreLog, "IsDir")
						continue
					}
				}
				// 同步文件改动
				fileChanges = append(fileChanges, FileMeta{filePath, optType, "", nil})
			}
			if len(fileChanges) > 0 {
				handleChanges(fileChanges)
			}
			mut.Unlock()
		}
	}
}

func handleChanges(fileChanges []FileMeta) {
	var filePaths []string
	for index, fileMeta := range fileChanges{
		if fileMeta.OptType == OptRemove {
			continue
		}
		file, err := os.Open(filepath.Join(clientConf.BaseDir, fileMeta.FilePath))
		if err != nil {
			log.Println("file open err", err)
			continue
		}
		md5hash := md5.New()
		_, err = io.Copy(md5hash, file)
		_ = file.Close()
		if err != nil {
			log.Println("file copy err", err)
			continue
		}
		md5Code := hex.EncodeToString(md5hash.Sum(nil))
		fileMeta.Md5Code = md5Code
		fileChanges[index] = fileMeta
		filePaths = append(filePaths, fileMeta.FilePath)
	}

	log.Printf(PreLog + " diff files: %v", filePaths)
	req := DiffReq {
		fileChanges,
	}

	buf := &bytes.Buffer{}
	_ = gob.NewEncoder(buf).Encode(req)
	wsMsg := WsReqMessage{
		"diff",
		buf.Bytes(),
	}

	messageChan <- wsMsg
}

func syncChanges(fileChanges []FileMeta) {
	deployCmd := ""
	var filePaths []string
	for index, fileMeta := range fileChanges {
		filePaths = append(filePaths, fileMeta.FilePath)
		if fileMeta.OptType == OptRemove {
			continue
		}
		filename := filepath.Join(clientConf.BaseDir, fileMeta.FilePath)
		fileData, err := ioutil.ReadFile(filename)
		if err != nil {
			log.Println("file open err", err)
			continue
		}
		fileMeta.FileData = fileData
		fileChanges[index] = fileMeta
		// 是否触发deploy-cmd
		if clientConf.DeployPathRegexp != "" {
			isMatch, _ := regexp.MatchString(clientConf.DeployPathRegexp, fileMeta.FilePath)
			if isMatch {
				deployCmd = clientConf.DeployCmd
			}
		} else {
			deployCmd = clientConf.DeployCmd
		}
	}

	log.Printf(PreLog + " sync begin, plz wait, files: %v, deploy? %t", filePaths, len(deployCmd) > 0)
	req := SyncReq {
		fileChanges,
		deployCmd,
		clientConf.DeployKillCmd,
	}
	buf := &bytes.Buffer{}
	_ = gob.NewEncoder(buf).Encode(req)
	wsMsg := WsReqMessage{
		"sync",
		buf.Bytes(),
	}

	messageChan <- wsMsg
}

func connectWs(done chan struct{}) {
	u := url.URL{Scheme: "ws", Host: clientConf.Server, Path: "/ws"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("dial:", err)
	}
	defer c.Close()
	_ = c.WriteMessage(websocket.TextMessage, []byte("set up connection from client"));
	log.Printf(PreLog + " start ws connection to server at: %s", clientConf.Server)

	go func() {
		for {
			_, message, err := c.ReadMessage()
			if err != nil {
				log.Printf(PreError + " read message from server failed, err: %v", err)
				done <- struct{}{}
				return
			}
			var wsResMsg WsResMessage
			_ = json.Unmarshal(message, &wsResMsg)
			switch wsResMsg.Type {
			case "diffRes":
				data := wsResMsg.Data
				var fileMetas []FileMeta
				_ = json.Unmarshal([]byte(data), &fileMetas)
				if len(fileMetas) > 0 {
					syncChanges(fileMetas)
				} else {
					log.Printf(PreLog + " no diff, skiped all changed fileds")
				}
			case "syncRes":
				data := wsResMsg.Data
				log.Printf(PreLog + " syncRes %s", data)
			case "deployStdout":
				fmt.Printf("[stdout] %s\n", wsResMsg.Data)
			case "deployStderr":
				fmt.Printf("[stderr] %s\n", wsResMsg.Data)
			}
		}
	}()

	for {
		select {
		case <-done:
			_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"))
			return
		case wsMsg := <-messageChan:
			buf := &bytes.Buffer{}
			err = gob.NewEncoder(buf).Encode(wsMsg)
			if err != nil {
				log.Printf("binary Encode err %v", err)
			}
			err = c.WriteMessage(websocket.BinaryMessage, buf.Bytes())
			if err != nil {
				log.Println("write:", err)
				return
			}
		}
	}
}

func CollectFileChangeEvents(watcher *ReWatcher, mut *sync.Mutex, events TimeEventMap, done chan struct{}, maxAge time.Duration) {
	go func() {
		for {
			select {
			case ev, ok := <-watcher.Events():
				if !ok {
					log.Printf(PreError + " watch done")
					done <- struct{}{}
					return
				}
				if ev.Error != nil {
					log.Println(PreError, "watch err:", ev.Error)
					continue
				}
				mut.Lock()
				RemoveOldEvents(&events, maxAge)
				events[time.Now()] = Event(ev.Body)
				mut.Unlock()
			}
		}
	}()
}

func RemoveOldEvents(events *TimeEventMap, maxAge time.Duration) {
	now := time.Now()
	longTimeAgo := now.Add(-maxAge)
	for t := range *events {
		if t.Before(longTimeAgo) {
			delete(*events, t)
		}
	}
}