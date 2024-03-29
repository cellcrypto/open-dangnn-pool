package api

import (
	"encoding/json"
	"fmt"
	"github.com/cellcrypto/open-dangnn-pool/api/alarm"
	"github.com/cellcrypto/open-dangnn-pool/hook"
	"github.com/cellcrypto/open-dangnn-pool/util/plogger"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cellcrypto/open-dangnn-pool/storage/mysql"
	"github.com/cellcrypto/open-dangnn-pool/storage/redis"
	"github.com/cellcrypto/open-dangnn-pool/util"
	"github.com/dgrijalva/jwt-go"
	"github.com/gorilla/mux"
	"github.com/rs/cors"
)

type ApiConfig struct {
	Enabled                 bool   `json:"enabled"`
	Listen                  string `json:"listen"`
	PoolChartsNum           int64  `json:"poolChartsNum"`
	MinerChartsNum          int64  `json:"minerChartsNum"`
	PoolChartInterval       string `json:"poolChartInterval"`
	MinerChartCheckInterval string `json:"minerChartCheckInterval"`
	MinerChartInterval      string `json:"minerChartInterval"`
	DeleteCheckInterval		string `json:"deleteCheckInterval"`
	DeleteMaxRecord			int64  `json:"deleteMaxRecord"`
	DeleteKeepRecord		int64  `json:"deleteKeepRecord"`
	MinerPoolTimeout        string `json:"minerPoolTimeout"`
	StatsCollectInterval    string `json:"statsCollectInterval"`
	HashrateWindow          string `json:"hashrateWindow"`
	HashrateLargeWindow     string `json:"hashrateLargeWindow"`
	LuckWindow              []int  `json:"luckWindow"`
	Payments                int64  `json:"payments"`
	Blocks                  int64  `json:"blocks"`
	PurgeOnly               bool   `json:"purgeOnly"`
	PurgeInterval           string `json:"purgeInterval"`
	AllowedOrigins 			[]string `json:"AllowedOrigins"`
	Coin                    string
	Name                    string
	Depth                   int64
	Alarm					*alarm.Config	`json:"alarm"`
	// In Shannon
	Threshold      int64  `json:"threshold"`
	AccessSecret   string `json:"AccessSecret"`
}

type ApiServer struct {
	config              *ApiConfig
	backend             *redis.RedisClient
	hashrateWindow      time.Duration
	hashrateLargeWindow time.Duration
	stats               atomic.Value
	miners              map[string]*Entry
	apiMiners           map[string]*Entry
	db                  *mysql.Database
	minersMu            sync.RWMutex
	apiMinersMu         sync.RWMutex
	statsIntv           time.Duration
	minerPoolTimeout    time.Duration
	minerPoolChartIntv  int64
	allowedOrigins      []string

	alarm     *alarm.AlramServer

	//poolChartIntv       time.Duration
	//minerChartIntv      time.Duration
}

func (s *ApiServer) RedisMessage(payload string) {
	splitData := strings.Split(payload,":")
	if len(splitData) != 3 {
		return
	}
	opcode := splitData[0]
	from := splitData[1]
	msg := splitData[2]
	switch opcode {
	case redis.OpcodeLoadID:
		//if s.alarm != nil {
		//	s.alarm.MakeAlarmList()	// can process it right away.
		//}
	case redis.OpcodeLoadIP:
	case redis.OpcodeWhiteList:
	case redis.OpcodeMinerSub:
	default:
		log.Printf("not defined opcode: %v", opcode)
	}

	fmt.Printf("(opcode:%v from:%s)RedisMessage: %s\n", opcode, from, msg)
}

type Entry struct {
	stats     map[string]interface{}
	updatedAt int64
}

const (
	basicTokenExpiration = int64(15)
	unLimitTokenExpiration = int64(26280000)
)

func NewApiServer(cfg *ApiConfig, coin string, name string, backend *redis.RedisClient, db *mysql.Database) *ApiServer {
	hashrateWindow := util.MustParseDuration(cfg.HashrateWindow)
	hashrateLargeWindow := util.MustParseDuration(cfg.HashrateLargeWindow)
	return &ApiServer{
		config:              cfg,
		backend:             backend,
		hashrateWindow:      hashrateWindow,
		hashrateLargeWindow: hashrateLargeWindow,
		miners:              make(map[string]*Entry),
		apiMiners:           make(map[string]*Entry),
		db:					db,
	}
}

