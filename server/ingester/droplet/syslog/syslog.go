/*
 * Copyright (c) 2023 Yunshan Networks
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package syslog

import (
	"bytes"
	"errors"
	"log/syslog"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/deepflowio/deepflow/server/libs/codec"
	logging "github.com/op/go-logging"

	"github.com/deepflowio/deepflow/server/libs/queue"
	"github.com/deepflowio/deepflow/server/libs/receiver"
	"github.com/deepflowio/deepflow/server/libs/utils"
)

var log = logging.MustGetLogger("droplet.syslog")

const (
	_FILE_FEED        = 3600 // 3600秒
	_FILE_BUFFER_SIZE = 2048
	QUEUE_BATCH_SIZE  = 1024
)

const (
	LOG_TYPE   = "daemon"
	LOG_MODULE = "trident"
)

type fileWriter struct {
	fileBuffer *DailyRotateWriter

	feed int
}

type syslogWriter struct {
	directory        string
	logToFileEnabled bool

	index   int
	fileMap map[uint32]*fileWriter
	in      queue.QueueReader

	esLogger *ESLogger
}

func (w *syslogWriter) create(ip net.IP) *fileWriter {
	fileName := filepath.Join(w.directory, ip.String()+".log")
	return &fileWriter{NewRotateWriter(fileName), _FILE_FEED}
}

func (w *syslogWriter) write(writer *fileWriter, bytes []byte) {
	writer.fileBuffer.Write(bytes)
	writer.feed = _FILE_FEED
}

func (w *syslogWriter) writeFile(ip net.IP, bytes []byte) {
	if !w.logToFileEnabled {
		return
	}
	if bytes == nil {
		// tick
		for key, value := range w.fileMap {
			value.fileBuffer.Flush()
			value.feed--
			if value.feed == 0 {
				value.fileBuffer.Close()
				delete(w.fileMap, key)
			}
		}
		return
	}
	hash := utils.GetIpHash(ip)
	if _, in := w.fileMap[hash]; !in {
		w.fileMap[hash] = w.create(ip)
	}
	w.write(w.fileMap[hash], bytes)
}

func (w *syslogWriter) writeES(bytes []byte) {
	if w.esLogger == nil {
		return
	}
	if bytes == nil {
		// tick
		w.esLogger.Flush()
		return
	}
	if esLog, err := parseSyslog(bytes); err == nil {
		w.esLogger.Log(esLog)
	} else {
		log.Debug("invalid log message for es:", err)
	}
}

func parseSyslog(bs []byte) (*ESLog, error) {
	// example log
	// 2020-11-23T16:56:35+08:00 dfi-153 trident[8642]: [INFO] synchronizer.go:397 update FlowAcls version  1605685133 to 1605685134
	columns := bytes.SplitN(bs, []byte{' '}, 6)
	if len(columns) != 6 {
		return nil, errors.New("not enough columns in log")
	}
	esLog := ESLog{Type: LOG_TYPE, Module: LOG_MODULE}
	datetime, err := time.Parse(time.RFC3339, string(columns[0]))
	if err != nil {
		return nil, err
	}
	esLog.Timestamp = uint32(datetime.Unix())
	esLog.Host = string(columns[1])
	severity := syslog.Priority(0)
	switch string(columns[3]) {
	case "[INFO]":
		severity = syslog.LOG_INFO
	case "[WARN]":
		severity = syslog.LOG_WARNING
	case "[ERRO]", "[ERROR]":
		severity = syslog.LOG_ERR
	default:
		return nil, errors.New("ignored log level: " + string(columns[3]))
	}
	esLog.Severity = strconv.Itoa(int(severity))
	esLog.SyslogTag = string(columns[4])
	esLog.Message = string(columns[5])
	return &esLog, nil
}

func NewSyslogWriter(in queue.QueueReader, logToFileEnabled, esEnabled bool, directory string, esAddresses []string, esUsername, esPassword string) *syslogWriter {
	if logToFileEnabled {
		if err := os.MkdirAll(directory, os.ModePerm); err != nil {
			log.Warningf("cannot output syslog to directory %s: %v", directory, err)
			return &syslogWriter{}
		}
	}
	var esLogger *ESLogger
	if esEnabled {
		esLogger = NewESLogger(esAddresses, esUsername, esPassword)
	}
	writer := &syslogWriter{
		logToFileEnabled: logToFileEnabled,
		directory:        directory,
		fileMap:          make(map[uint32]*fileWriter, 8),
		in:               in,
		esLogger:         esLogger,
	}

	go writer.run()
	return writer
}

func (w *syslogWriter) run() {
	packets := make([]interface{}, QUEUE_BATCH_SIZE)
	decoder := &codec.SimpleDecoder{}

	for {
		n := w.in.Gets(packets)
		for i := 0; i < n; i++ {
			value := packets[i]
			if receiveBuffer, ok := value.(*receiver.RecvBuffer); ok {
				bytes := receiveBuffer.Buffer[receiveBuffer.Begin:receiveBuffer.End]
				if receiveBuffer.SocketType == receiver.UDP {
					w.writeFile(receiveBuffer.IP, bytes)
					w.writeES(bytes)
				} else {
					decoder.Init(bytes)
					for !decoder.IsEnd() {
						syslog := decoder.ReadBytes()
						if syslog != nil {
							w.writeFile(receiveBuffer.IP, syslog)
							w.writeES(syslog)
						}
					}
				}
				receiver.ReleaseRecvBuffer(receiveBuffer)
			} else if value == nil { // flush ticker
				w.writeFile(nil, nil)
				w.writeES(nil)
			} else {
				log.Warning("get queue data type wrong")
			}
		}
	}
}
