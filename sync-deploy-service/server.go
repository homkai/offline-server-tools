package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/bluele/gcache"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	OptWrite = iota
	OptRemove
)


type FileMeta struct {
	FilePath string `json:"filePath"`
	OptType int `json:"optType"`
	Md5Code string `json:"md5Code"`
}

type fileMd5Meta struct {
	Md5Code string
	ModTime time.Time
}

var md5Cache gcache.Cache

func setupRouter(conf ServerConf) {
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			return
		}
		urlPath := r.URL.Path
		if strings.HasPrefix(urlPath, "/") {
			urlPath = strings.Replace(urlPath, "/", "", 1)
		}
		filePath := filepath.Join(conf.BaseDir, urlPath)
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
	})

	http.HandleFunc("/diff", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}

		fileMetasJson := r.PostFormValue("fileMetas")
		if fileMetasJson == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "fileMetas empty")
			return
		}
		var fileMetas []FileMeta;
		err := json.Unmarshal([]byte(fileMetasJson), &fileMetas)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "fileMetas Unmarshal json:%s, err: %v", fileMetasJson, err)
			return
		}

		noDiffs := make([]string, len(fileMetas))
		for index, fileMeta := range fileMetas {
			filePath := filepath.Join(conf.BaseDir, fileMeta.FilePath)
			// 对比md5
			md5Code, err := calcFileMd5(filePath)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, "read exist file err: %v", err)
				return
			}
			if md5Code != "" && fileMeta.Md5Code == md5Code {
				noDiffs[index] = fileMeta.FilePath
			}
		}

		w.WriteHeader(http.StatusOK)
		retJson, err := json.Marshal(noDiffs)
		_,_ = w.Write(retJson)
	})

	http.HandleFunc("/sync", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			return
		}

		w.Header().Set("Transfer-Encoding", "chunked")

		deploy := r.PostFormValue("deploy")
		fileMetasJson := r.PostFormValue("fileMetas")
		if deploy == "" || fileMetasJson == "" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "deploy or filePaths empty")
			return
		}
		var fileMetas []FileMeta;
		err := json.Unmarshal([]byte(fileMetasJson), &fileMetas)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "fileMetas Unmarshal json:%s, err: %v", fileMetasJson, err)
			return
		}
		if r.MultipartForm.File == nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "no file uploaded")
			return
		}
		// max memory for read file
		_ = r.ParseMultipartForm(100 << 20)
		for _, fileMeta := range fileMetas {
			filePath := filepath.Join(conf.BaseDir, fileMeta.FilePath)
			if fileMeta.OptType == OptRemove {
				_, err = os.Lstat(filePath)
				if err != nil {
					continue
				}
				err = os.Remove(filePath)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprintf(w, "remove file err: %v", err)
					return
				}
				log.Println("file removed", filePath)
				continue
			}
			// 拿到上传的文件
			fhs, ok := r.MultipartForm.File[fileMeta.FilePath]
			if !ok || len(fhs) == 0 {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, "upload file err: %v", err)
				return
			}
			file, _ := fhs[0].Open()
			// 创建父文件夹
			fileDir := filepath.Dir(filePath)
			_, err = os.Lstat(fileDir)
			if os.IsNotExist(err) {
				err = os.MkdirAll(fileDir, os.ModePerm)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = fmt.Fprintf(w, "mkdir err: %v", err)
					return
				}
			}
			// 写文件
			saveFile, err := os.Create(filePath)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, "write file err: %v", err)
				return
			}
			_, err = io.Copy(saveFile, file)
			_ = saveFile.Close()
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = fmt.Fprintf(w, "write file err: %v", err)
				return
			}
			log.Println("file written", filePath)
		}

		log.Println("deploy:", deploy)
		cmd := exec.Command("sh", "-c", deploy)
		stdout, _ := cmd.StdoutPipe()
		err = cmd.Start()
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprintf(w, "deploy err: %v", err)
			return
		}
		reader := bufio.NewReader(stdout)
		for {
			line, err := reader.ReadString('\n')
			if err != nil || io.EOF == err {
				break
			}
			log.Println("stdout line:", line)
			_, _ = w.Write([]byte(line))
			w.(http.Flusher).Flush()
		}
		_ = cmd.Wait()
	})
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

func StartServer(conf ServerConf) {
	md5Cache = gcache.New(100).LFU().Build()
	setupRouter(conf)
	err := http.ListenAndServe(conf.Server, nil)
	if err != nil {
		log.Fatalf("server run at %s, failed. please check the config-file/server", conf.Server)
	}
}
