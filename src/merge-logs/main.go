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

var stampFormat = "2006/01/02 15:04:05.000 MST"
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
	duration := flag.Int64("duration", MAX_INT, "start of range of logs")
	rangeStopStr := flag.String("stop", "", "start of range of logs")
	flag.Parse()

	rangeStop := MAX_INT
	if *rangeStopStr != "" {
		t, err := time.Parse(stampFormat, *rangeStopStr)
		if err != nil {
			log.Fatalf("Unable to parse '%s' as timestamp", rangeStopStr)
		}
		rangeStop = t.UnixNano()
		// if duration is larger than the stop time, adjust it so that start
		// time is positive
		if *duration > t.Unix() {
			*duration = t.Unix()
		}
	}

	if userColor == "light" {
		palette[0] = ansi.ColorCode("234") // blackish
	}

	gfeLogLineRE, err := regexp.Compile(`^\[\w+ (([^ ]* ){3}).*`)
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

		logFile := mergedlog.LogFile{
			Alias:      alias,
			Scanner:    bufio.NewScanner(f),
			AggLog:     aggLog,
			Color:      palette[colorIndex],
			RangeStart: rangeStop - int64(time.Duration(*duration)*time.Second),
			RangeStop:  rangeStop,
		}
		logFile.Scanner.Split(mergedlog.ScanLogEntries)
		logs = append(logs, logFile)
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
					if x := logs[i].InsertTimeless(line); x != nil {
						v, _ := x.Value.(*mergedlog.LogLine)
						lastTimeRead = v.UTime
					}
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
		entry, _ := e.Value.(*mergedlog.LogLine)
		if entry.UTime < highestStamp {
			format := "%s%" + strconv.Itoa(len(entry.Alias)-maxLogNameLength) + "s[%s] %s%s\n"
			for _, line := range strings.Split(entry.Text, "\n") {
				if userColor != "off" {
					fmt.Printf(format, entry.Color, "", entry.Alias, line, resetColor)
				} else {
					fmt.Printf(format, "", "", entry.Alias, line, "")
				}
			}
			aggLog.Remove(e)
		} else {
			break
		}
	}
}
