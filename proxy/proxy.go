package proxy

import (
	"encoding/json"
	"fmt"
	"github.com/cellcrypto/open-dangnn-pool/hook"
	"github.com/cellcrypto/open-dangnn-pool/util/plogger"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/mux"

	"github.com/cellcrypto/open-dangnn-pool/policy"
	"github.com/cellcrypto/open-dangnn-pool/rpc"
	"github.com/cellcrypto/open-dangnn-pool/storage/mysql"
	"github.com/cellcrypto/open-dangnn-pool/storage/redis"
	"github.com/cellcrypto/open-dangnn-pool/util"
)

type ProxyServer struct {
	config             *Config
	blockTemplate      atomic.Value
	upstream           int32
	upstreams          []*rpc.RPCClient
	backend            *redis.RedisClient
	db 				   *mysql.Database
	diff               string
	policy             *policy.PolicyServer
	hashrateExpiration time.Duration
	failsCount         int64
	reportRatesMu sync.RWMutex
	reportRates		   map[string]*ReportedRate

	// Stratum
	sessionsMu sync.RWMutex
	sessions   map[*Session]struct{}
	timeout    time.Duration

	subMinerMu sync.RWMutex
	subMiner map[string]*MinerSubInfo

	// alarm
	minerBeatIntv int64
}

type ReportedRate struct {
	rate int64
	insertTime int64
}

type Session struct {
	ip  string
	enc *json.Encoder

	// Stratum
	sync.Mutex
	conn  *net.TCPConn
	login string
}

func NewProxy(cfg *Config, backend *redis.RedisClient, db *mysql.Database) *ProxyServer {
	if len(cfg.Name) == 0 {
		log.Fatal("You must set instance name")
	}
	policy := policy.Start(&cfg.Proxy.Policy, backend, db)
	proxy := &ProxyServer{config: cfg, backend: backend, db: db, policy: policy}
	proxy.diff = util.GetTargetHex(cfg.Proxy.Difficulty)

	proxy.upstreams = make([]*rpc.RPCClient, len(cfg.Upstream))
	for i, v := range cfg.Upstream {
		proxy.upstreams[i] = rpc.NewRPCClient(v.Name, v.Url, v.Timeout, cfg.NetId)
		log.Printf("Upstream: %s => %s", v.Name, v.Url)
	}
	log.Printf("Default upstream: %s => %s", proxy.rpc().Name, proxy.rpc().Url)

	if cfg.Proxy.Stratum.Enabled {
		proxy.sessions = make(map[*Session]struct{})
		go proxy.ListenTCP()
	}

	proxy.reportRates = make(map[string]*ReportedRate,0)
	proxy.subMiner = make(map[string]*MinerSubInfo,0)

	proxy.InitSubLogin()
	proxy.fetchBlockTemplate()

	proxy.hashrateExpiration = util.MustParseDuration(cfg.Proxy.HashrateExpiration)

	refreshIntv := util.MustParseDuration(cfg.Proxy.BlockRefreshInterval)
	refreshTimer := time.NewTimer(refreshIntv)
	log.Printf("Set block refresh every %v", refreshIntv)

	checkIntv := util.MustParseDuration(cfg.UpstreamCheckInterval)
	checkTimer := time.NewTimer(checkIntv)

	stateUpdateIntv := util.MustParseDuration(cfg.Proxy.StateUpdateInterval)
	stateUpdateTimer := time.NewTimer(stateUpdateIntv)

	quit := make(chan struct{})
	hooks := make(chan struct{})

	plogger.InsertLog("START PROXY SERVER", plogger.LogTypeSystem, plogger.LogErrorNothing, 0, 0, "", "")
	hook.RegistryHook("proxy.go", func(name string) {
		plogger.InsertLog("SHUTDOWN PROXY SERVER", plogger.LogTypeSystem, plogger.LogErrorNothing, 0, 0, "", "")
		close(quit)
		<- hooks
	})

	go func() {
		for {
			select {
			case <-refreshTimer.C:
				proxy.fetchBlockTemplate()
				refreshTimer.Reset(refreshIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-checkTimer.C:
				proxy.checkUpstreams()
				checkTimer.Reset(checkIntv)
			}
		}
	}()

	go func() {
		for {
			select {
			case <-quit:
				hooks <- struct{}{}
				return
			case <-stateUpdateTimer.C:
				t := proxy.currentBlockTemplate()
				if t != nil {
					err := backend.WriteNodeState(cfg.Name, t.Height, t.Difficulty)
					if err != nil {
						log.Printf("Failed to write node state to backend: %v", err)
						proxy.markSick()
					} else {
						proxy.markOk()
					}
				}
				stateUpdateTimer.Reset(stateUpdateIntv)
			}
		}
	}()

	return proxy
}

func (s *ProxyServer) RedisMessage(payload string) {
	splitData := strings.Split(payload,":")
	if len(splitData) != 3 {
		return
	}
	opcode := splitData[0]
	from := splitData[1]
	msg := splitData[2]
	switch opcode {
	case redis.OpcodeLoadID:
		s.policy.RefreshInboundID()
	case redis.OpcodeLoadIP:
		s.policy.RefreshInboundIP()
	case redis.OpcodeWhiteList:
		s.policy.RefreshBanWhiteList()
	case redis.OpcodeMinerSub:
		s.InitSubLogin()
	default:
		log.Printf("not defined opcode: %v", opcode)
	}

	fmt.Printf("(opcode:%v from:%s)RedisMessage: %s\n", opcode, from, msg)
}

func (s *ProxyServer) Start() {
	log.Printf("Starting proxy on %v", s.config.Proxy.Listen)
	r := mux.NewRouter()
	r.Handle("/{login:0x[0-9a-fA-F]{40}}/{id:[0-9a-zA-Z-_]{1,8}}", s)
	r.Handle("/{login:0x[0-9a-fA-F]{40}}", s)
	srv := &http.Server{
		Addr:           s.config.Proxy.Listen,
		Handler:        r,
		MaxHeaderBytes: s.config.Proxy.LimitHeadersSize,
	}

	s.backend.InitPubSub("proxy",s)

	err := srv.ListenAndServe()
	if err != nil {
		log.Fatalf("Failed to start proxy: %v", err)
	}
}

func (s *ProxyServer) rpc() *rpc.RPCClient {
	i := atomic.LoadInt32(&s.upstream)
	return s.upstreams[i]
}

func (s *ProxyServer) checkUpstreams() {
	candidate := int32(0)
	backup := false

	for i, v := range s.upstreams {
		if v.Check() && !backup {
			candidate = int32(i)
			backup = true
		}
	}

	if s.upstream != candidate {
		log.Printf("Switching to %v upstream", s.upstreams[candidate].Name)
		atomic.StoreInt32(&s.upstream, candidate)
	}
}

func (s *ProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		s.writeError(w, 405, "rpc: POST method required, received "+r.Method)
		return
	}
	ip := s.remoteAddr(r)
	if !s.policy.CheckInboundIP(ip) {
		log.Printf("Invalid Ip : %s", ip)
		s.writeError(w, 404, "rpc: authenticationError, received "+r.Method)
		return
	}
	if !s.policy.IsBanned(ip) {
		s.handleClient(w, r, ip)
	}
}