func (s *ApiServer) Start() {
	if s.config.PurgeOnly {
		log.Printf("Starting API in purge-only mode")
	} else {
		log.Printf("Starting API on %v", s.config.Listen)
	}

	quit := make(chan struct{})
	hooks := make(chan struct{})

	plogger.InsertLog("START API SERVER", plogger.LogTypeSystem, plogger.LogErrorNothing, 0, 0, "", "")
	hook.RegistryHook("server.go", func(name string) {
		plogger.InsertLog("SHUTDOWN API SERVER", plogger.LogTypeSystem, plogger.LogErrorNothing, 0, 0, "", "")
		close(quit)
		<- hooks
	})

	s.statsIntv = util.MustParseDuration(s.config.StatsCollectInterval)
	statsTimer := time.NewTimer(s.statsIntv)
	log.Printf("Set stats collect interval to %v", s.statsIntv)

	purgeIntv := util.MustParseDuration(s.config.PurgeInterval)
	purgeTimer := time.NewTimer(purgeIntv)
	log.Printf("Set purge interval to %v", purgeIntv)

	poolChartIntv := util.MustParseDuration(s.config.PoolChartInterval)
	poolChartTimer := time.NewTimer(poolChartIntv)
	s.minerPoolChartIntv = poolChartIntv.Milliseconds() / 1000
	log.Printf("Set pool chart interval to %v", poolChartIntv)

	minerChartCheckIntv := util.MustParseDuration(s.config.MinerChartCheckInterval)
	minerChartTimer := time.NewTimer(minerChartCheckIntv)

	minerChartIntv := util.MustParseDuration(s.config.MinerChartInterval)
	minerChartIntvSec := int64(minerChartIntv.Minutes() * 60)
	log.Printf("Set miner chart interval to %v %v", minerChartCheckIntv, minerChartIntvSec)

	s.minerPoolTimeout = util.MustParseDuration(s.config.MinerPoolTimeout)

	var (
		deleteCheckIntv time.Duration
		deleteTimer *time.Timer
	)
	if s.config.DeleteCheckInterval != "" {
		deleteCheckIntv = util.MustParseDuration(s.config.DeleteCheckInterval)
		deleteTimer = time.NewTimer(deleteCheckIntv)
	}

	sort.Ints(s.config.LuckWindow)

	s.backend.InitPubSub("api",s)

	s.config.Alarm.Coin = s.config.Coin
	if s.config.Alarm.Enabled == true {
		s.alarm = alarm.Start(s.config.Alarm,s.backend,s.db)
	}

	if s.config.PurgeOnly {
		s.purgeStale()
	} else {
		s.purgeStale()
		s.collectStats()
		if deleteCheckIntv != 0 && deleteTimer != nil {
			s.deleteDB()
		}
	}

	go func() {
		for {
			select {
			case <-statsTimer.C:
				if !s.config.PurgeOnly {
					s.collectStats()
				}
				statsTimer.Reset(s.statsIntv)
			case <-purgeTimer.C:
				s.purgeStale()
				purgeTimer.Reset(purgeIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-quit:
				hooks <- struct{}{}
				return
			case <-poolChartTimer.C:
				s.collectPoolCharts()

				poolChartTimer.Reset(poolChartIntv)
			case <-minerChartTimer.C:
				miners, err := s.db.GetAllMinerAccount(s.minerPoolTimeout, minerChartIntvSec)
				if err != nil {
					log.Println("Get all miners account error: ", err)
				}

				ts := util.MakeTimestamp() / 1000

				for _, miner := range miners {

					if ok := s.db.CheckTimeMinerCharts(miner, ts, minerChartIntvSec); ok {
						reportedHash, _ := s.backend.GetAllReportedtHashrate(miner.Addr)

						online, _, totalHashrate , currentHashrate := s.backend.CollectWorkersStatsEx(s.hashrateWindow, s.hashrateLargeWindow, miner.Addr)
						// stats, _ := s.backend.CollectWorkersAllStats(s.hashrateWindow, s.hashrateLargeWindow, miner.Addr)
						s.collectMinerCharts(miner.Addr, currentHashrate, totalHashrate, online, int64(miner.Share), reportedHash)
					}
				}
				minerChartTimer.Reset(minerChartCheckIntv)
			}
		}
	}()

	if deleteCheckIntv != 0 && deleteTimer != nil {
		go func() {
			for {
				select {
				case <-deleteTimer.C:
					s.deleteDB()
					deleteTimer.Reset(deleteCheckIntv)
				}
			}
		}()
	}

	if !s.config.PurgeOnly {
		s.listen()
	}
}

func (s *ApiServer) VerifyToken(accessToken string) (*jwt.Token, error) {
	token, err := jwt.Parse(accessToken, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return []byte(s.config.AccessSecret), nil
	})
	if err != nil {
		return nil, err
	}
	return token, nil
}

func (s *ApiServer) TokenValid(accessToken string) (*jwt.Token,error) {
	token, err := s.VerifyToken(accessToken)
	if err != nil {
		return nil, err
	}
	if _, ok := token.Claims.(jwt.Claims); !ok && !token.Valid {
		return nil, err
	}
	return token, nil
}

func (s *ApiServer) authenticationMiddleware (next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//token := r.Header.Get("access-token")

		requestURL := strings.Split(r.RequestURI,"/")
		if len(requestURL) > 1 {
			switch requestURL[1] {
			case "signin","token","health":
				fmt.Println(requestURL[1])
				next.ServeHTTP(w, r)
				return
			}
			passed, errStr := s.CheckJwtToken(r, requestURL[1])
			if !passed {
				fmt.Println("CheckJwtToken Error:",errStr)
				s.ServerError(w, r, errStr)
				return
			}
		} else {
			s.ServerError(w, r, "nothing page URI")
			return
		}

		origin := r.Header.Get("Origin")
		if origin == "" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *ApiServer) CheckJwtToken(r *http.Request, requestURI string) (bool,string) {
	idToken := r.Header.Get("API_KEY")
	if idToken == "" {
		cookie, _ := r.Cookie("access-token")
		if cookie == nil || len(cookie.Value) <= 0 {
			return false, "unauthorized: non cookie"
		}
		idToken = cookie.Value
	}

	token, err := s.TokenValid(idToken)
	if err != nil {
		return false, "unauthorized: " + err.Error()
	}

	access, ok := token.Claims.(jwt.MapClaims)["access"]
	if !ok {
		return false, "unauthorized: nothing access"
	}

	var login string

	if devId, ok := token.Claims.(jwt.MapClaims)["DevId"]; ok {
		if access != "user" {
			return false, "unauthorized: non argument"
		}

		r.Header.Set("DevId", devId.(string))

		login = strings.ToLower(mux.Vars(r)["login"])
		if devId.(string) != "all" {
			lowerDevId:= strings.ToLower(devId.(string))	// case-insensitive
			if login != lowerDevId {
				return false, "unauthorized: diff argument"
			}
		} else {
			login = devId.(string)
		}
	} else {
		if access != "all" {
			return false, "unauthorized: non argument"
		}

		login, _ = token.Claims.(jwt.MapClaims)["user_id"].(string)
	}
	r.Header.Set("login", login)

	accessFlag := false
	if access, ok := token.Claims.(jwt.MapClaims)["access"]; ok {
		accesURI := strings.Split( access.(string), ",")
		for _, uri := range accesURI {
			if uri == requestURI || uri == "all" {
				accessFlag = true
				break
			}
		}
		if accessFlag == false {
			return false, "unauthorized: Invalid Access"
		}
	} else {
		return false, "unauthorized: Invalid Claims"
	}
	accessSign, err := s.backend.GetToken(util.Join(s.config.Coin, login))
	if err != nil {
		return false, "unauthorized: Invalid login"
	}
	if strings.Compare(accessSign, token.Signature) != 0 {
		return false, "unauthorized: Invalid sign"
	}
	return true, ""
}

