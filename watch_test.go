package main

import (
	"fmt"
	"testing"
	"time"

	cv "github.com/glycerine/goconvey/convey"
)

func TestWatchdogShouldNoticeAndRestartChild(t *testing.T) {

	cv.Convey("our Watchdog goroutine should notice and restart", t, func() {
		cfg := &WatchdogConfig{
			PathToChildProcess: "./testcmd/sleep50",
		}
		watcher := NewWatchdog(cfg)
		watcher.Start()

		var err error
		testOver := time.After(100 * time.Millisecond)

	testloop:
		for {
			select {
			case <-testOver:
				fmt.Printf("\n testOver timeout fired.\n")
				err = watcher.Stop()
				break testloop
			case <-time.After(3 * time.Millisecond):
				VPrintf("watch_test: after 3 milliseconds: requesting restart of child process.\n")
				watcher.RestartChild <- true
			case <-watcher.Done:
				fmt.Printf("\n water.Done fired.\n")
				err = watcher.GetErr()
				fmt.Printf("\n watcher.Done, with err = '%v'\n", err)
				break testloop
			}
		}
		panicOn(err)
		// getting 14-27 starts on OSX
		fmt.Printf("\n done after %d starts.\n", watcher.startCount)
		cv.So(watcher.startCount, cv.ShouldBeGreaterThan, 5)
	})
}
