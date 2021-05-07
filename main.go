/*
   2020年4月17日 14:19:02 by elvis
   kubernetes nginx configmap reload

*/
package main

import (
	"github.com/fsnotify/fsnotify"
	proc "github.com/shirou/gopsutil/process"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

const (
	nginxProcessName     = "nginx"
	defaultNginxConfPath = "/etc/nginx"
	watchPathEnvVarName  = "WATCH_NGINX_CONF_PATH"
)

var stderrLogger = log.New(os.Stderr, "error: ", log.Lshortfile)
var stdoutLogger = log.New(os.Stdout, "", log.Lshortfile)

func getMasterNginxPid() (int, error) {
	processes, processesErr := proc.Processes()
	if processesErr != nil {
		return 0, processesErr
	}

	nginxProcesses := map[int32]int32{}

	for _, process := range processes {
		processName, processNameErr := process.Name()
		if processNameErr != nil {
			return 0, processNameErr
		}

		if processName == nginxProcessName {
			ppid, ppidErr := process.Ppid()

			if ppidErr != nil {
				return 0, ppidErr
			}

			nginxProcesses[process.Pid] = ppid
		}
	}

	var masterNginxPid int32

	for pid, ppid := range nginxProcesses {
		if ppid == 0 {
			masterNginxPid = pid

			break
		}
	}

	stdoutLogger.Println("found master nginx pid:", masterNginxPid)

	return int(masterNginxPid), nil
}

func signalNginxReload(pid int) error {
	stdoutLogger.Printf("signaling master nginx process (pid: %d) -> SIGHUP\n", pid)
	nginxProcess, nginxProcessErr := os.FindProcess(pid)

	if nginxProcessErr != nil {
		return nginxProcessErr
	}

	return nginxProcess.Signal(syscall.SIGHUP)
}

func main() {
	watcher, watcherErr := fsnotify.NewWatcher()
	if watcherErr != nil {
		stderrLogger.Fatal(watcherErr)
	}
	defer watcher.Close()

	done := make(chan bool)
	go func() {
		for {
			select {
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}

				if event.Op&fsnotify.Create == fsnotify.Create {
					//if filepath.Base(event.Name) == "..data" {
					stdoutLogger.Println("config map updated")

					nginxPid, nginxPidErr := getMasterNginxPid()
					if nginxPidErr != nil {
						stderrLogger.Printf("getting master nginx pid failed: %s", nginxPidErr.Error())

						continue
					}

					if err := signalNginxReload(nginxPid); err != nil {
						stderrLogger.Printf("signaling master nginx process failed: %s", err)
					}
				}
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				stderrLogger.Printf("received watcher.Error: %s", err)
			}
		}
	}()

	pathToWatch, ok := os.LookupEnv(watchPathEnvVarName)
	if !ok {
		pathToWatch = defaultNginxConfPath
	}

	stdoutLogger.Printf("adding path: `%s` to watch\n", pathToWatch)

	if err := watcher.Add(pathToWatch); err != nil {
		stderrLogger.Fatal(err)
	}
	<-done
}