func (s *ApiServer) ServerError(w http.ResponseWriter, r *http.Request, errMsg string) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	origin := r.Header.Get("Origin")
	if origin == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
	} else {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
	//w.Header().Set("Access-Control-Allow-Header", "access-token")
	w.Header().Set("Cache-Control", "no-cache")

	w.WriteHeader(http.StatusUnauthorized)
	//w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{
		"msg": errMsg,
	})
	return
}

func (s *ApiServer) listen() {
	r := mux.NewRouter()
	//apiRouter := r.GetRoute("api")
	//apiRouter.
	r.HandleFunc("/api/stats", s.StatsIndex)
	r.HandleFunc("/api/miners", s.MinersIndex)
	r.HandleFunc("/api/blocks", s.BlocksIndex)
	r.HandleFunc("/api/payments", s.PaymentsIndex)
	r.HandleFunc("/api/accounts/{login:0x[0-9a-fA-F]{40}}", s.AccountIndex)
	r.HandleFunc("/user/accounts/{login:0x[0-9a-fA-F]{40}}", s.AccountExIndex)
	r.HandleFunc("/user/payout/{login:0x[0-9a-fA-F]{40}}/{value:[0-9]+}", s.PayoutLimitIndex)
	r.HandleFunc("/signin", s.SignInIndex)
	r.HandleFunc("/signup", s.SignupIndex)
	r.HandleFunc("/api/reglist", s.GetAccountListIndex)
	r.HandleFunc("/token", s.GetTokenIndex).Methods("POST")
	r.HandleFunc("/api/inbounds", s.InboundListIndex)
	r.HandleFunc("/api/saveinbound", s.SaveInboundIndex)
	r.HandleFunc("/api/delinbound", s.DelInboundIndex)
	r.HandleFunc("/api/idbounds", s.DevIdInboundListIndex)
	r.HandleFunc("/api/saveidbound", s.SaveDevIdInboundIndex)
	r.HandleFunc("/api/delidbound", s.DelIDboundIndex)
	r.HandleFunc("/api/devsearch", s.GetLikeDevSubListIndex)
	r.HandleFunc("/api/addsubid", s.SaveSubIdIndex)
	r.HandleFunc("/api/delsubid", s.DelSubIdIndex)

	r.HandleFunc("/api/addaccount", s.AddAccountIndex)
	r.HandleFunc("/api/changeacc", s.ChangeAccessIndex)
	r.HandleFunc("/api/changepass", s.ChangePasswordIndex)
	r.HandleFunc("/api/delaccount", s.DelAccounIndex)

	r.HandleFunc("/api/changealarm", s.ChangeAlarmIndex)
	r.HandleFunc("/api/changedesc", s.ChangeDescIndex)

	r.HandleFunc("/api/applyid", s.ApplyInboundIDIndex)
	r.HandleFunc("/api/applyip", s.ApplyInboundIPIndex)
	r.HandleFunc("/api/applysub", s.ApplyMinerSbuIndex)

	r.HandleFunc("/health", s.Health)

	var c *cors.Cors
	s.allowedOrigins = make([]string, len(s.config.AllowedOrigins))
	if len(s.config.AllowedOrigins) > 0 {
		for i, v := range s.config.AllowedOrigins {
			s.allowedOrigins[i] = v
		}

		c = cors.New(cors.Options{
			AllowedOrigins: s.allowedOrigins,
			AllowCredentials: true,
			AllowedHeaders: []string{"access_token"},
			AllowedMethods: []string{"get","post","options"},
		})
	}

	//r.HandleFunc("/api/accounts/{login:0x[0-9a-fA-F]{40}}/{personal:0x[0-9a-fA-F]{40}}", s.AccountIndexEx)
	r.NotFoundHandler = http.HandlerFunc(notFound)
	r.Use(s.authenticationMiddleware )

	var err error
	if c != nil {
		err = http.ListenAndServe(s.config.Listen, c.Handler(r))
	} else {
		err = http.ListenAndServe(s.config.Listen, r)
	}

	if err != nil {
		log.Fatalf("Failed to start API: %v", err)
	}
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusNotFound)
}

func (s *ApiServer) purgeStale() {
	start := time.Now()
	total, err := s.backend.FlushStaleStats(s.hashrateWindow, s.hashrateLargeWindow)
	if err != nil {
		log.Println("Failed to purge stale data from backend:", err)
	} else {
		log.Printf("Purged stale stats from backend, %v shares affected, elapsed time %v", total, time.Since(start))
	}
}