func (s *ProxyServer) remoteAddr(r *http.Request) string {
	if s.config.Proxy.BehindReverseProxy {
		ip := r.Header.Get("X-Forwarded-For")
		if len(ip) > 0 && net.ParseIP(ip) != nil {
			return ip
		}
	}
	ip, _, _ := net.SplitHostPort(r.RemoteAddr)
	return ip
}

func (s *ProxyServer) handleClient(w http.ResponseWriter, r *http.Request, ip string) {
	if r.ContentLength > s.config.Proxy.LimitBodySize {
		log.Printf("Socket flood from %s", ip)
		s.policy.ApplyMalformedPolicy(ip)
		http.Error(w, "Request too large", http.StatusExpectationFailed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, s.config.Proxy.LimitBodySize)
	defer r.Body.Close()

	cs := &Session{ip: ip, enc: json.NewEncoder(w)}
	dec := json.NewDecoder(r.Body)
	for {
		var req JSONRpcReq
		if err := dec.Decode(&req); err == io.EOF {
			break
		} else if err != nil {
			log.Printf("Malformed request from %v: %v", ip, err)
			s.policy.ApplyMalformedPolicy(ip)
			return
		}
		cs.handleMessage(s, r, &req)
	}
}

func (cs *Session) handleMessage(s *ProxyServer, r *http.Request, req *JSONRpcReq) {
	if req.Id == nil {
		log.Printf("Missing RPC id from %s", cs.ip)
		s.policy.ApplyMalformedPolicy(cs.ip)
		return
	}

	vars := mux.Vars(r)
	login := strings.ToLower(vars["login"])

	if !util.IsValidHexAddress(login) {
		errReply := &ErrorReply{Code: -1, Message: "Invalid login"}
		cs.sendError(req.Id, errReply)
		return
	}
	if !s.policy.ApplyLoginPolicy(login, cs.ip) {
		errReply := &ErrorReply{Code: -1, Message: "You are blacklisted"}
		cs.sendError(req.Id, errReply)
		return
	}

	// Handle RPC methods
	switch req.Method {
	case "eth_getWork":
		reply, errReply := s.handleGetWorkRPC(cs)
		if errReply != nil {
			cs.sendError(req.Id, errReply)
			break
		}
		cs.sendResult(req.Id, &reply)
	case "eth_submitWork":
		if req.Params != nil {
			var params []string
			err := json.Unmarshal(req.Params, &params)
			if err != nil {
				log.Printf("Unable to parse params from %v", cs.ip)
				s.policy.ApplyMalformedPolicy(cs.ip)
				break
			}
			reply, errReply := s.handleSubmitRPC(cs, login, vars["id"], params)
			if errReply != nil {
				cs.sendError(req.Id, errReply)
				break
			}
			cs.sendResult(req.Id, &reply)
		} else {
			s.policy.ApplyMalformedPolicy(cs.ip)
			errReply := &ErrorReply{Code: -1, Message: "Malformed request"}
			cs.sendError(req.Id, errReply)
		}
	case "eth_getBlockByNumber":
		reply := s.handleGetBlockByNumberRPC()
		cs.sendResult(req.Id, reply)
	case "eth_submitHashrate":
		var params []string
		err := json.Unmarshal(req.Params, &params)
		if err != nil {
			log.Println("Malformed stratum request params from", cs.ip)
			return
		}
		s.handleSubmitHashRateRPC(cs, cs.login, params[0], "")
		cs.sendResult(req.Id, true)
	default:
		errReply := s.handleUnknownRPC(cs, req.Method)
		cs.sendError(req.Id, errReply)
	}
}

func (cs *Session) sendResult(id json.RawMessage, result interface{}) error {
	message := JSONRpcResp{Id: id, Version: "2.0", Error: nil, Result: result}
	return cs.enc.Encode(&message)
}

func (cs *Session) sendError(id json.RawMessage, reply *ErrorReply) error {
	message := JSONRpcResp{Id: id, Version: "2.0", Error: reply}
	return cs.enc.Encode(&message)
}

func (s *ProxyServer) writeError(w http.ResponseWriter, status int, msg string) {
	w.WriteHeader(status)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
}

func (s *ProxyServer) currentBlockTemplate() *BlockTemplate {
	t := s.blockTemplate.Load()
	if t != nil {
		return t.(*BlockTemplate)
	} else {
		return nil
	}
}

func (s *ProxyServer) markSick() {
	atomic.AddInt64(&s.failsCount, 1)
}

func (s *ProxyServer) isSick() bool {
	x := atomic.LoadInt64(&s.failsCount)
	if s.config.Proxy.HealthCheck && x >= s.config.Proxy.MaxFails {
		return true
	}
	return false
}

func (s *ProxyServer) markOk() {
	atomic.StoreInt64(&s.failsCount, 0)
}

func (s *ProxyServer) InitSubLogin() {
	subList, err := s.db.GetMinerSubList()
	if err != nil {
		log.Fatalf("failed to GetMinerSubList: %v", err)
		return
	}

	tmpSubMiner := make(map[string]*MinerSubInfo)

	for _, sub := range subList {
		minerSubInfo, ok := tmpSubMiner[sub.DevAddr]
		if !ok {
			minerSubInfo = &MinerSubInfo{
				login:       sub.DevAddr,
				choice:      0,
				timeout:     0,
				totalCount:  0,
				subLogins:   make([]string, 0, 18),
				subLoginMap: make(map[string]int),
			}
			tmpSubMiner[sub.DevAddr] = minerSubInfo
		}

		if sub.Amount <= 0 {
			sub.Amount = 1
		}
		for i:=int64(0);i < sub.Amount;i++ {
			minerSubInfo.subLogins = append(minerSubInfo.subLogins, sub.SubAddr)
		}

		if subLoginMap, ok := minerSubInfo.subLoginMap[sub.SubAddr]; ok {
			minerSubInfo.subLoginMap[sub.SubAddr] = subLoginMap + int(sub.Amount)
			minerSubInfo.totalCount += sub.Amount
		} else {
			minerSubInfo.subLoginMap[sub.SubAddr] = int(sub.Amount)
			minerSubInfo.totalCount += sub.Amount
		}
	}

	// randomly shuffle minerSubInfo.subLogins
	for _, value  := range  tmpSubMiner {
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(value.subLogins), func(i, j int) { value.subLogins[i], value.subLogins[j] = value.subLogins[j], value.subLogins[i] })
	}

	fmt.Printf("Re-load separated minor information: \n total size: %v sub size: %v\n",len(subList), len(tmpSubMiner))

	s.subMinerMu.Lock()
	s.subMiner = tmpSubMiner
	s.subMinerMu.Unlock()
}
