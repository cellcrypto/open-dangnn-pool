package policy

import (
	"fmt"
	"github.com/cellcrypto/open-dangnn-pool/storage/mysql"
	"github.com/cellcrypto/open-dangnn-pool/storage/redis"
	"log"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cellcrypto/open-dangnn-pool/util"
	mapset "github.com/deckarep/golang-set"
)

type Config struct {
	Workers         int     `json:"workers"`
	Banning         Banning `json:"banning"`
	Limits          Limits  `json:"limits"`
	ResetInterval   string  `json:"resetInterval"`
	RefreshInterval string  `json:"refreshInterval"`	// Deprecated. Use Alarm feature instead.
	MinerShareCheckBeatInterval	string `json:"minerShareCheckBeatInterval"`
}

type Limits struct {
	Enabled   bool   `json:"enabled"`
	Limit     int32  `json:"limit"`
	Grace     string `json:"grace"`
	LimitJump int32  `json:"limitJump"`
}

type Banning struct {
	Enabled        bool    `json:"enabled"`
	IPSet          string  `json:"ipset"`
	Timeout        int64   `json:"timeout"`
	InvalidPercent float32 `json:"invalidPercent"`
	CheckThreshold int32   `json:"checkThreshold"`
	MalformedLimit int32   `json:"malformedLimit"`
}

type Stats struct {
	sync.Mutex
	// We are using atomic with LastBeat,
	// so moving it before the rest in order to avoid alignment issue
	LastBeat      int64
	BannedAt      int64
	ValidShares   int32
	InvalidShares int32
	Malformed     int32
	ConnLimit     int32
	Banned        int32
	Logined		 bool
}

type AlarmBeat struct {
	updateAt int64
	login string
}

type PolicyServer struct {
	sync.RWMutex
	statsMu    sync.Mutex
	config     *Config
	stats      map[string]*Stats
	banChannel chan string
	startedAt  int64
	grace      int64
	timeout    int64
	inboundIp mapset.Set
	allowAllIp bool
	inboundId mapset.Set
	whitelist  mapset.Set
	allowAllId bool
	storage   *redis.RedisClient
	db 		   *mysql.Database

	alarmBeatsMu sync.RWMutex
	alarmBeats map[string]*AlarmBeat
	beatIntv time.Duration
}

func Start(cfg *Config, storage *redis.RedisClient, db *mysql.Database) *PolicyServer {
	s := &PolicyServer{config: cfg, startedAt: util.MakeTimestamp()}
	grace := util.MustParseDuration(cfg.Limits.Grace)
	s.grace = int64(grace / time.Millisecond)
	s.banChannel = make(chan string, 64)
	s.stats = make(map[string]*Stats)
	s.alarmBeats = make(map[string]*AlarmBeat)
	s.storage = storage
	s.db = db
	s.refreshState()

	timeout := util.MustParseDuration(s.config.ResetInterval)
	s.timeout = int64(timeout / time.Millisecond)

	resetIntv := util.MustParseDuration(s.config.ResetInterval)
	resetTimer := time.NewTimer(resetIntv)
	log.Printf("Set policy stats reset every %v", resetIntv)

	minerShareCheckBeatIntv := util.MustParseDuration(s.config.MinerShareCheckBeatInterval)
	s.beatIntv = minerShareCheckBeatIntv
	s.InitAlarmBeat(minerShareCheckBeatIntv)

	go func() {
		for {
			select {
			case <-resetTimer.C:
				s.resetStats()
				resetTimer.Reset(resetIntv)
			//case <-refreshTimer.C:
			//	s.refreshState()
			//	refreshTimer.Reset(refreshIntv)
			}
		}
	}()

	for i := 0; i < s.config.Workers; i++ {
		s.startPolicyWorker()
	}
	log.Printf("Running with %v policy workers", s.config.Workers)
	return s
}

func (s *PolicyServer) startPolicyWorker() {
	go func() {
		for {
			select {
			case ip := <-s.banChannel:
				s.doBan(ip)
			}
		}
	}()
}

func (s *PolicyServer) resetStats() {
	now := util.MakeTimestamp()
	banningTimeout := s.config.Banning.Timeout * 1000
	total := 0
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	for key, m := range s.stats {
		lastBeat := atomic.LoadInt64(&m.LastBeat)
		bannedAt := atomic.LoadInt64(&m.BannedAt)

		if now-bannedAt >= banningTimeout {
			atomic.StoreInt64(&m.BannedAt, 0)
			if atomic.CompareAndSwapInt32(&m.Banned, 1, 0) {
				log.Printf("Ban dropped for %v", key)
				delete(s.stats, key)
				total++
			}
		}
		if now-lastBeat >= s.timeout {
			delete(s.stats, key)
			total++
		}
	}
	log.Printf("Flushed stats for %v IP addresses", total)
}

