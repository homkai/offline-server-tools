package main

import (
	"bytes"
	"fmt"
	"github.com/emirpasic/gods/lists/arraylist"
	"io"
	"io/ioutil"
	"log"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
)

func GenDirIndex(w http.ResponseWriter, filePath string, urlPath string) {
	files, err := ioutil.ReadDir(filePath)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprintf(w, "error reading directory: %v", err)
		return
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Name() < files[j].Name() })

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, "<h1>Index of %s</h1>", filePath + "/")
	_, _ = fmt.Fprintf(w, "<style>td{padding: 5px}</style>")
	_, _ = fmt.Fprintf(w, "<table>\n<tr><td>文件名</td><td>大小</td><td>修改时间</td></tr>\n")
	fileItemTpl := "<tr><td><a href=\"%s\">%s</a></td><td>%s</td><td>%s</td></tr>\n"

	for _, file := range files {
		name := file.Name()
		if file.IsDir() {
			name += "/"
		}
		urlObj := url.URL{Path: urlPath + "/" + name}
		href := urlObj.String()
		fileSize := FormatFileSize(file.Size())
		modTime := file.ModTime().Format("2006-01-02 15:04:05")

		_, _ = fmt.Fprintf(w, fileItemTpl, href, name, fileSize, modTime)
	}
	_, _ = fmt.Fprintf(w, "</table>\n")
}

func FormatFileSize(fileSize int64) string {
	size := strconv.FormatInt(fileSize, 10) + "B"
	if fileSize > 1024 {
		size = strconv.FormatInt(fileSize / 1024, 10) + "KB"
	}
	if fileSize > 1024 * 1024 {
		size = strconv.FormatInt(fileSize / (1024 * 1024), 10) + "MB"
	}
	return size
}

func Post(url string, params map[string]string) (*http.Response, error) {
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)
	for key, value := range params {
		_ = bodyWriter.WriteField(key, value)
	}

	contentType := bodyWriter.FormDataContentType()
	_ = bodyWriter.Close()
	return http.Post(url, contentType, bodyBuf)
}

func PostFile(url string, params map[string]string, fileChanges *arraylist.List, baseDir string) (*http.Response, error) {
	bodyBuf := &bytes.Buffer{}
	bodyWriter := multipart.NewWriter(bodyBuf)

	for _, fileMeta := range fileChanges.Values() {
		fileMeta := fileMeta.(FileMeta)
		if fileMeta.OptType == OptRemove {
			continue
		}
		file, err := os.Open(filepath.Join(baseDir, fileMeta.FilePath))
		if err != nil {
			log.Println("file open err", err)
		}
		fileWriter, _ := bodyWriter.CreateFormFile(fileMeta.FilePath, filepath.Base(fileMeta.FilePath))
		_, err = io.Copy(fileWriter, file)
		if err != nil {
			log.Println("file copy err", err)
		}
		_ = file.Close()
	}

	for key, value := range params {
		_ = bodyWriter.WriteField(key, value)
	}

	contentType := bodyWriter.FormDataContentType()
	_ = bodyWriter.Close()
	return http.Post(url, contentType, bodyBuf)
}