package main

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
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
		urlObj := url.URL{Path: "/" + urlPath + name}
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

func formatFilePath(path string) string {
	return strings.Replace(path, "\\", "/", -1)
}

