package alarm

import (
	"fmt"
	"github.com/cellcrypto/open-dangnn-pool/storage/mysql"
	"github.com/cellcrypto/open-dangnn-pool/storage/redis"
	"github.com/cellcrypto/open-dangnn-pool/storage/types"
	"github.com/cellcrypto/open-dangnn-pool/util"
	"github.com/slack-go/slack"
	"log"
	"sync"
	"time"
)

type Config struct {
	Enabled 				bool   		`json:"enabled"`
	Coin					string
	AlarmCheckInterval 		string  	`json:"alarmCheckInterval"`		// Alarm check cycle
	AlarmCheckWaitInterval 	string  	`json:"alarmCheckWaitInterval"`	// Alarm waiting time
	SlackBotToken 			string 		`json:"slackBotToken"`
	SlackChannelId			string 		`json:"slackChannelId"`
}

type Entry struct {
	updateTime int64
	miner string
}

type AlramServer struct {
	config  			*Config
	startedAt   		int64
	alarmList   		map[string]*types.InboundIdList
	alarmMiners 		map[string]*Entry
	alarmMinersMu   	sync.RWMutex
	storage 			*redis.RedisClient
	db  				*mysql.Database
	AlarmCheckWaitSec   int64
	api 				*slack.Client
}

func Start(cfg *Config, storage *redis.RedisClient, db *mysql.Database) *AlramServer  {
	a := &AlramServer{
		config: 		cfg,
		startedAt: 		util.MakeTimestamp(),
		alarmMiners:    make(map[string]*Entry),
		storage: storage,
		db: db,
	}

	alarmChecktIntv := util.MustParseDuration(a.config.AlarmCheckInterval)
	alarmCheckTimer := time.NewTimer(alarmChecktIntv)

	// a.LastBeatCheckSec = util.MustParseDuration(a.config.LastBeatCheckTime)
	a.AlarmCheckWaitSec = util.MustParseDuration(a.config.AlarmCheckWaitInterval).Milliseconds() / 1000

	a.api = slack.New(a.config.SlackBotToken)

	// Make an alarm list.
	a.MakeAlarmList()

	log.Printf("Set Alaram check every %v", alarmChecktIntv)

	a.alarmProcess()

	go func() {
		for {
			select {
			case <-alarmCheckTimer.C:
				a.alarmProcess()
				alarmCheckTimer.Reset(alarmChecktIntv)
			}
		}
	}()
	
	return a
}

func (a *AlramServer) alarmProcess()  {

	now := util.MakeTimestamp() / 1000
	// Call the alarm target from DB.
	a.alarmMinersMu.RLock()
	alarmList := a.alarmList
	a.alarmMinersMu.RUnlock()

 	if alarmList == nil || len(alarmList) == 0 {
		fmt.Printf("[alarmProcess] non-existent alarm list.\n")
		return
	}

	var alarmIds []string
	for _, alarm := range alarmList {
		entry, exist := a.alarmMiners[alarm.Id]
		if exist == true {
			// If the update time has passed one hour, delete it from the list and proceed.
			if entry.updateTime > now {
				continue
			}
			// Check if the alarm target is in alarmMiners and exclude it.
			delete(a.alarmMiners,alarm.Id)
		}

		alarmIds = append(alarmIds,alarm.Id)
	}

	fmt.Printf("[alarmProcess] searching alarm total list: %v send list: %v\n", len(alarmList), len(alarmIds))
	if alarmIds == nil || len(alarmIds) == 0 {
		// There is no one to send the alarm.
		return
	}

	// Check active status in Redis.
	var sendSlackList string
	for _, login := range alarmIds {
		res, err := a.storage.GetAlarmBeat(login)
		if err != nil {
			fmt.Println(err)
			continue
		}
		if res == true {
			continue
		}

		alarmInfo, _ := alarmList[login]

		a.alarmMiners[login] = &Entry{
			updateTime: now + a.AlarmCheckWaitSec,
			miner:      login,
		}

		//
		switch alarmInfo.Alarm {
		case "slack":
			sendSlackList += "occurrence of abnormal system: (" + a.config.Coin + ")"+alarmInfo.Desc + "[" + alarmInfo.Id + "]\n"
		case "mail":
		}
	}

	// Send a message to Slack.
	if len(sendSlackList) > 0 {
		a.SendMessageToSlack(sendSlackList)
		fmt.Printf("[alarmProcess]\n %v]\n", sendSlackList)
	}
}


func (a *AlramServer) SendMessageToSlack(msg string) error {

	attachment := slack.Attachment{
		Pretext: "*It's work time HUMAN!!!!!* (" + a.config.Coin + ")",
		Text:    msg,
	}

	channelID, timestamp, err := a.api.PostMessage(
		a.config.SlackChannelId,
		slack.MsgOptionAttachments(attachment),
	)

	if err != nil {
		fmt.Printf("SendMessageToSlack %v\n", err)
		return err
	}

	fmt.Printf("slack message post successfully %s at %s\n", channelID, timestamp)
	return nil
}

func (a *AlramServer) MakeAlarmList() {
	tmpAlarmList, err := a.db.GetAlarmInfo()
	if err != nil {
		panic("Failed to read alarm list.\n")
		return
	}

	fmt.Printf("[MakeAlarmList] Load alarm list size: %v\n", len(tmpAlarmList))

	a.alarmMinersMu.Lock()
	a.alarmList = tmpAlarmList
	a.alarmMinersMu.Unlock()
}