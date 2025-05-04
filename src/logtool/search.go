package logtool

import (
	"bufio"
	"encoding/json"
	"fmt"
	"github.com/fsnotify/fsnotify"
	"github.com/gin-gonic/gin"
	"github.com/jom-io/gorig/utils/errors"
	"github.com/jom-io/gorig/utils/logger"
	"github.com/spf13/cast"
	"go.uber.org/zap"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func ListLogFiles(opts SearchOptions) (map[string]string, error) {
	if opts.RootDir == "" {
		opts.RootDir = "."
	}
	logDir := filepath.Join(opts.RootDir, ".logs")
	result := make(map[string]string)

	if _, err := os.Stat(logDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("log dir not found: %s", logDir)
	}

	catDirList, err := os.ReadDir(logDir)
	if err != nil {
		return nil, fmt.Errorf("read log dir error: %v", err)
	}

	categories := opts.Categories
	if len(categories) == 0 {
		categories = Categories
	}

	newCategories := make([]string, 0)
	for _, catDir := range catDirList {
		for _, cat := range categories {
			if catDir.Name() == cat {
				newCategories = append(newCategories, cat)
			}
		}
	}

	for _, cat := range newCategories {
		catDir := filepath.Join(logDir, cat)
		_ = filepath.Walk(catDir, func(path string, info fs.FileInfo, err error) error {
			if err != nil {
				logger.Warn(nil, "skip file", zap.Error(err))
				return nil
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".jsonl") {
				if strings.HasPrefix(info.Name(), cat) {
					fileTime := strings.TrimSuffix(info.Name(), ".jsonl")
					if strings.HasPrefix(fileTime, cat+"-") {
						fileTime = strings.TrimPrefix(fileTime, cat+"-")
						parseTime, e := time.Parse("2006-01-02T15-04-05.000", fileTime)
						if e != nil {
							//logger.Warn(nil, "parse file time error", zap.Error(e))
							return nil
						}
						if opts.StartTime != "" {
							startTime, _ := time.Parse("2006-01-02 15:04:05", opts.StartTime)
							if parseTime.Before(startTime) {
								return nil
							}
						}
						if opts.EndTime != "" {
							endTime, _ := time.Parse("2006-01-02 15:04:05", opts.EndTime)
							if parseTime.After(endTime) {
								return nil
							}
						}
					}
					result[path] = info.Name()
				}
			}
			return nil
		})
		//if err != nil {
		//	logger.Warn(nil, "walk log dir error", zap.Error(err))
		//	//fmt.Printf("warn: %v\n", err)
		//	return nil, nil
		//}
	}

	return result, nil
}

func SearchLogs(opts SearchOptions) ([]MatchedRecord, *errors.Error) {
	if opts.Size <= 0 {
		opts.Size = 10
	}

	files, err := ListLogFiles(opts)
	if err != nil {
		return nil, errors.Verify(err.Error())
	}

	var matchedRecords []MatchedRecord
	var matchedCount int
	lastRecordFound := false
	startProcessing := false

	if opts.LastPath == "" {
		startProcessing = true
	}

	for filePath, _ := range files {
		if !startProcessing && filePath == opts.LastPath {
			startProcessing = true
		}

		if !startProcessing {
			continue
		}

		f, err := os.Open(filePath)
		if err != nil {
			return nil, errors.Verify(fmt.Sprintf("open file error: %v", err))
		}
		defer f.Close()

		scanner := bufio.NewScanner(f)
		var lineNumber int64 = 0
		for scanner.Scan() {
			lineNumber++
			if filePath == opts.LastPath && lineNumber <= opts.LastLine {
				continue // Continue from last line
			}

			line := scanner.Text()
			if strings.TrimSpace(line) == "" {
				continue
			}

			var recMap map[string]interface{}
			if errR := json.Unmarshal([]byte(line), &recMap); errR != nil {
				//logger.Error(nil, "unmarshal record error", zap.Error(errR))
				continue
			}

			rec := map2LogRecord(recMap)

			if matchRecord(*rec, opts) {
				matchedRecords = append(matchedRecords, MatchedRecord{
					FilePath:   filePath,
					LineNumber: lineNumber,
					Record:     rec,
				})
				matchedCount++

				// If matched count reaches the limit, stop searching
				if matchedCount >= opts.Size {
					lastRecordFound = true
					break
				}
			}
		}

		if lastRecordFound {
			break
		}
	}

	return matchedRecords, nil
}

// matchRecord Match a log record with search options
func matchRecord(r LogRecord, opts SearchOptions) bool {
	// 1. level
	if opts.Level != "" && !strings.EqualFold(r.Level, opts.Level) {
		return false
	}

	// 2. trace ID
	if opts.TraceID != "" && r.TraceID != opts.TraceID {
		return false
	}

	// 3. Time range
	pt, err := r.ParsedTime()
	if err != nil {
		return false
	}
	if strings.TrimSpace(opts.StartTime) != "" {
		startTime, err := time.Parse("2006-01-02 15:04:05", opts.StartTime)
		if err != nil {
			return false
		}
		if !startTime.IsZero() && pt.Before(startTime) {
			return false
		}
	}

	if strings.TrimSpace(opts.EndTime) != "" {
		endTime, err := time.Parse("2006-01-02 15:04:05", opts.EndTime)
		if err != nil {
			return false
		}
		if !endTime.IsZero() && pt.After(endTime) {
			return false
		}
	}

	// 4. Keyword
	if opts.Keyword != "" {
		kw := opts.Keyword
		if strings.Contains(r.Msg, kw) {
			return true
		}
		if strings.Contains(r.Error, kw) {
			return true
		}
		if r.Data != nil {
			for _, v := range r.Data {
				if strings.Contains(v, kw) {
					return true
				}
			}
			return false
		}
		return false
	}

	return true
}

type ContextLogLine struct {
	FilePath   string     `json:"path"`
	LineNumber int64      `json:"line"`
	Content    string     `json:"content"`
	Record     *LogRecord `json:"record"`
}

// FetchContextLines Read context lines around the center line
func FetchContextLines(filePath string, centerLine, contextRange int64) ([]ContextLogLine, *errors.Error) {
	if centerLine < 1 {
		return nil, errors.Verify("center line must be greater than 0")
	}
	if contextRange < 0 {
		contextRange = 0
	}

	startLine := centerLine - contextRange
	if startLine < 1 {
		startLine = 1
	}
	endLine := centerLine + contextRange

	var result []ContextLogLine

	f, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Verify(fmt.Sprintf("open file error: %v", err))
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	var currentLine int64 = 0
	for scanner.Scan() {
		currentLine++
		if currentLine < startLine {
			continue
		}
		if currentLine > endLine {
			break
		}

		text := scanner.Text()
		dataMap := map[string]interface{}{}
		if err := json.Unmarshal([]byte(text), &dataMap); err != nil {
			//logger.Error(nil, "unmarshal record error", zap.Error(err))
			continue
		}
		rec := map2LogRecord(dataMap)
		result = append(result, ContextLogLine{
			FilePath:   filePath,
			LineNumber: currentLine,
			Content:    text,
			Record:     rec,
		})
	}

	return result, nil
}

// MonitorLogs monitors logs based on categories and conditions in real-time
func MonitorLogs(ctx *gin.Context, opts SearchOptions) *errors.Error {

	ctx.Writer.Header().Set("Access-Control-Allow-Origin", "*")
	ctx.Writer.Header().Set("Content-Type", "text/event-stream")
	ctx.Writer.Header().Set("Cache-Control", "no-cache")
	ctx.Writer.Header().Set("Connection", "keep-alive")
	ctx.Writer.Flush()

	// Initialize fsnotify watcher
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return errors.Verify(fmt.Sprintf("unable to create watcher: %v", err))
	}
	defer watcher.Close()

	// Get the files to monitor based on categories
	files, err := ListLogFiles(opts)
	if err != nil {
		return errors.Verify(fmt.Sprintf("unable to list log files: %v", err))
	}

	// Add files to the watcher
	for file, _ := range files {
		err = watcher.Add(file)
		if err != nil {
			return errors.Verify(fmt.Sprintf("unable to add file to watcher: %v", err))
		}
	}

	ctx.SSEvent("message", "monitoring started")
	ctx.Writer.Flush()
	defer func() {
		ctx.SSEvent("message", "monitoring stopped")
		ctx.Writer.Flush()
	}()
	// Monitor file events and process new logs
	for {
		select {
		case event := <-watcher.Events:
			if event.Op&fsnotify.Write == fsnotify.Write {
				result, errP := processFile(event.Name, opts)
				if errP != nil {
					return errP
				}
				if result != nil {
					//fmt.Println("Matching log:", *result)
					ctx.SSEvent("message", result)
					ctx.Writer.Flush()
				}
			}
		case err := <-watcher.Errors:
			if err != nil {
				return errors.Verify(fmt.Sprintf("watcher error: %v", err))
			}
		case <-ctx.Request.Context().Done():
			return nil
		}
	}
}

