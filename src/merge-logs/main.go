package main

import (
	"bufio"
	"container/list"
	"flag"
	"fmt"
	"log"
	"merge-logs/mergedlog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mgutz/ansi"
)

var MAX_INT = int64(^uint64(0) >> 1)
var FLUSH_BATCH_SIZE = 1000

var stampFormat = "2006-01-02T15:04:05.999Z"
var logs mergedlog.LogCollection
var aggLog *list.List
var lineCount = 0

var palette [8]string
var resetColor string
var userColor string

func init() {
	resetColor = ansi.ColorCode("reset")

	// Taken from the Solarized color palette
	palette[0] = ansi.ColorCode("253") // whiteish
	palette[1] = ansi.ColorCode("64")  // green
	palette[2] = ansi.ColorCode("37")  // cyan
	palette[3] = ansi.ColorCode("33")  // blue
	palette[4] = ansi.ColorCode("61")  // violet
	palette[5] = ansi.ColorCode("125") // magenta
	palette[6] = ansi.ColorCode("160") // red
	palette[7] = ansi.ColorCode("166") // orange
}

func main() {
	flag.StringVar(&userColor, "color", "dark", "Color scheme to use: light, dark or off")
	flag.Parse()

	if userColor == "light" {
		palette[0] = ansi.ColorCode("234") // blackish
	}

	gfeLogLineRE, err := regexp.Compile(`"timestamp":"([^"]+)"`)
	if err != nil {
		log.Fatalf("Invalid regex: %s", err)
	}

	aggLog = list.New()
	colorIndex := 0

	logs = mergedlog.NewLogCollection(len(flag.Args()))

	maxLogNameLength := 0
	var logName, alias string
	// Gather our files and set up a Scanner for each of them
	for _, logTagName := range flag.Args() {
		parts := strings.Split(logTagName, ":")
		alias = parts[0]

		// See if we have an alias for the log
		if len(parts) == 1 {
			logName = parts[0]
			bits := strings.Split(logName, "/")
			alias = bits[len(bits)-1]
		} else {
			logName = parts[1]
		}

		f, err := os.Open(logName)
		if err != nil {
			log.Fatalf("Error opening file: %s", err)
		}
		defer f.Close()

		logs = append(logs, mergedlog.LogFile{Alias: alias, Scanner: bufio.NewScanner(f), AggLog: aggLog, Color: palette[colorIndex]})
		colorIndex = (colorIndex + 1) % 8

		if len(alias) > maxLogNameLength {
			maxLogNameLength = len(alias)
		}
	}

	var oldestStampSeen int64 = MAX_INT
	var lastTimeRead int64

	for len(logs) > 0 {
		// Process log list backwards so that we can delete entries as necessary
		for i := len(logs) - 1; i >= 0; i-- {
			if logs[i].Scanner.Scan() {
				lineCount++
				line := logs[i].Scanner.Text()
				matches := gfeLogLineRE.FindStringSubmatch(line)

				if matches != nil {
					stamp := strings.TrimSpace(matches[1])
					t, err := time.Parse(stampFormat, stamp)
					if err != nil {
						log.Printf("Unable to parse date stamp '%s': %s", stamp, err)
						continue
					}
					lastTimeRead = t.UnixNano()

					l := &mergedlog.LogLine{Alias: logs[i].Alias, UTime: t.UnixNano(), Text: line, Color: logs[i].Color}
					logs[i].Insert(l)
				} else {
					x := logs[i].InsertTimeless(line)
					v, _ := x.Value.(*mergedlog.LogLine)
					lastTimeRead = v.UTime
				}

				if lastTimeRead < oldestStampSeen {
					oldestStampSeen = lastTimeRead
				}

			} else {
				logs = append(logs[:i], logs[i+1:]...)
			}
		}

		if lineCount%FLUSH_BATCH_SIZE == 0 {
			flushLogs(oldestStampSeen, aggLog, maxLogNameLength)
			oldestStampSeen = MAX_INT
		}
	}

	flushLogs(MAX_INT, aggLog, maxLogNameLength)
}

func flushLogs(highestStamp int64, aggLog *list.List, maxLogNameLength int) {
	for e := aggLog.Front(); e != nil; e = aggLog.Front() {
		line, _ := e.Value.(*mergedlog.LogLine)
		if line.UTime < highestStamp {
			format := "%s%" + strconv.Itoa(len(line.Alias)-maxLogNameLength) + "s[%s] %s%s\n"
			if userColor != "off" {
				fmt.Printf(format, line.Color, "", line.Alias, line.Text, resetColor)
			} else {
				fmt.Printf(format, "", "", line.Alias, line.Text, "")
			}
			aggLog.Remove(e)
		} else {
			break
		}
	}
}