func (s *ApiServer) deleteDB() {
	start := time.Now()

	// Get the min max.
	minSeq, maxSeq := s.db.GetBlockBalanceMinMax()
	var (
		tmpMax int64
		count int64
		total int64
	)

	deleteKeepRecord := s.config.DeleteKeepRecord
	deleteMaxRecord := s.config.DeleteMaxRecord
	if deleteKeepRecord == 0 || deleteMaxRecord == 0 {
		return
	}

	for maxSeq - minSeq > deleteKeepRecord {
		if maxSeq - deleteKeepRecord > minSeq + deleteMaxRecord {
			tmpMax = minSeq + deleteMaxRecord -1
		} else {
			tmpMax = maxSeq - deleteKeepRecord
		}

		queryStart := time.Now()
		result := s.db.DeleteBlockBalance(minSeq, tmpMax)

		log.Printf("(%v) Deletes data from %d to %d in the credits_balance table. Rows Affected: %v", time.Since(queryStart), minSeq, tmpMax, result)

		minSeq += deleteMaxRecord
		count++
		total += result
	}

	fmt.Printf("(%v) Amount of data deleted from DB: %v total delete record: %v\n", time.Since(start), count, total)
}

func (s *ApiServer) collectStats() {
	start := time.Now()
	stats, err := s.backend.CollectStats(s.hashrateWindow, s.config.Blocks, s.config.Payments)
	if err != nil {
		log.Printf("Failed to fetch stats from backend: %v", err)
		return
	}
	if len(s.config.LuckWindow) > 0 {
		stats["luck"], err = s.backend.CollectLuckStats(s.config.LuckWindow)
		if err != nil {
			log.Printf("Failed to fetch luck stats from backend: %v", err)
			return
		}
	}

	currentHeight, _ := s.backend.GetNodeHeight(s.config.Name)
	stats["poolCharts"], err = s.backend.GetPoolCharts(s.config.PoolChartsNum)
	sqlCount := int64(0)
	depth := s.config.Depth * 2
	minHeight := currentHeight-depth-100
	stats["poolBalanceOnce"], sqlCount,_ = s.db.GetPoolBalanceByOnce(currentHeight-depth, minHeight, s.config.Coin)
	s.stats.Store(stats)

	log.Printf("Stats collection finished %s poolEarnPerDay(%v,%v,%v,%v)", time.Since(start), stats["poolBalanceOnce"], sqlCount, minHeight, currentHeight-depth)
}