// processFile processes a log file and prints the last matching record
func processFile(filePath string, opts SearchOptions) (*MatchedRecord, *errors.Error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, errors.Verify(fmt.Sprintf("unable to open log file: %v", err))
	}
	defer f.Close()

	lastRec, errR := readLastRecord(f)
	if errR != nil {
		return nil, errors.Verify(fmt.Sprintf("unable to read last record: %v", errR))
	}

	if lastRec != nil && matchRecord(*lastRec.Record, opts) {
		return lastRec, nil
	}
	return nil, nil
}

// map2LogRecord
func map2LogRecord(dataMap map[string]interface{}) *LogRecord {
	rec := &LogRecord{}
	for k, v := range dataMap {
		switch k {
		case "level":
			rec.Level = cast.ToString(v)
		case "time":
			rec.Time = cast.ToString(v)
		case "_trace_id_":
			rec.TraceID = cast.ToString(v)
		case "msg":
			rec.Msg = cast.ToString(v)
		case "error":
			rec.Error = cast.ToString(v)
		default:
			if rec.Data == nil {
				rec.Data = map[string]string{}
			}
			rec.Data[k] = cast.ToString(v)
			if rec.Data[k] == "" {
				jsonBytes, _ := json.Marshal(v)
				rec.Data[k] = string(jsonBytes)
			}
		}
	}
	return rec
}