func (s *PolicyServer) refreshState() {
	// Reads the list of IPs to receive from the pool server from DB.
	s.RefreshInboundIP()
	// Reads the list of ids to receive from the pool server from DB.
	s.RefreshInboundID()
	// Read the white list.
	s.RefreshBanWhiteList()
	log.Println("!! Policy state refresh complete")
}

func (s *PolicyServer) RefreshBanWhiteList() {

	tmpWhiteList, err := s.db.GetBanWhitelist()

	s.Lock()
	defer s.Unlock()

	if err != nil {
		log.Printf("Failed to get whitelist from backend: %v", err)
		s.whitelist = nil
	} else {
		s.whitelist = tmpWhiteList
	}

	log.Println("!! Policy white list refresh complete")
}

func (s *PolicyServer) RefreshInboundIP() {
	inboundIp, err := s.db.GetIpInboundList()

	s.Lock()
	defer s.Unlock()

	if err != nil {
		log.Printf("Failed to get blacklist from backend: %v", err)
		s.inboundIp = nil
	} else {
		for _, origin := range inboundIp {
			if origin.Ip == "*" {
				if origin.Allowed == true {
					s.allowAllIp = true
				}
			}
		}
		tmpInboundIp := mapset.NewSet()
		for _, origin := range inboundIp {
			if s.allowAllIp == true {
				// black list
				if origin.Allowed == false {
					tmpInboundIp.Add(origin.Ip)
				}
			} else {
				// white list
				if origin.Allowed == true {
					tmpInboundIp.Add(origin.Ip)
				}
			}
		}
		s.inboundIp = tmpInboundIp
	}

	log.Println("!! Policy inbound ip list refresh complete")
}

func (s *PolicyServer) RefreshInboundID() {
	inboundId, err := s.db.GetIdInboundList()

	s.Lock()
	defer s.Unlock()

	if err != nil {
		log.Printf("Failed to get blacklist from backend: %v", err)
		s.inboundId = nil
	} else {
		for _, origin := range inboundId {
			if origin.Id == "*" {
				if origin.Allowed == true {
					s.allowAllId = true
				}
			}
		}
		tmpInboundId := mapset.NewSet()
		alarmBeat := mapset.NewSet()
		for _, origin := range inboundId {
			if s.allowAllId == true {
				// black list
				if origin.Allowed == false {
					tmpInboundId.Add(origin.Id)
				}
			} else {
				// white list
				if origin.Allowed == true {
					tmpInboundId.Add(origin.Id)
				}
			}

			if origin.Alarm != "none" {
				alarmBeat.Add(origin.Id)
			}
		}
		s.inboundId = tmpInboundId

		var tmpDeleteList []string
		for _, value := range s.alarmBeats {
			if !alarmBeat.Contains(value.login) {
				tmpDeleteList = append(tmpDeleteList, value.login)
			} else {
				alarmBeat.Remove(value.login)
			}
		}
		// modify alarm list
		s.modifyAlarmList(tmpDeleteList, alarmBeat)
	}

	log.Println("!! Policy inbound id list refresh complete")
}

func (s *PolicyServer) modifyAlarmList(tmpDeleteList []string, alarmBeat mapset.Set) {
	s.alarmBeatsMu.Lock()
	defer s.alarmBeatsMu.Unlock()

	if len(tmpDeleteList) > 0 {
		for _, delObj := range tmpDeleteList {
			delete(s.alarmBeats, delObj)
		}
	}

	if alarmBeat != nil {
		for value := range alarmBeat.Iter() {
			s.alarmBeats[value.(string)] = &AlarmBeat{
				updateAt: 0,
				login:    value.(string),
			}
		}
	}
}

func (s *PolicyServer) NewStats() *Stats {
	x := &Stats{
		ConnLimit: s.config.Limits.Limit,
	}
	x.heartbeat()
	return x
}

func (s *PolicyServer) Get(ip string) *Stats {
	s.statsMu.Lock()
	defer s.statsMu.Unlock()

	if x, ok := s.stats[ip]; !ok {
		x = s.NewStats()
		s.stats[ip] = x
		return x
	} else {
		x.heartbeat()
		return x
	}
}

func (s *PolicyServer) BanClient(ip string) {
	x := s.Get(ip)
	s.forceBan(x, ip)
}

func (s *PolicyServer) IsBanned(ip string) bool {
	x := s.Get(ip)
	return atomic.LoadInt32(&x.Banned) > 0
}

func (s *PolicyServer) ApplyLimitPolicy(ip string) bool {
	if !s.config.Limits.Enabled {
		return true
	}
	now := util.MakeTimestamp()
	if now-s.startedAt > s.grace {
		return s.Get(ip).decrLimit() > 0
	}
	return true
}

func (s *PolicyServer) ApplyLoginPolicy(addy, ip string) bool {
	if s.inboundId == nil {
		// If you do not get blacklist information, you cannot log in.
		return false
	} else if s.InIdBlackList(addy) {
		x := s.Get(ip)
		s.forceBan(x, ip)
		log.Printf("Invalid addr : %v", addy)
		return false
	}
	return true
}

