package plogger

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
)

var logger *Logger

type Msg struct {
	content 	string
	msgType 	int
	msgErr      int
	roundHeight int64
	height      int64
	addr    	string
	addr2 		string
	insertTime time.Time
}

type Logger struct {
	MsgQueue chan Msg
	Db LogDB

	maxWorkers int
	maxQueueSize int

	where string
	logTableName string

	//logData []LogData

	lock       sync.Mutex
	InsertCnt  int             // insert count
	sqlBuilder strings.Builder // SQL STATEMENT
}
const (
	maxQueueSize = 20000
	maxWorkers = 2
	insertSize = 5000
)

const (
	LogTypePendingBlock = 1000
	LogTypeMaturedBlock = 2000
	LogTypePaymentWork	= 3000

	LogTypeSystem 		= 7000
)

const (
	LogErrorNothing	= 0
	LogSubTypeImmaturedBlock = 201
	LogSubTypeOrphanBlcok = 202
	LogSubTypeLostBlcok = 203
	LogSubTypePaymentLock 			= 301
	LogSubTypePaymentTransaction 	= 302
	LogSubTypePaymentUnlock 		= 303
	LogSubTypePaymentWriteDB 		= 304
	LogSubTypePaymentTxWait 		= 305
	LogSubTypePaymentTxComplete 	= 306
	LogSubTypeError = 10000
	LogSubTypeSystemRoundInfoRedis = 10001
	LogErrorNothingRoundBlock = 10002
)

type LogDB interface {
	InsertSqlLog(sql *string)
}

func New(db LogDB, where string, logTableName string) *Logger {


	// create job channel
	jobs := make(chan Msg, maxQueueSize)

	logger = &Logger{
		MsgQueue:     jobs,
		Db:           db,
		maxWorkers:   maxWorkers,
		maxQueueSize: maxQueueSize,
		where : where,
		logTableName: logTableName,
		// logData: make([]LogData,maxWorkers),
	}

	// create workers
	logger.init()

	return logger
}

func Save() {

}

func (l *Logger) init() *Logger {
	for i := 1; i <= l.maxWorkers; i++ {
		go func(i int) {
			for m := range l.MsgQueue {
				l.doWork(i, m)
				if len(l.MsgQueue) == 0 {
					l.Save(i, 0)
				}
			}
		}(i)
	}
	return l
}


func InsertSystemError(logType int, roundHeight int64, height int64, format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	log.Printf(s)
	InsertLog(s, logType, LogSubTypeError,roundHeight, height,"","" )
}

func InsertSystemPaymemtError(logType int, addr string, addr2 string, format string, v ...interface{}) {
	s := fmt.Sprintf(format, v...)
	log.Printf(s)
	InsertLog(s, logType, LogSubTypeError,0, 0,addr,addr2 )
}

func InsertLog(content string, msgType int, msgErr int, roundHeight int64, height int64, addr, addr2 string)  {
	msg := Msg{
		content:     content,
		msgType:     msgType,
		msgErr:      msgErr,
		roundHeight: roundHeight,
		height:      height,
		addr:        addr,
		addr2:       addr2,
		insertTime:  time.Now(),
	}

	logger.MsgQueue <- msg
}

func (l *Logger) insertLog(msg Msg) {
	l.lock.Lock()
	defer l.lock.Unlock()

	if l.InsertCnt == 0 {
		l.sqlBuilder = strings.Builder{}
		l.sqlBuilder.WriteString(fmt.Sprintf("INSERT INTO %v(`msg_type`,`msg_err`, `where`, `round_height`, `height`, `addr`, `addr2`, `msg`, `insert_time`) VALUES (\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\")", l.logTableName, msg.msgType, msg.msgErr, l.where, msg.roundHeight, msg.height, msg.addr, msg.addr2, msg.content, msg.insertTime.Format("2006-01-02 15:04:05.000")))
	} else {
		l.sqlBuilder.WriteString(fmt.Sprintf(",(\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\")", msg.msgType, msg.msgErr, l.where, msg.roundHeight, msg.height, msg.addr, msg.addr2, msg.content, msg.insertTime.Format("2006-01-02 15:04:05.000")))
	}
	l.InsertCnt++
}

func (l *Logger) doWork(id int, msg Msg) {
	l.insertLog(msg)
	if l.InsertCnt > insertSize {
		l.Save(id, insertSize)
	}
}

func (l *Logger) Save(id int,insertSize int) {

//	start := time.Now()
	tmpString, size := l.checkIns(insertSize)
	if size <= 0 {
		return
	}

	if tmpString != nil {
		l.Db.InsertSqlLog(tmpString)
	}

//	log.Printf("id %v doWork (gap: %v). size:%v\n", id, time.Since(start), size)
}

func (l *Logger) Close() {
	// Save all log messages.
	for m := range l.MsgQueue {
		l.doWork(0, m)
	}

	for i := 1; i <= l.maxWorkers; i++ {
		l.Save(i, 0)
	}
}



func (l *Logger) checkIns(insertSize int) (*string, int) {
	var tmpString *string
	l.lock.Lock()
	defer l.lock.Unlock()

	ret := l.InsertCnt
	if l.InsertCnt > insertSize {
		l.InsertCnt = 0
		tmpLog := l.sqlBuilder.String()
		tmpString = &tmpLog
		l.sqlBuilder =strings.Builder{}
	} else {
		return nil, ret
	}
	return tmpString, ret
}