// readLastRecord
func readLastRecord(f *os.File) (*MatchedRecord, *errors.Error) {
	fi, err := f.Stat()
	if err != nil {
		return nil, errors.Verify(fmt.Sprintf("unable to get file info: %v", err))
	}

	buf := make([]byte, 1024)
	offset := int64(0)
	for {
		offset += 1024
		_, err := f.Seek(-offset, 2)
		if err != nil {
			return nil, errors.Verify(fmt.Sprintf("unable to seek file: %v", err))
		}

		n, err := f.Read(buf)
		if err != nil {
			return nil, errors.Verify(fmt.Sprintf("unable to read file: %v", err))
		}

		for i := n - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				line := string(buf[i+1 : n])
				if strings.TrimSpace(line) == "" {
					continue
				}

				dataMap := map[string]interface{}{}
				if err := json.Unmarshal([]byte(line), &dataMap); err != nil {
					//logger.Error(nil, "unmarshal record error", zap.Error(err))
					return nil, nil
				}

				record := map2LogRecord(dataMap)
				result := &MatchedRecord{
					FilePath:   f.Name(),
					LineNumber: -1,
					Record:     record,
				}
				return result, nil
			}
		}

		if offset > fi.Size() {
			break
		}
	}

	return nil, errors.Verify("no complete record found")
}

// DownloadLogs downloads logs based on categories and conditions
func DownloadLogs(ctx *gin.Context, path string) *errors.Error {
	if strings.Contains(path, "..") || !strings.HasSuffix(path, ".jsonl") {
		return errors.Verify("invalid log file")
	}

	if _, err := os.Stat(path); os.IsNotExist(err) {
		return errors.Verify("log file does not exist")
	}

	ctx.Header("Content-Type", "application/octet-stream")
	ctx.Header("Content-Disposition", "attachment; filename="+filepath.Base(path))

	ctx.File(path)
	return nil
}