func (s *PolicyServer) ApplyMalformedPolicy(ip string) bool {
	x := s.Get(ip)
	n := x.incrMalformed()
	if n >= s.config.Banning.MalformedLimit {
		s.forceBan(x, ip)
		return false
	}
	return true
}

func (s *PolicyServer) ApplyShareID(login string, validShare bool)  {
	apply := s.CheckShareID(login)

	if apply {
		s.storage.WriteAlarmBeat(login, s.beatIntv * 2)
	}
}

func (s *PolicyServer) CheckShareID(login string) bool {
	now := util.MakeTimestamp() / 1000

	s.alarmBeatsMu.Lock()
	defer s.alarmBeatsMu.Unlock()

	beat, exist := s.alarmBeats[login]
	if exist {
		if beat.updateAt < now {
			beat.updateAt = now + s.beatIntv.Milliseconds()/1000
			return true
		}
	}
	return false
}

// InitAlarmBeat When the proxy server is turned on for the first time, put all the children who need an alarm.
func (s *PolicyServer) InitAlarmBeat(beatIntv time.Duration) {
	alarmList, err := s.db.GetAlarmInfo()
	if err != nil {
		return
	}

	if alarmList == nil || len(alarmList) == 0 {
		return
	}

	var list []string
	for _, data := range alarmList {
		list = append(list, data.Id)
	}

	s.storage.InitAlarmBeat(list, beatIntv)
}

func (s *PolicyServer) ApplySharePolicy(ip string, validShare bool) bool {
	x := s.Get(ip)
	x.Lock()

	if validShare {
		x.ValidShares++
		if s.config.Limits.Enabled {
			x.incrLimit(s.config.Limits.LimitJump)
		}
	} else {
		x.InvalidShares++
	}

	totalShares := x.ValidShares + x.InvalidShares
	if totalShares < s.config.Banning.CheckThreshold {
		x.Unlock()
		return true
	}
	validShares := float32(x.ValidShares)
	invalidShares := float32(x.InvalidShares)
	x.resetShares()
	x.Unlock()

	ratio := invalidShares / validShares

	if ratio >= s.config.Banning.InvalidPercent/100.0 {
		s.forceBan(x, ip)
		return false
	}
	return true
}

func (x *Stats) resetShares() {
	x.ValidShares = 0
	x.InvalidShares = 0
}

func (s *PolicyServer) forceBan(x *Stats, ip string) {
	if !s.config.Banning.Enabled || s.InForceBanWhiteList(ip) {
		return
	}
	atomic.StoreInt64(&x.BannedAt, util.MakeTimestamp())

	if atomic.CompareAndSwapInt32(&x.Banned, 0, 1) {
		if len(s.config.Banning.IPSet) > 0 {
			s.banChannel <- ip
		} else {
			log.Println("Banned peer", ip)
		}
	}
}

func (x *Stats) incrLimit(n int32) {
	atomic.AddInt32(&x.ConnLimit, n)
}

func (x *Stats) incrMalformed() int32 {
	return atomic.AddInt32(&x.Malformed, 1)
}

func (x *Stats) decrLimit() int32 {
	return atomic.AddInt32(&x.ConnLimit, -1)
}

func (s *PolicyServer) CheckInboundIP(ip string) bool {
	if s.allowAllIp == true {
		// black list
		if s.inboundIp.Contains(ip) {
			return true
		}
		return false
	} else {
		// white list
		if s.inboundIp.Contains(ip) {
			return false
		}
		return true
	}
}

func (s *PolicyServer) InIdBlackList(addy string) bool {
	if s.allowAllId == true {
		// black list
		if s.inboundId.Contains(addy) {
			return true
		}
		return false
	} else {
		// white list
		if s.inboundId.Contains(addy) {
			return false
		}
		return true
	}
	//s.RLock()
	//defer s.RUnlock()
	//return util.StringInSlice(addy, s.blacklist)
}

func (s *PolicyServer) InForceBanWhiteList(ip string) bool {
	return s.whitelist.Contains(ip)
}

func (s *PolicyServer) doBan(ip string) {
	set, timeout := s.config.Banning.IPSet, s.config.Banning.Timeout
	cmd := fmt.Sprintf("sudo ipset add %s %s timeout %v -!", set, ip, timeout)
	args := strings.Fields(cmd)
	head := args[0]
	args = args[1:]

	log.Printf("Banned %v with timeout %v on ipset %s", ip, timeout, set)

	_, err := exec.Command(head, args...).Output()
	if err != nil {
		log.Printf("CMD Error: %s", err)
	}
}

func (x *Stats) heartbeat() {
	now := util.MakeTimestamp()
	atomic.StoreInt64(&x.LastBeat, now)
}