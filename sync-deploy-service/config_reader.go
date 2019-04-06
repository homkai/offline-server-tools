package main

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"log"
)

const (
	PreError = "[syncds ERROR]:"
	PreLog = "[syncds INFO]"
)

type WatchConf struct {
	IncludePaths []string `yaml:"include-paths"`
	IncludeRegexp string `yaml:"include-regexp"`
	ExcludeRegexp string `yaml:"exclude-regexp"`
	Deploy string `yaml:"deploy"`
}

type ClientConf struct {
	Name string `yaml:"name"`
	Server string `yaml:"server"`
	BaseDir string `yaml:"base-dir"`
	IntervalMs int `yaml:"interval-ms"`
	WatchList []WatchConf `yaml:"watch-list"`
}

func (conf *ClientConf) getConf() *ClientConf {
	yamlFile, err := ioutil.ReadFile("syncds-client.yml")
	if err != nil {
		log.Fatalf("syncds-client Get err %v ", err)
	}
	err = yaml.Unmarshal(yamlFile, conf)
	if err != nil {
		log.Fatalf("syncds-client Unmarshal err %v", err)
	}
	return conf
}


type ServerConf struct {
	Name string `yaml:"name"`
	Server string `yaml:"server"`
	BaseDir string `yaml:"base-dir"`
	ShowDirList bool `yaml:"show-dir-list"`
}

func (conf *ServerConf) getConf() *ServerConf {
	yamlFile, err := ioutil.ReadFile("syncds-server.yml")
	if err != nil {
		log.Fatalf("syncds-server Get err %v ", err)
	}
	err = yaml.Unmarshal(yamlFile, conf)
	if err != nil {
		log.Fatalf("syncds-server Unmarshal err %v", err)
	}
	return conf
}