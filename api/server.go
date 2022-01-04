package api

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"github.com/cellcrypto/open-dangnn-pool/storage/mysql"
	"github.com/cellcrypto/open-dangnn-pool/storage/redis"
	"github.com/cellcrypto/open-dangnn-pool/util"
)

type ApiConfig struct {
	Enabled              bool   `json:"enabled"`
	Listen               string `json:"listen"`
	PoolChartsNum        int64  `json:"poolChartsNum"`
	MinerChartsNum       int64  `json:"minerChartsNum"`
	PoolChartInterval    string `json:"poolChartInterval"`
	MinerChartCheckInterval string `json:"minerChartCheckInterval"`
	MinerChartInterval   string `json:"minerChartInterval"`
	MinerPoolTimeout	 string `json:"minerPoolTimeout"`
	StatsCollectInterval string `json:"statsCollectInterval"`
	HashrateWindow       string `json:"hashrateWindow"`
	HashrateLargeWindow  string `json:"hashrateLargeWindow"`
	LuckWindow           []int  `json:"luckWindow"`
	Payments             int64  `json:"payments"`
	Blocks               int64  `json:"blocks"`
	PurgeOnly            bool   `json:"purgeOnly"`
	PurgeInterval        string `json:"purgeInterval"`
	Coin				string
	Name 				string
	Depth				int64
}

type ApiServer struct {
	config              *ApiConfig
	backend             *redis.RedisClient
	hashrateWindow      time.Duration
	hashrateLargeWindow time.Duration
	stats               atomic.Value
	miners              map[string]*Entry
	db					*mysql.Database
	minersMu            sync.RWMutex
	statsIntv           time.Duration
	minerPoolTimeout	time.Duration
	minerPoolChartIntv	int64

	//poolChartIntv       time.Duration
	//minerChartIntv      time.Duration
}

type Entry struct {
	stats     map[string]interface{}
	updatedAt int64
}

func NewApiServer(cfg *ApiConfig, coin string, name string, backend *redis.RedisClient, db *mysql.Database) *ApiServer {
	hashrateWindow := util.MustParseDuration(cfg.HashrateWindow)
	hashrateLargeWindow := util.MustParseDuration(cfg.HashrateLargeWindow)
	return &ApiServer{
		config:              cfg,
		backend:             backend,
		hashrateWindow:      hashrateWindow,
		hashrateLargeWindow: hashrateLargeWindow,
		miners:              make(map[string]*Entry),
		db:					db,
	}
}

func (s *ApiServer) Start() {
	if s.config.PurgeOnly {
		log.Printf("Starting API in purge-only mode")
	} else {
		log.Printf("Starting API on %v", s.config.Listen)
	}

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

	sort.Ints(s.config.LuckWindow)

	if s.config.PurgeOnly {
		s.purgeStale()
	} else {
		s.purgeStale()
		s.collectStats()
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
						reportedHash, reportTime, _ := s.backend.GetReportedtHashrate(miner.Addr)

						if reportedHash < 0 || reportTime + 1200 < ts{
							reportedHash = 0
						}
						online, _, totalHashrate , currentHashrate := s.backend.CollectWorkersStatsEx(s.hashrateWindow, s.hashrateLargeWindow, miner.Addr)
						// stats, _ := s.backend.CollectWorkersStats(s.hashrateWindow, s.hashrateLargeWindow, miner.Addr)
						s.collectMinerCharts(miner.Addr, currentHashrate, totalHashrate, online, int64(miner.Share), reportedHash)
					}
				}
				minerChartTimer.Reset(minerChartCheckIntv)
			}
		}
	}()

	if !s.config.PurgeOnly {
		s.listen()
	}
}

func authenticationMiddleware (next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		//token := r.Header.Get("X-Access-Token")

		next.ServeHTTP(w, r)
	})
}

