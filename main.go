// +build go1.9

package main

import (
	"encoding/json"
	"github.com/cellcrypto/open-dangnn-pool/hook"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/yvasiyarov/gorelic"

	"github.com/cellcrypto/open-dangnn-pool/api"
	"github.com/cellcrypto/open-dangnn-pool/payouts"
	"github.com/cellcrypto/open-dangnn-pool/proxy"
	"github.com/cellcrypto/open-dangnn-pool/storage/mysql"
	"github.com/cellcrypto/open-dangnn-pool/storage/redis"
	"github.com/cellcrypto/open-dangnn-pool/util/plogger"
)

var cfg proxy.Config
var backend *redis.RedisClient
var db *mysql.Database
var logger *plogger.Logger

func startProxy() {
	s := proxy.NewProxy(&cfg, backend, db)
	s.Start()
}

func startApi() {
	s := api.NewApiServer(&cfg.Api, cfg.Coin, cfg.Name, backend, db)
	s.Start()
}

func startBlockUnlocker() {
	u := payouts.NewBlockUnlocker(&cfg.BlockUnlocker, backend, db)
	u.Start()
}

func startPayoutsProcessor() {
	u := payouts.NewPayoutsProcessor(&cfg.Payouts, backend, db)
	u.Start()
}

func startNewrelic() {
	if cfg.NewrelicEnabled {
		nr := gorelic.NewAgent()
		nr.Verbose = cfg.NewrelicVerbose
		nr.NewrelicLicense = cfg.NewrelicKey
		nr.NewrelicName = cfg.NewrelicName
		nr.Run()
	}
}

func readConfig(cfg *proxy.Config) {
	configFileName := "config.json"
	if len(os.Args) > 1 {
		configFileName = os.Args[1]
	}
	configFileName, _ = filepath.Abs(configFileName)
	log.Printf("Loading config: %v", configFileName)

	configFile, err := os.Open(configFileName)
	if err != nil {
		log.Fatal("File error: ", err.Error())
	}
	defer configFile.Close()
	jsonParser := json.NewDecoder(configFile)
	if err := jsonParser.Decode(&cfg); err != nil {
		log.Fatal("Config error: ", err.Error())
	}

	if cfg.Mysql.Coin == "" {
		cfg.Mysql.Coin = cfg.Coin
	}

	cfg.Api.Coin = cfg.Coin
	cfg.Api.Name = cfg.Name
	cfg.Api.Depth = cfg.BlockUnlocker.Depth
}

func main() {
	readConfig(&cfg)
	rand.Seed(time.Now().UnixNano())

	if cfg.Threads > 0 {
		runtime.GOMAXPROCS(cfg.Threads)
		log.Printf("Running with %v threads", cfg.Threads)

		if cfg.Threads < cfg.Payouts.ConcurrentTx {
			log.Printf("The goroutine is smaller than the payout thread GOMAXPROCS:%v > ConcurrentTx:%v ", cfg.Threads, cfg.Payouts.ConcurrentTx)
		}
	}

	startNewrelic()

	backend = redis.NewRedisClient(&cfg.Redis, cfg.Coin, cfg.Proxy.Difficulty, cfg.Pplns)
	pong, err := backend.Check()
	if err != nil {
		log.Printf("Can't establish connection to backend: %v", err)
	} else {
		log.Printf("Backend check reply: %v", pong)
	}

	if db, err = mysql.New(&cfg.Mysql, cfg.Proxy.Difficulty, backend); err != nil {
		log.Printf("Can't establish connection to mysql: %v", err)
		os.Exit(1)
	}
	backend.SetDB(db)

	log.Printf("connected mysql host:%v",cfg.Mysql.Endpoint)

	hook.RegistryMainHook(func() {

		logger.Close()	// Save all logs.
	})

	// logger is pooling
	logger = plogger.New(db, cfg.Coin)

	if cfg.Proxy.Enabled {
		go startProxy()
	}
	if cfg.Api.Enabled {
		go startApi()
	}
	if cfg.BlockUnlocker.Enabled {
		go startBlockUnlocker()
	}
	if cfg.Payouts.Enabled {
		go startPayoutsProcessor()
	}

	hook.Listen()


}