func (s *ApiServer) StatsIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	nodes, err := s.backend.GetNodeStates()
	if err != nil {
		log.Printf("Failed to get nodes stats from backend: %v", err)
	}
	reply["nodes"] = nodes

	stats := s.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp()
		reply["stats"] = stats["stats"]
		reply["poolCharts"] = stats["poolCharts"]
		reply["hashrate"] = stats["hashrate"]
		reply["minersTotal"] = stats["minersTotal"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
	}

	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) MinersIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := s.getStats()
	if stats != nil {
		reply["now"] = util.MakeTimestamp()
		reply["miners"] = stats["miners"]
		reply["hashrate"] = stats["hashrate"]
		reply["minersTotal"] = stats["minersTotal"]
	}

	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) BlocksIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := s.getStats()
	if stats != nil {
		reply["matured"] = stats["matured"]
		reply["maturedTotal"] = stats["maturedTotal"]
		reply["immature"] = stats["immature"]
		reply["immatureTotal"] = stats["immatureTotal"]
		reply["candidates"] = stats["candidates"]
		reply["candidatesTotal"] = stats["candidatesTotal"]
		reply["luck"] = stats["luck"]
	}

	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) PaymentsIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)

	reply := make(map[string]interface{})
	stats := s.getStats()
	if stats != nil {
		reply["payments"] = stats["payments"]
		reply["paymentsTotal"] = stats["paymentsTotal"]
	}

	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")
	//http.SetCookie(w, &http.Cookie{
	//	Name: "name of cookie",
	//	Value: "value of cookie",
	//	Path: "/",
	//})

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(map[string]string {
		"status":"ok",
	})
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) AccountIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	login := strings.ToLower(mux.Vars(r)["login"])
	s.minersMu.Lock()
	defer s.minersMu.Unlock()

	reply, ok := s.miners[login]
	now := util.MakeTimestamp()
	ts := now / 1000
	cacheIntv := int64(s.statsIntv / time.Millisecond)
	// Refresh stats if stale
	if !ok || reply.updatedAt < now-cacheIntv {
		exist, setPayout, err := s.db.IsMinerExists(login)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		if !exist {
			w.WriteHeader(http.StatusNotFound)
			return
		}

		stats, err := s.backend.GetMinerStats(login, s.config.Payments)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		reportedHash, _ := s.backend.GetReportedtHashrate(login)
		workers, err := s.backend.CollectWorkersAllStats(s.hashrateWindow, s.hashrateLargeWindow, login, reportedHash)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}

		for key, value := range workers {
			stats[key] = value
		}
		stats["pageSize"] = s.config.Payments
		stats["minPayout"] = s.config.Threshold
		stats["maxPayout"] = s.config.Threshold * 100
		stats["setPayout"] = setPayout
		stats["minerCharts"], err = s.db.GetMinerCharts(s.config.MinerChartsNum, s.minerPoolChartIntv, login, ts)
		//stats["minerCharts"], err = s.backend.GetMinerCharts(s.config.MinerChartsNum, login)
		//stats["paymentCharts"], err = s.backend.GetPaymentCharts(login)

		statsM := s.getStats()
		if stats != nil {
			stats["hashrateTotal"] = statsM["hashrate"]
			stats["minersTotal"] = statsM["minersTotal"]
			stats["poolBalanceOnce"] = statsM["poolBalanceOnce"]
		}

		reply = &Entry{stats: stats, updatedAt: now}
		s.miners[login] = reply
	}

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply.stats)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) AccountExIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	login := strings.ToLower(mux.Vars(r)["login"])

	nowtime := time.Now()
	now := util.MakeTimestamp()
	ts := now / 1000
	cacheIntv := int64(s.statsIntv / time.Millisecond)

	s.apiMinersMu.Lock()
	defer s.apiMinersMu.Unlock()
	reply, ok := s.apiMiners[login]

	// Refresh stats if stale
	if !ok || reply.updatedAt < now-cacheIntv {
		exist, setPayout, err := s.db.IsMinerExists(login)
		if err != nil {
			s.WirteResponseData(w, http.StatusInternalServerError, "Failed to fetch stats from backend: %v", err)
			return
		}
		if !exist {
			s.WirteResponseData(w, http.StatusNotFound, "non-existent minor:" + login)
			return
		}

		stats, err := s.backend.GetMinerStats(login, s.config.Payments)
		if err != nil {
			s.WirteResponseData(w, http.StatusInternalServerError, "Failed to no minor information: %v", err)
			return
		}
		reportedHash, _ := s.backend.GetReportedtHashrate(login)
		workers, err := s.backend.CollectWorkersStats(s.hashrateWindow, s.hashrateLargeWindow, login, reportedHash)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}

		for key, value := range workers {
			stats[key] = value
		}
		if setPayout == 0 {
			setPayout = s.config.Threshold
		}
		stats["pageSize"] = s.config.Payments
		stats["minPayout"] = s.config.Threshold
		stats["maxPayout"] = s.config.Threshold * 100
		stats["setPayout"] = setPayout
		stats["minerCharts"], err = s.db.GetMinerCharts(s.config.MinerChartsNum, s.minerPoolChartIntv, login, ts)
		//stats["minerCharts"], err = s.backend.GetMinerCharts(s.config.MinerChartsNum, login)
		//stats["paymentCharts"], err = s.backend.GetPaymentCharts(login)

		statsM := s.getStats()
		if stats != nil {
			stats["statsm"] = statsM["stats"]
			stats["hashrateTotal"] = statsM["hashrate"]
			stats["minersTotal"] = statsM["minersTotal"]
			stats["poolBalanceOnce"] = statsM["poolBalanceOnce"]
		}

		reply = &Entry{stats: stats, updatedAt: now}
		s.apiMiners[login] = reply
	}

	fmt.Printf("test time: %v\n", time.Since(nowtime))

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply.stats)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) PayoutLimitIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	login := strings.ToLower(mux.Vars(r)["login"])
	value := strings.ToLower(mux.Vars(r)["value"])

	// value check
	setPayout,err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		s.WirteResponseData(w, http.StatusBadRequest,"Failed to set payout value error:%v",err)
		return
	}
	minPayout := s.config.Threshold
	maxPayout := s.config.Threshold * 100
	if setPayout != 0 {	// Default if 0
		if setPayout < minPayout {
			s.WirteResponseData(w, http.StatusBadRequest, "Failed to UpdatePayoutLimit:payout out of range(min:%v)", minPayout)
			return
		}
		if setPayout > maxPayout {
			s.WirteResponseData(w, http.StatusBadRequest, "Failed to UpdatePayoutLimit:payout out of range(max:%v)", maxPayout)
			return
		}
	}

	if !s.db.UpdatePayoutLimit(login, value) {
		s.WirteResponseData(w, http.StatusInternalServerError, "Failed to UpdatePayoutLimit (%v)",login)
		return
	}

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) WirteResponseData(w http.ResponseWriter, status int, format string, v ...interface{}) {
	msg := fmt.Sprintf(format, v...)
	log.Printf(msg)

	reply := make(map[string]interface{})
	reply["msg"] = msg
	w.WriteHeader(status)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) SignInIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Headers", "*")
	w.Header().Set("Cache-Control", "no-cache")

	switch r.Method {
	case "GET":
		http.ServeFile(w, r, "#/login")
		return
	case "POST":
	default:
		fmt.Fprintf(w, "Sorry, only GET and POST methods are supported.")
		return
	}

	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	passDb, access, err := s.db.GetAccountPassword(user.Username)
	if err != nil {
		log.Printf("failed to DB Connected: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !util.CheckPasswordHash(passDb, user.Password) {
		log.Printf("failed to password is different: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string {
			"error": fmt.Sprintf("password is different: %v", err),
		})
		return
	}

	// permission check


	// Token Issuance
	token, _ := s.CreateUserToken(user.Username, access, basicTokenExpiration)

	tokenSplit := strings.Split(token,".")
	if len(tokenSplit) != 3 {
		return
	}
	// Register token as devid in Redis.
	s.backend.SetToken(util.Join(s.config.Coin, user.Username), tokenSplit[2],basicTokenExpiration)


	cookie := new(http.Cookie)
	cookie.Name = "access-token"
	cookie.Value = token
	cookie.HttpOnly = true
	cookie.Expires = time.Now().Add(time.Hour * 24)

	http.SetCookie(w, cookie)

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	reply["token"] = token
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) GetTokenIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	switch r.Method {
	case "GET":
		http.ServeFile(w, r, "#/login")
		return
	case "POST":
	default:
		fmt.Fprintf(w, "Sorry, only GET and POST methods are supported.")
		return
	}

	var userToken UserToken
	if err := json.NewDecoder(r.Body).Decode(&userToken); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var tokenExp = basicTokenExpiration
	if userToken.DevId != "all" {
		if !util.IsValidHexAddress(userToken.DevId) {
			log.Printf("failed to DevId: %v", userToken.DevId)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
	} else {
		tokenExp = unLimitTokenExpiration
	}


	passDb, access, err := s.db.GetAccountPassword(userToken.Username)
	if err != nil {
		log.Printf("failed to DB Connected: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !util.CheckPasswordHash(passDb, userToken.Password) {
		log.Printf("failed to password is different: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]string {
			"error": fmt.Sprintf("password is different: %v", err),
		})
		return
	}

	// Permission Check


	// Token Issuance
	token, _ := s.CreateToken(userToken.DevId, access, tokenExp)

	tokenSplit := strings.Split(token,".")
	if len(tokenSplit) != 3 {
		return
	}
	// Register token as devid in Redis.
	s.backend.SetToken(util.Join(s.config.Coin, userToken.DevId), tokenSplit[2],tokenExp)


	cookie := new(http.Cookie)
	cookie.Name = "access-token"
	cookie.Value = token
	cookie.HttpOnly = true
	cookie.Expires = time.Now().Add(time.Hour * 24)

	http.SetCookie(w, cookie)

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	reply["token"] = token
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

type User struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Access	string `json:"access"`
}

