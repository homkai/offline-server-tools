package main

import (
	"bufio"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"github.com/emirpasic/gods/lists/arraylist"
	"github.com/radovskyb/watcher"
	"io"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

func StartClient(clientConf ClientConf) {
	watch(clientConf)
}

func watch(clientConf ClientConf) {
	w := watcher.New()
	baseAbsPath, _ := filepath.Abs(clientConf.BaseDir)

	go func() {
		for {
			select {
			case event := <-w.Event:
				optType := getOptType(event.Op)
				relativePath := strings.Replace(event.Path, baseAbsPath, "", 1)
				// 只监听文件，跳过文件夹
				if event.IsDir() {
					log.Println("watcher event dir:", relativePath, event.Op.String())
					continue
				}
				// 同步文件改动
				log.Printf(PreLog + " watcher event path: %s, optType: %s", relativePath, event.Op.String())
				fileChanges := arraylist.New()
				fileChanges.Add(FileMeta{relativePath, optType, ""})
				watchConf, err := getWatchConf(clientConf, event.Path)
				if err != nil {
					log.Fatalln(PreError, err)
					return
				}
				handleChange(clientConf, watchConf, fileChanges)
			case err := <-w.Error:
				log.Println(PreError, "watcher error:", err)
			case <-w.Closed:
				return
			}
		}
	}()

	// add watch paths
	for _, watchConf := range clientConf.WatchList {
		for _, includePath := range watchConf.IncludePaths {
			if strings.HasPrefix(includePath, "/") {
				log.Fatalln(PreError, "`include-paths` must be relative paths! err path:", includePath)
			}
			if strings.Contains(includePath, "..") {
				log.Fatalln(PreError, "`include-paths` not support `../`! err path:", includePath)
			}
			relativePath := filepath.Join(clientConf.BaseDir, includePath)
			fileInfo, err := os.Lstat(relativePath)
			if err != nil {
				log.Fatalln(PreError, "relativePath is illegal, err path:", relativePath)
			}
			if fileInfo.IsDir() {
				if err := w.AddRecursive(relativePath); err != nil {
					log.Fatalln(PreError, "watch dir relativePath error, err path:", relativePath)
				}
			} else {
				if err := w.Add(relativePath); err != nil {
					log.Fatalln(PreError, "watch file relativePath error, err path:", relativePath)
				}
			}
		}
		if watchConf.IncludeRegexp != "" || watchConf.ExcludeRegexp != "" {
			w.AddFilterHook(func(fileInfo os.FileInfo, fullPath string) error {
				for _, includePath := range watchConf.IncludePaths {
					absPath, _ := filepath.Abs(filepath.Join(clientConf.BaseDir, includePath))
					if strings.HasPrefix(fullPath, absPath) {
						relativePath := strings.Replace(fullPath, absPath, "", 1)
						if watchConf.IncludeRegexp != "" {
							reg := regexp.MustCompile(watchConf.IncludeRegexp)
							if !reg.MatchString(relativePath) {
								return watcher.ErrSkip
							}
						}
						if watchConf.ExcludeRegexp != "" {
							reg := regexp.MustCompile(watchConf.ExcludeRegexp)
							if reg.MatchString(relativePath) {
								return watcher.ErrSkip
							}
						}
					}
				}
				return nil
			})
		}
	}

	// Start the watching process - it'll check for changes every 100ms.
	if err := w.Start(time.Duration(clientConf.IntervalMs) * time.Millisecond); err != nil {
		log.Fatalln(err)
	}
}

func handleChange(clientConf ClientConf, watchConf WatchConf, fileChanges *arraylist.List) {
	// 预先diff，避免重复提交
	noDiffs, err := diff(clientConf, fileChanges)
	log.Println("no diff files", noDiffs.Values())
	if err == nil {
		// 过滤不需要同步的文件
		fileChanges = fileChanges.Select(func(index int, value interface{}) bool {
			return !noDiffs.Contains(value.(FileMeta).FilePath)
		})
		if fileChanges.Empty() {
			log.Println("skip sync")
			return
		}
	}
	// 同步文件
	sync(clientConf, watchConf, fileChanges)
}

func diff(clientConf ClientConf, fileChanges *arraylist.List) (*arraylist.List, error) {
	diffService := "http://" + clientConf.Server + "/diff"

	toDiffs := arraylist.New()
	for _, fileMeta := range fileChanges.Values() {
		fileMeta := fileMeta.(FileMeta)
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
		toDiffs.Add(FileMeta{fileMeta.FilePath, OptWrite, md5Code})
	}

	fileMetasJson, _ := toDiffs.ToJSON()
	params := map[string]string{
		"fileMetas": string(fileMetasJson),
	}
	resp, err := Post(diffService, params)

	if err != nil {
		log.Println("post err", err)
		return nil, err
	}
	body, err := ioutil.ReadAll(resp.Body)
	noDiffs := arraylist.New()
	err = noDiffs.FromJSON(body)
	if err != nil {
		log.Println("resp err", err)
		return nil, err
	}

	return noDiffs, nil
}

func sync(clientConf ClientConf, watchConf WatchConf, fileChanges *arraylist.List) {
	syncService := "http://" + clientConf.Server + "/sync"

	fileMetasJson, _ := fileChanges.ToJSON()

	params := map[string]string {
		"deploy": watchConf.Deploy,
		"fileMetas": string(fileMetasJson),
	}
	resp, err := PostFile(syncService, params, fileChanges, clientConf.BaseDir)

	if err != nil {
		log.Println("post err", err)
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Split(bufio.ScanLines)
	for scanner.Scan() {
		log.Println("syncds resp:", scanner.Text())
	}
}

func getOptType(op watcher.Op) int  {
	if op == watcher.Remove || op == watcher.Rename || op == watcher.Move {
		return OptRemove
	}
	return OptWrite
}

func getWatchConf(clientConf ClientConf, fullPath string) (WatchConf, error) {
	for _, watchConf := range clientConf.WatchList {
		for _, includePath := range watchConf.IncludePaths {
			absPath, _ := filepath.Abs(filepath.Join(clientConf.BaseDir, includePath))
			if strings.HasPrefix(fullPath, absPath) {
				return watchConf, nil
			}
		}
	}
	return WatchConf{}, errors.New("fullPath not match any include-paths rule, trigger fullPath:" + fullPath)
}