func (s *ApiServer) listen() {
	r := mux.NewRouter()
	r.HandleFunc("/api/stats", s.StatsIndex)
	r.HandleFunc("/api/miners", s.MinersIndex)
	r.HandleFunc("/api/blocks", s.BlocksIndex)
	r.HandleFunc("/api/payments", s.PaymentsIndex)
	r.HandleFunc("/api/accounts/{login:0x[0-9a-fA-F]{40}}", s.AccountIndex)
	//r.HandleFunc("/api/accounts/{login:0x[0-9a-fA-F]{40}}/{personal:0x[0-9a-fA-F]{40}}", s.AccountIndexEx)
	r.NotFoundHandler = http.HandlerFunc(notFound)
	r.Use(authenticationMiddleware )

	err := r.Walk(func(route *mux.Route, router *mux.Router, ancestors []*mux.Route) error {
		pathTemplate, err := route.GetPathTemplate()
		if err == nil {
			fmt.Println("ROUTE:", pathTemplate)
		}
		pathRegexp, err := route.GetPathRegexp()
		if err == nil {
			fmt.Println("Path regexp:", pathRegexp)
		}
		queriesTemplates, err := route.GetQueriesTemplates()
		if err == nil {
			fmt.Println("Queries templates:", strings.Join(queriesTemplates, ","))
		}
		queriesRegexps, err := route.GetQueriesRegexp()
		if err == nil {
			fmt.Println("Queries regexps:", strings.Join(queriesRegexps, ","))
		}
		methods, err := route.GetMethods()
		if err == nil {
			fmt.Println("Methods:", strings.Join(methods, ","))
		}
		fmt.Println()
		return nil
	})

	err = http.ListenAndServe(s.config.Listen, r)
	if err != nil {
		log.Fatalf("Failed to start API: %v", err)
	}
}

func notFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
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
	w.Header().Set("Access-Control-Allow-Origin", "*")
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

func (s *ApiServer) TestIndex(w http.ResponseWriter, r *http.Request) {

}

func (s *ApiServer) AccountIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
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
		exist, err := s.backend.IsMinerExists(login)
		if !exist {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}

		stats, err := s.backend.GetMinerStats(login, s.config.Payments)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		workers, err := s.backend.CollectWorkersStats(s.hashrateWindow, s.hashrateLargeWindow, login)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			log.Printf("Failed to fetch stats from backend: %v", err)
			return
		}
		for key, value := range workers {
			stats[key] = value
		}
		stats["pageSize"] = s.config.Payments
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

//
//func (s *ApiServer) AccountIndexEx(w http.ResponseWriter, r *http.Request) {
//	w.Header().Set("Content-Type", "application/json; charset=UTF-8")
//	w.Header().Set("Access-Control-Allow-Origin", "*")
//	w.Header().Set("Cache-Control", "no-cache")
//
//	login := strings.ToLower(mux.Vars(r)["login"])
//	s.minersMu.Lock()
//	defer s.minersMu.Unlock()
//
//	reply, ok := s.miners[login]
//	now := util.MakeTimestamp()
//	cacheIntv := int64(s.statsIntv / time.Millisecond)
//	// Refresh stats if stale
//	if !ok || reply.updatedAt < now-cacheIntv {
//		exist, err := s.backend.IsMinerExists(login)
//		if !exist {
//			w.WriteHeader(http.StatusNotFound)
//			return
//		}
//		if err != nil {
//			w.WriteHeader(http.StatusInternalServerError)
//			log.Printf("Failed to fetch stats from backend: %v", err)
//			return
//		}
//
//		stats, err := s.backend.GetMinerStats(login, s.config.Payments)
//		if err != nil {
//			w.WriteHeader(http.StatusInternalServerError)
//			log.Printf("Failed to fetch stats from backend: %v", err)
//			return
//		}
//		workers, err := s.backend.CollectWorkersStats(s.hashrateWindow, s.hashrateLargeWindow, login)
//		if err != nil {
//			w.WriteHeader(http.StatusInternalServerError)
//			log.Printf("Failed to fetch stats from backend: %v", err)
//			return
//		}
//		for key, value := range workers {
//			stats[key] = value
//		}
//		stats["pageSize"] = s.config.Payments
//		stats["minerCharts"], err = s.backend.GetMinerCharts(s.config.MinerChartsNum, login)
//		//stats["paymentCharts"], err = s.backend.GetPaymentCharts(login)
//		reply = &Entry{stats: stats, updatedAt: now}
//		s.miners[login] = reply
//	}
//
//	w.WriteHeader(http.StatusOK)
//	err := json.NewEncoder(w).Encode(reply.stats)
//	if err != nil {
//		log.Println("Error serializing API response: ", err)
//	}
//}


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

	log.Println("Miner "+login+" Hash is", ts, t2, hash, largeHash, share, report)
	err := s.db.WriteMinerCharts(ts, t2, login, hash, largeHash, workerOnline, share, report)
	// err := s.backend.WriteMinerCharts(ts, t2, login, hash, largeHash, workerOnline, share, report)
	if err != nil {
		log.Printf("Failed to fetch miner %v charts from backend: %v", login, err)
	}
}