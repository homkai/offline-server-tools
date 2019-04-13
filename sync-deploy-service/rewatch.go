package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/fsnotify/fsnotify"
)

// WatchEvent represents a watcher event. These can include errors.
type WatchEvent struct {
	Error error
	Body  *fsnotify.Event
}

type WatchFilterFunc func(relativeBasePath string, isDir  bool) bool

// ReWatcher is the struct for the recursive watcher. Run Init() on it.
type ReWatcher struct {
	Path     string // computed path
	IsWatch  WatchFilterFunc
	debug    bool
	safename string // safe path
	watcher  *fsnotify.Watcher
	events   chan WatchEvent // one channel for events and err...
	closed   bool            // is the events channel closed?
	mutex    sync.Mutex      // lock guarding the channel closing
	wg       sync.WaitGroup
	exit     chan struct{}
}

// NewReWatcher creates an initializes a new recursive watcher.
func New(path string, filterFunc WatchFilterFunc, debug bool) (*ReWatcher, error) {
	obj := &ReWatcher{
		Path:    path,
		IsWatch: filterFunc,
		debug:   debug,
	}
	return obj, obj.Init()
}

// Init starts the recursive file watcher.
func (obj *ReWatcher) Init() error {
	obj.watcher = nil
	obj.events = make(chan WatchEvent)
	obj.exit = make(chan struct{})
	obj.safename = filepath.Clean(obj.Path)          // no trailing slash

	var err error
	obj.watcher, err = fsnotify.NewWatcher()
	if err != nil {
		return err
	}

	if err := obj.addSubFolders(obj.safename); err != nil {
		return err
	}

	obj.wg.Add(1)
	go func() {
		defer obj.wg.Done()
		if err := obj.Watch(); err != nil {
			// we need this mutex, because if we Init and then Close
			// immediately, this can send after closed which panics!
			obj.mutex.Lock()
			if !obj.closed {
				select {
				case obj.events <- WatchEvent{Error: err}:
				case <-obj.exit:
					// pass
				}
			}
			obj.mutex.Unlock()
		}
	}()
	return nil
}

//func (obj *ReWatcher) Add(path string) error { // XXX: implement me or not?
//
//}
//
//func (obj *ReWatcher) Remove(path string) error { // XXX: implement me or not?
//
//}

// Close shuts down the watcher.
func (obj *ReWatcher) Close() error {
	var err error
	close(obj.exit) // send exit signal
	obj.wg.Wait()
	if obj.watcher != nil {
		err = obj.watcher.Close()
		obj.watcher = nil
	}
	obj.mutex.Lock() // FIXME: I don't think this mutex is needed anymore...
	obj.closed = true
	close(obj.events)
	obj.mutex.Unlock()
	return err
}

// Events returns a channel of events. These include events for errors.
func (obj *ReWatcher) Events() chan WatchEvent { return obj.events }

// Watch is the primary listener for this resource and it outputs events.
func (obj *ReWatcher) Watch() error {
	if obj.watcher == nil {
		return fmt.Errorf("the watcher is not initialized")
	}

	root := obj.safename

	for {
		if obj.debug {
			log.Printf("watching: %s", root) // attempting to watch...
		}
		// initialize in the loop so that we can reset on rm-ed handles
		if err := obj.watcher.Add(root); err != nil {
			log.Printf("watcher.Add(%s): Error: %v", root, err)
		}

		select {
		case event := <-obj.watcher.Events:
			if obj.debug {
				log.Printf("event(%s): %s", event.Name, event.Op.String())
			}
			if !obj.testWatch(event.Name, isDir(event.Name)) {
				continue
			}
			if event.Op&fsnotify.Write == fsnotify.Write {
				if isDir(event.Name) {
					continue
				}
			} else if event.Op&fsnotify.Create == fsnotify.Create {
				obj.watcher.Add(event.Name)
				if isDir(event.Name) {
					if err := obj.addSubFolders(event.Name); err != nil {
						log.Printf("new addSubFolders err: %v", err)
					}
				}
			} else if event.Op&fsnotify.Rename == fsnotify.Rename {
				obj.watcher.Remove(event.Name)
				obj.watcher.Add(event.Name)
			} else if event.Op&fsnotify.Remove == fsnotify.Remove {
				obj.watcher.Remove(event.Name)
			}

			// only invalid state on certain types of events
			select {
			// exit even when we're blocked on event sending
			case obj.events <- WatchEvent{Error: nil, Body: &event}:
			case <-obj.exit:
				return fmt.Errorf("pending event not sent")
			}

		case err := <-obj.watcher.Errors:
			return fmt.Errorf("unknown watcher error: %v", err)

		case <-obj.exit:
			return nil
		}
	}
}

func (obj *ReWatcher) testWatch(path string, isDir bool) bool {
	relativePath := GetRelativeDirPath(obj.safename, path)
	if relativePath != "." && !obj.IsWatch(relativePath, isDir) {
		return false
	}
	return true
}

// addSubFolders is a helper that is used to add recursive dirs to the watches.
func (obj *ReWatcher) addSubFolders(p string) error {
	if !obj.testWatch(p, true) {
		return nil
	}
	// look at all subfolders...
	walkFn := func(path string, info os.FileInfo, err error) error {
		if obj.debug {
			log.Printf("walk: %s (%v): %v", path, info, err)
		}
		if err != nil {
			return nil
		}
		if info.IsDir() {
			if !obj.testWatch(path, true) {
				return nil
			}
			err := obj.watcher.Add(path)
			if err != nil {
				return err
			}
		}
		return nil
	}
	err := filepath.Walk(p, walkFn)
	return err
}

func isDir(path string) bool {
	fInfo, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fInfo.IsDir()
}

func GetRelativeDirPath(baseDir string, path string) string {
	if baseDir != "." {
		path = strings.Replace(path, baseDir, "", 1)
	}
	return LtrimPathSep(path)
}

func LtrimPathSep(path string) string {
	if strings.HasPrefix(path, "/") || strings.HasPrefix(path, "\\") {
		path = path[1:]
	}
	return filepath.Clean(path)
}