type UserToken struct {
	Username string `json:"username"`
	Password string `json:"password"`
	DevId    string `json:"devid"`
}

type DbIPInbound struct {
	Ip string `json:"ip"`
	Rule string `json:"rule"`
	Alarm string `json:"alarm"`
	Desc    string `json:"desc"`
}

type DevSubList struct {
	DevId 	string `json:"devid"`
	SubId 	string `json:"subid"`
	Amount  string `json:"amount"`
	AllowId bool `json:"allowid"`
}

func (s *ApiServer) InboundListIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")


	inboundList, err := s.db.GetIpInboundList()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed to GetIpInboundList()")
		return
	}

	reply := make(map[string]interface{})
	reply["inbounds"] = inboundList
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) SaveInboundIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var ipInbound DbIPInbound
	if err := json.NewDecoder(r.Body).Decode(&ipInbound); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// validation data
	if !util.StringInSlice(ipInbound.Rule,[]string{"allow", "deny"}) {
		log.Printf("failed to incorrect value: %v", ipInbound.Rule)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ipInbound.Ip = strings.ToLower(ipInbound.Ip)

	saveFlag := s.db.SaveIpInbound(ipInbound.Ip,ipInbound.Rule)

	reply := make(map[string]interface{})
	if saveFlag {
		reply["state"] = "true"
		reply["msg"] = "success"
	} else {
		reply["state"] = "false"
		reply["msg"] = "failed"
	}

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) DelInboundIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var ipInbound DbIPInbound
	if err := json.NewDecoder(r.Body).Decode(&ipInbound); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data



	saveFlag := s.db.DelIpInbound(ipInbound.Ip)

	reply := make(map[string]interface{})
	if saveFlag {
		reply["state"] = "true"
		reply["msg"] = "success"
	} else {
		reply["state"] = "false"
		reply["msg"] = "failed"
	}

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) DevIdInboundListIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")


	idboundList, err := s.db.GetIdInboundList()
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed to GetIdInboundList()")
		return
	}

	reply := make(map[string]interface{})
	reply["idbounds"] = idboundList
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) SaveDevIdInboundIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var ipInbound DbIPInbound
	if err := json.NewDecoder(r.Body).Decode(&ipInbound); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data
	if util.StringInSlice(ipInbound.Rule,[]string{"allow", "deny"}) == false {
		return
	}
	if util.StringInSlice(ipInbound.Alarm,[]string{"none", "slack"}) == false {
		return
	}
	var ok bool
	if ipInbound.Ip, ok = util.CheckValidHexAddress(ipInbound.Ip); !ok {
		log.Printf("failed to DevId: %v", ipInbound.Ip)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ipInbound.Ip = strings.ToLower(ipInbound.Ip)

	saveFlag := s.db.SaveIdInbound(ipInbound.Ip, ipInbound.Rule, ipInbound.Alarm, ipInbound.Desc)

	reply := make(map[string]interface{})
	if saveFlag {
		reply["state"] = "true"
		reply["msg"] = "success"
	} else {
		reply["state"] = "false"
		reply["msg"] = "failed"
	}

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) DelIDboundIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var idInbound DbIPInbound
	if err := json.NewDecoder(r.Body).Decode(&idInbound); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data



	saveFlag := s.db.DelIdInbound(idInbound.Ip)

	reply := make(map[string]interface{})
	if saveFlag {
		reply["state"] = "true"
		reply["msg"] = "success"
	} else {
		reply["state"] = "false"
		reply["msg"] = "failed"
	}

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) GetLikeDevSubListIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")


	var devSubList DevSubList
	if err := json.NewDecoder(r.Body).Decode(&devSubList); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data
	//if !util.IsValidHexAddress(devSubList.DevId) {
	//	log.Printf("failed to DevId: %v", devSubList.DevId)
	//	w.WriteHeader(http.StatusBadRequest)
	//	return
	//}

	devList, err := s.db.GetLikeMinerSubList(devSubList.DevId)
	if err != nil {
		log.Printf("Failed to GetLikeMinerSubList()")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	reply := make(map[string]interface{})
	reply["devlist"] = devList
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) SaveSubIdIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var devSubList DevSubList
	if err := json.NewDecoder(r.Body).Decode(&devSubList); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ok := false
	// validation data
	if devSubList.DevId, ok = util.CheckValidHexAddress(devSubList.DevId); !ok {
		log.Printf("failed to DevId: %v", devSubList.DevId)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if devSubList.SubId, ok = util.CheckValidHexAddress(devSubList.SubId); !ok {
		log.Printf("failed to SubId: %v", devSubList.SubId)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	lowerDevId := strings.ToLower(devSubList.DevId)
	lowerSubId := strings.ToLower(devSubList.SubId)


	// Get the quantity and set the max value
	devList, err := s.db.GetMinerSubInfo(lowerDevId)
	if err != nil {
		log.Printf("Failed to GetMinerSubInfo()")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	var (
		devTotalCount = int64(0)
		addCount = int64(0)
	)

	for _, dev := range devList {
		count := int64(1)
		if dev.Amount > 1 {
			count = dev.Amount
		}

		devTotalCount += count
		if lowerDevId == dev.DevAddr && lowerSubId == dev.SubAddr {
			addCount += count
		}
	}
	amount, _ := strconv.ParseInt(devSubList.Amount,10,64)
	addCount += amount
	devTotalCount += amount
	if devTotalCount > 18 || devTotalCount < 1{
		log.Printf("Exceeding max dev count: %v",devTotalCount)
		s.ErrorWrite(w, "Exceeding max dev count")
		return
	}

	saveFlag := s.db.SaveSubIdIndex(lowerDevId, lowerSubId, addCount)
	if saveFlag && devSubList.AllowId {
		// Allow ID
		if !s.db.IsIdInboundId(lowerDevId) {
			s.db.SaveIdInbound(lowerDevId,"allow", "none", "")
		}
	}

	reply := make(map[string]interface{})
	if saveFlag {
		reply["state"] = "true"
		reply["msg"] = "success"
	} else {
		reply["state"] = "false"
		reply["msg"] = "failed"
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) DelSubIdIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var devSubList DevSubList
	if err := json.NewDecoder(r.Body).Decode(&devSubList); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	ok := false
	// validation data
	if devSubList.DevId, ok = util.CheckValidHexAddress(devSubList.DevId); !ok {
		log.Printf("failed to DevId: %v", devSubList.DevId)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if devSubList.SubId, ok = util.CheckValidHexAddress(devSubList.SubId); !ok {
		log.Printf("failed to SubId: %v", devSubList.SubId)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	saveFlag := s.db.DelSubIdIndex(devSubList.DevId,devSubList.SubId)

	reply := make(map[string]interface{})
	if saveFlag {
		reply["state"] = "true"
		reply["msg"] = "success"
	} else {
		reply["state"] = "false"
		reply["msg"] = "failed"
	}

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) AddAccountIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var userToken UserToken
	if err := json.NewDecoder(r.Body).Decode(&userToken); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data
	if !util.IsValidUsername(userToken.Username) {
		log.Printf("failed to Username: %v", userToken.Username)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	hashedPassword, err := util.HashPassword(userToken.Password)
	if err != nil {
		log.Printf("failed to GenerateFromPassword: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !s.db.CreateAccount(userToken.Username, hashedPassword, "none") {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed to CreateAccount()")
		return
	}

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) ChangeAccessIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data
	if !util.IsValidUsername(user.Username) {
		log.Printf("failed to Username: %v", user.Username)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if !util.StringInSlice(user.Access,[]string{"none", "all", "user"}) {
		log.Printf("failed to incorrect value: %v", user.Access)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !s.db.ChangeAccountAccess(user.Username, user.Access) {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed to ChangeAccountAccess()")
		return
	}

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) ChangePasswordIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data
	if !util.IsValidUsername(user.Username) {
		log.Printf("failed to Username: %v", user.Username)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	hashedPassword, err := util.HashPassword(user.Password)
	if err != nil {
		log.Printf("failed to GenerateFromPassword: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if !s.db.ChangeAccountPassword(user.Username, hashedPassword) {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed to ChangePasswordIndex()")
		return
	}

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) DelAccounIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var user User
	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data
	if !util.IsValidUsername(user.Username) {
		log.Printf("failed to Username: %v", user.Username)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !s.db.DeleteAccount(user.Username) {
		w.WriteHeader(http.StatusInternalServerError)
		log.Printf("Failed to DelAccounIndex()")
		return
	}

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) ErrorWrite(w http.ResponseWriter, errorStr string) {
	reply := make(map[string]interface{})
	reply["state"] = "false"
	reply["msg"] = errorStr
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) SignupIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	log.Println("Sign up")
	var user User

	if err := json.NewDecoder(r.Body).Decode(&user); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// validation data
	if !util.IsValidUsername(user.Username) {
		log.Printf("failed to Username: %v", user.Username)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	hashedPassword, err := util.HashPassword(user.Password)
	if err != nil {
		log.Printf("failed to GenerateFromPassword: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}


	if !s.db.CreateAccount(user.Username, hashedPassword, "none") {
		log.Printf("Failed to CreateAccount()")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) GetAccountListIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	log.Println("GetAccountListIndex")

	userInfo, err:= s.db.GetAccountList()
	if err != nil {
		log.Printf("Failed to GetAccountList()")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	idToken := r.Header.Get("login")

	reply := make(map[string]interface{})
	reply["msg"] = "success"
	reply["username"] = idToken
	reply["registers"] = userInfo
	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(reply)
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) getStats() map[string]interface{} {
	stats := s.stats.Load()
	if stats != nil {
		return stats.(map[string]interface{})
	}
	return nil
}

func (s *ApiServer) collectPoolCharts() {
	ts := util.MakeTimestamp() / 1000
	now := time.Now()
	year, month, day := now.Date()
	hour, min, _ := now.Clock()
	t2 := fmt.Sprintf("%d-%02d-%02d %02d_%02d", year, month, day, hour, min)
	stats := s.getStats()
	hash := fmt.Sprint(stats["hashrate"])
	log.Println("Pool Hash is ", ts, t2, hash)
	err := s.backend.WritePoolCharts(ts, t2, hash)
	if err != nil {
		log.Printf("Failed to fetch pool charts from backend: %v", err)
		return
	}
}

func (s *ApiServer) collectMinerCharts(login string, hash int64, largeHash int64, workerOnline int64, share int64, report int64) {
	ts := util.MakeTimestamp() / 1000
	now := time.Now()
	year, month, day := now.Date()
	hour, min, _ := now.Clock()
	t2 := fmt.Sprintf("%d-%02d-%02d %02d_%02d", year, month, day, hour, min)

	//log.Println("Miner "+login+" Hash is", ts, t2, hash, largeHash, share, report)
	err := s.db.WriteMinerCharts(ts, t2, login, hash, largeHash, workerOnline, share, report)
	// err := s.backend.WriteMinerCharts(ts, t2, login, hash, largeHash, workerOnline, share, report)
	if err != nil {
		log.Printf("Failed to fetch miner %v charts from backend: %v", login, err)
	}
}

func (s *ApiServer) CreateToken(devId, access string, expirationMin int64) (string, error) {
	var err error
	//Creating Access Token
	atClaims := jwt.MapClaims{}
	atClaims["authorized"] = true
	atClaims["DevId"] = devId
	atClaims["access"] = access
	atClaims["exp"] = time.Now().Add(time.Minute * time.Duration(expirationMin)).Unix()
	at := jwt.NewWithClaims(jwt.SigningMethodHS256, atClaims)
	token, err := at.SignedString([]byte(s.config.AccessSecret))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *ApiServer) CreateUserToken(id, access string, expirationMin int64) (string, error) {
	var err error
	//Creating Access Token
	atClaims := jwt.MapClaims{}
	atClaims["authorized"] = true
	atClaims["user_id"] = id
	atClaims["access"] = access
	atClaims["exp"] = time.Now().Add(time.Minute * time.Duration(expirationMin)).Unix()
	at := jwt.NewWithClaims(jwt.SigningMethodHS256, atClaims)
	token, err := at.SignedString([]byte(s.config.AccessSecret))
	if err != nil {
		return "", err
	}
	return token, nil
}

func (s *ApiServer) ApplyInboundIDIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")


	_, err := s.backend.Publish(redis.ChannelProxy,redis.OpcodeLoadID, "", redis.ChannelApi)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		err = json.NewEncoder(w).Encode(map[string]string {
			"status":"fail",
			"msg":"Failed to send to proxy server",
		})
		return
	}

	if s.alarm != nil {
		s.alarm.MakeAlarmList()	// can process it right away.
	}
	// Not sent to Redis and processed.
	//_, err = s.backend.Publish(redis.ChannelApi,redis.OpcodeLoadID, "", redis.ChannelApi)
	//if err != nil {
	//	w.WriteHeader(http.StatusOK)
	//	err = json.NewEncoder(w).Encode(map[string]string {
	//		"status":"fail",
	//		"msg":"Failed to send to api server",
	//	})
	//	return
	//}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(map[string]string {
		"status":"ok",
	})
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) ApplyInboundIPIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")


	_, err := s.backend.Publish(redis.ChannelProxy,redis.OpcodeLoadIP, "", redis.ChannelApi)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		err = json.NewEncoder(w).Encode(map[string]string {
			"status":"fail",
		})
		return
	}

	_, err = s.backend.Publish(redis.ChannelProxy,redis.OpcodeWhiteList, "", redis.ChannelApi)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		err = json.NewEncoder(w).Encode(map[string]string {
			"status":"fail",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(map[string]string {
		"status":"ok",
	})
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) ApplyMinerSbuIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")


	_, err := s.backend.Publish(redis.ChannelProxy,redis.OpcodeMinerSub, "", redis.ChannelApi)
	if err != nil {
		w.WriteHeader(http.StatusOK)
		err = json.NewEncoder(w).Encode(map[string]string {
			"status":"fail",
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	err = json.NewEncoder(w).Encode(map[string]string {
		"status":"ok",
	})
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}

func (s *ApiServer) ChangeAlarmIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var idInbound DbIPInbound
	if err := json.NewDecoder(r.Body).Decode(&idInbound); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var ok bool
	if idInbound.Ip, ok = util.CheckValidHexAddress(idInbound.Ip); !ok {
		log.Printf("failed to DevId: %v", idInbound.Ip)
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if util.StringInSlice(idInbound.Alarm,[]string{"none", "slack"}) == false {
		return
	}

	s.db.UpdateIdInboundAlarm(idInbound.Ip, idInbound.Alarm)

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(map[string]string {
		"status":"ok",
	})
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}


func (s *ApiServer) ChangeDescIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	//w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "no-cache")

	var idInbound DbIPInbound
	if err := json.NewDecoder(r.Body).Decode(&idInbound); err != nil {
		log.Printf("failed to Decode: %v", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	var ok bool
	if idInbound.Ip, ok = util.CheckValidHexAddress(idInbound.Ip); !ok {
		log.Printf("failed to DevId: %v", idInbound.Ip)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	s.db.UpdateIdInboundDesc(idInbound.Ip, idInbound.Desc)

	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(map[string]string {
		"status":"ok",
	})
	if err != nil {
		log.Println("Error serializing API response: ", err)
	}
}