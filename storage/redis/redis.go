package redis

import (
	"fmt"
	"github.com/cellcrypto/open-dangnn-pool/storage/types"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"

	"gopkg.in/redis.v3"

	"github.com/cellcrypto/open-dangnn-pool/util"
)

type Config struct {
	Endpoint string `json:"endpoint"`
	Password string `json:"password"`
	Database int64  `json:"database"`
	PoolSize int    `json:"poolSize"`
}

type RedisClient struct {
	client *redis.Client
	mysql IMysqlDB
	prefix string
	pplns  int64
	DiffByShareValue int64
}

type PoolCharts struct {
	Timestamp  int64  `json:"x"`
	TimeFormat string `json:"timeFormat"`
	PoolHash   int64  `json:"y"`
}

type PaymentCharts struct {
	Timestamp  int64  `json:"x"`
	TimeFormat string `json:"timeFormat"`
	Amount     int64  `json:"amount"`
}

type SumRewardData struct {
	Interval int64  `json:"inverval"`
	Reward   int64  `json:"reward"`
	Name     string `json:"name"`
	Offset   int64  `json:"offset"`
}

type Miner struct {
	LastBeat  int64 `json:"lastBeat"`
	HR        int64 `json:"hr"`
	Offline   bool  `json:"offline"`
	startedAt int64
}

type Worker struct {
	Miner
	TotalHR int64 `json:"hr2"`
	WorkerDiff     int64  `json:"difficulty"`
	WorkerHostname string `json:"hostname"`
	Size  			int64 `json:"size"`
	Reported		int64 `json:"reported"`
}

type IMysqlDB interface {
	WriteCandidates(height uint64, params []string, nowTime string, ts int64, roundDiff int64, totalShares int64)
	CollectLuckStats(windowMax int64) ([]*types.BlockData,error)
	CollectStats(maxBlocks int64) ([]*types.BlockData, []*types.BlockData, []*types.BlockData, int, []map[string]interface{}, int64, error)
	GetMinerStats(login string, maxPayments int64) (map[string]interface{}, error)
	GetChartRewardList(login string, maxList int) ([]*types.RewardData, error)
	//GetAllPayments(maxPayments int64) ([]map[string]interface{}, error)
}

func NewRedisClient(cfg *Config, prefix string, proxyDiff int64, pplns int64) *RedisClient {
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.Endpoint,
		Password: cfg.Password,
		DB:       cfg.Database,
		PoolSize: cfg.PoolSize,
	})
	return &RedisClient{client: client, prefix: prefix, pplns: pplns, DiffByShareValue: proxyDiff}
}

func (r *RedisClient) Client() *redis.Client {
	return r.client
}

func (r *RedisClient) Check() (string, error) {
	return r.client.Ping().Result()
}

func (r *RedisClient) BgSave() (string, error) {
	return r.client.BgSave().Result()
}

// Always returns list of addresses. If Redis fails it will return empty list.
func (r *RedisClient) GetBlacklist() ([]string, error) {
	cmd := r.client.SMembers(r.formatKey("blacklist"))
	if cmd.Err() != nil {
		return []string{}, cmd.Err()
	}
	return cmd.Val(), nil
}

// Always returns list of IPs. If Redis fails it will return empty list.
func (r *RedisClient) GetWhitelist() ([]string, error) {
	cmd := r.client.SMembers(r.formatKey("whitelist"))
	if cmd.Err() != nil {
		return []string{}, cmd.Err()
	}
	return cmd.Val(), nil
}

// WritePoolCharts is pool charts
func (r *RedisClient) WritePoolCharts(time1 int64, time2 string, poolHash string) error {
	s := util.Join(time1, time2, poolHash)
	cmd := r.client.ZAdd(r.formatKey("charts", "pool"), redis.Z{Score: float64(time1), Member: s})
	return cmd.Err()
}

func (r *RedisClient) WriteMinerCharts(time1 int64, time2, k string, hash, largeHash, workerOnline int64, share int64, report int64) error {
	s := util.Join(time1, time2, hash, largeHash, workerOnline, share, report)
	cmd := r.client.ZAdd(r.formatKey("charts", "miner", k), redis.Z{Score: float64(time1), Member: s})
	return cmd.Err()
}

func (r *RedisClient) GetPoolCharts(poolHashLen int64) (stats []*PoolCharts, err error) {

	tx := r.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(r.formatKey("charts", "pool"), "-inf", fmt.Sprint("(", now-172800))
		tx.ZRevRangeWithScores(r.formatKey("charts", "pool"), 0, poolHashLen)
		return nil
	})

	if err != nil {
		return nil, err
	}

	stats = convertPoolChartsResults(cmds[1].(*redis.ZSliceCmd))
	return stats, nil
}

func convertPoolChartsResults(raw *redis.ZSliceCmd) []*PoolCharts {
	var result []*PoolCharts
	for _, v := range raw.Val() {
		// "Timestamp:TimeFormat:Hash"
		pc := PoolCharts{}
		pc.Timestamp = int64(v.Score)
		str := v.Member.(string)
		pc.TimeFormat = str[strings.Index(str, ":")+1 : strings.LastIndex(str, ":")]
		pc.PoolHash, _ = strconv.ParseInt(str[strings.LastIndex(str, ":")+1:], 10, 64)
		result = append(result, &pc)
	}
	return result
}

func convertMinerChartsResults(raw *redis.ZSliceCmd) []*types.MinerCharts {
	var result []*types.MinerCharts
	for _, v := range raw.Val() {
		// "Timestamp:TimeFormat:Hash:largeHash:workerOnline"
		mc := types.MinerCharts{}
		mc.Timestamp = int64(v.Score)
		str := v.Member.(string)
		splitStr := strings.Split(str, ":")
		mc.TimeFormat = splitStr[1]
		mc.MinerHash, _ = strconv.ParseInt(splitStr[2], 10, 64)
		mc.MinerLargeHash, _ = strconv.ParseInt(splitStr[3], 10, 64)
		mc.WorkerOnline = splitStr[4]
		if len(splitStr) > 5 {
			mc.Share, _ = strconv.ParseInt(splitStr[5], 10, 64)
			mc.MinerReportHash, _ = strconv.ParseInt(splitStr[6], 10, 64)
		}
		result = append(result, &mc)
	}
	return result
}

func (r *RedisClient) GetAllMinerAccount() (account []string, err error) {
	var c int64
	for {
		now := util.MakeTimestamp() / 1000
		c, keys, err := r.client.Scan(c, r.formatKey("miners", "*"), now).Result()

		if err != nil {
			return account, err
		}
		for _, key := range keys {
			m := strings.Split(key, ":")
			//if ( len(m) >= 2 && strings.Index(strings.ToLower(m[2]), "0x") == 0) {
			if len(m) >= 2 {
				account = append(account, m[2])
			}
		}
		if c == 0 {
			break
		}
	}
	return account, nil
}

func (r *RedisClient) GetMinerCharts(hashNum int64, login string) (stats []*types.MinerCharts, err error) {

	tx := r.client.Multi()
	defer tx.Close()
	now := util.MakeTimestamp() / 1000
	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(r.formatKey("charts", "miner", login), "-inf", fmt.Sprint("(", now-172800))
		tx.ZRevRangeWithScores(r.formatKey("charts", "miner", login), 0, hashNum)
		return nil
	})
	if err != nil {
		return nil, err
	}
	stats = convertMinerChartsResults(cmds[1].(*redis.ZSliceCmd))
	return stats, nil
}

func (r *RedisClient) GetPaymentCharts(login string) (stats []*PaymentCharts, err error) {

	tx := r.client.Multi()
	defer tx.Close()
	cmds, err := tx.Exec(func() error {
		tx.ZRevRangeWithScores(r.formatKey("payments", login), 0, 360)
		return nil
	})
	if err != nil {
		return nil, err
	}
	stats = convertPaymentChartsResults(cmds[0].(*redis.ZSliceCmd))
	//fmt.Println(stats)
	return stats, nil
}



func (r *RedisClient) WriteNodeState(id string, height uint64, diff *big.Int) error {
	tx := r.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	_, err := tx.Exec(func() error {
		tx.HSet(r.formatKey("nodes"), util.Join(id, "name"), id)
		tx.HSet(r.formatKey("nodes"), util.Join(id, "height"), strconv.FormatUint(height, 10))
		tx.HSet(r.formatKey("nodes"), util.Join(id, "difficulty"), diff.String())
		tx.HSet(r.formatKey("nodes"), util.Join(id, "lastBeat"), strconv.FormatInt(now, 10))
		return nil
	})
	return err
}


func (r *RedisClient) GetNodeHeight(id string) (int64, error) {
	cmd := r.client.HGet(r.formatKey("nodes"), util.Join(id, "height"))
	if cmd.Err() == redis.Nil {
		return 0, nil
	} else if cmd.Err() != nil {
		return 0, cmd.Err()
	}
	return cmd.Int64()
}

func (r *RedisClient) GetNodeStates() ([]map[string]interface{}, error) {
	cmd := r.client.HGetAllMap(r.formatKey("nodes"))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	m := make(map[string]map[string]interface{})
	for key, value := range cmd.Val() {
		parts := strings.Split(key, ":")
		if val, ok := m[parts[0]]; ok {
			val[parts[1]] = value
		} else {
			node := make(map[string]interface{})
			node[parts[1]] = value
			m[parts[0]] = node
		}
	}
	v := make([]map[string]interface{}, len(m), len(m))
	i := 0
	for _, value := range m {
		v[i] = value
		i++
	}
	return v, nil
}

func (r *RedisClient) CheckPoWExist(height uint64, params []string) (bool, error) {
	// Sweep PoW backlog for previous blocks, we have 3 templates back in RAM
	r.client.ZRemRangeByScore(r.formatKey("pow"), "-inf", fmt.Sprint("(", height-8))
	val, err := r.client.ZAdd(r.formatKey("pow"), redis.Z{Score: float64(height), Member: strings.Join(params, ":")}).Result()
	return val == 0, err
}

func (r *RedisClient) WriteShare(login, id string, params []string, diff int64, height uint64, window time.Duration, hostname string, loginCnt int) (bool, error) {
	tx := r.client.Multi()
	defer tx.Close()

	ms := util.MakeTimestamp()
	ts := ms / 1000

	_, err := tx.Exec(func() error {
		r.writeShare(tx, ms, ts, login, id, diff, window, hostname, loginCnt)
		tx.HIncrBy(r.formatKey("stats"), "roundShares", diff)
		return nil
	})
	return false, err
}

func (r *RedisClient) WriteBlock(login, id string, params []string, diff, roundDiff int64, height uint64, window time.Duration, hostname string, loginCnt int) (bool, error) {
	tx := r.client.Multi()
	defer tx.Close()

	nowTime := time.Now()
	ms := nowTime.UnixNano() / int64(time.Millisecond)
	ts := ms / 1000

	cmds, err := tx.Exec(func() error {
		r.writeShare(tx, ms, ts, login, id, diff, window, hostname, loginCnt)
		tx.HSet(r.formatKey("stats"), "lastBlockFound", strconv.FormatInt(ts, 10))
		tx.HDel(r.formatKey("stats"), "roundShares")
		tx.ZIncrBy(r.formatKey("finders"), 1, login)
		//tx.HIncrBy(r.formatKey("miners", login), "blocksFound", 1)
		tx.HGetAllMap(r.formatKey("shares", "roundCurrent"))
		tx.Del(r.formatKey("shares", "roundCurrent"))
		tx.LRange(r.formatKey("lastshares"), 0, r.pplns)
		return nil
	})
	if err != nil {
		return false, err
	} else {

		shares := cmds[len(cmds)-1].(*redis.StringSliceCmd).Val()

		tx2 := r.client.Multi()
		defer tx2.Close()

		totalshares := make(map[string]int64)
		for _, val := range shares {
			totalshares[val] += 1
		}

		_, err := tx2.Exec(func() error {
			for k, v := range totalshares {
				tx2.HIncrBy(r.formatRound(int64(height), params[0]), k, v)
			}
			return nil
		})
		if err != nil {
			return false, err
		}
		//r.mysql.WriteRoundShare(height, params[0], totalshares)

		sharesMap, _ := cmds[len(cmds)-3].(*redis.StringStringMapCmd).Result()
		totalShares := int64(0)
		for _, v := range sharesMap {
			n, _ := strconv.ParseInt(v, 10, 64)
			totalShares += n
		}

		r.mysql.WriteCandidates(height, params, nowTime.Format("2006-01-02 15:04:05.000"), ts, roundDiff, totalShares)
		return false, nil
	}
}

func (r *RedisClient) writeShare(tx *redis.Multi, ms, ts int64, login, id string, diff int64, expire time.Duration, hostname string, loginCnt int) {
	times := int(diff / r.DiffByShareValue)

	// Moved get hostname to stratums

	if times > 0 {	// Share is incremented by one.
		tx.LPush(r.formatKey("lastshares"), login)
	}
	tx.LTrim(r.formatKey("lastshares"), 0, r.pplns)

	tx.HIncrBy(r.formatKey("shares", "roundCurrent"), login, diff)
	// For aggregation of hashrate, to store value in hashrate key
	tx.ZAdd(r.formatKey("hashrate"), redis.Z{Score: float64(ts), Member: util.Join(diff, login, id, ms, diff, hostname)})
	// For separate miner's workers hashrate, to store under hashrate table under login key
	tx.ZAdd(r.formatKey("hashrate", login), redis.Z{Score: float64(ts), Member: util.Join(diff, id, loginCnt, ms, diff, hostname)})
	// Will delete hashrates for miners that gone
	tx.Expire(r.formatKey("hashrate", login), expire)
	//tx.HSet(r.formatKey("miners", login), "lastShare", strconv.FormatInt(ts, 10))
}

func (r *RedisClient) formatKey(args ...interface{}) string {
	return util.Join(r.prefix, util.Join(args...))
}

func (r *RedisClient) formatRound(height int64, nonce string) string {
	return r.formatKey("shares", "round"+strconv.FormatInt(height, 10), nonce)
}


func (r *RedisClient) GetCandidates(maxHeight int64) ([]*types.BlockData, error) {
	option := redis.ZRangeByScore{Min: "0", Max: strconv.FormatInt(maxHeight, 10)}
	cmd := r.client.ZRangeByScoreWithScores(r.formatKey("blocks", "candidates"), option)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return convertCandidateResults(cmd), nil
}

func (r *RedisClient) GetImmatureBlocks(maxHeight int64) ([]*types.BlockData, error) {
	option := redis.ZRangeByScore{Min: "0", Max: strconv.FormatInt(maxHeight, 10)}
	cmd := r.client.ZRangeByScoreWithScores(r.formatKey("blocks", "immature"), option)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return convertBlockResults(cmd), nil
}

func (r *RedisClient) GetRewards(login string) ([]*types.RewardData, error) {
	option := redis.ZRangeByScore{Min: "0", Max: strconv.FormatInt(10, 10)}
	cmd := r.client.ZRangeByScoreWithScores(r.formatKey("rewards", login), option)
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	return convertRewardResults(cmd), nil
}

func (r *RedisClient) GetRoundShares(height int64, nonce string) (map[string]int64, error) {
	result := make(map[string]int64)
	cmd := r.client.HGetAllMap(r.formatRound(height, nonce))
	if cmd.Err() != nil {
		return nil, cmd.Err()
	}
	sharesMap, _ := cmd.Result()
	for login, v := range sharesMap {
		n, _ := strconv.ParseInt(v, 10, 64)
		result[login] = n
	}
	return result, nil
}

func (r *RedisClient) GetPayees() ([]string, error) {
	payees := make(map[string]struct{})
	var result []string
	var c int64

	for {
		var keys []string
		var err error
		c, keys, err = r.client.Scan(c, r.formatKey("miners", "*"), 100).Result()
		if err != nil {
			return nil, err
		}
		for _, row := range keys {
			login := strings.Split(row, ":")[2]
			payees[login] = struct{}{}
		}
		if c == 0 {
			break
		}
	}
	for login := range payees {
		result = append(result, login)
	}
	return result, nil
}

func (r *RedisClient) GetTotalShares() (int64, error) {
	cmd := r.client.LLen(r.formatKey("lastshares"))
	if cmd.Err() == redis.Nil {
		return 0, nil
	} else if cmd.Err() != nil {
		return 0, cmd.Err()
	}
	return cmd.Val(), nil
}

func (r *RedisClient) GetBalance(login string) (int64, error) {
	cmd := r.client.HGet(r.formatKey("miners", login), "balance")
	if cmd.Err() == redis.Nil {
		return 0, nil
	} else if cmd.Err() != nil {
		return 0, cmd.Err()
	}
	return cmd.Int64()
}

func (r *RedisClient) LockPayouts(login string, amount int64) error {
	key := r.formatKey("payments", "lock")
	result := r.client.SetNX(key, util.Join(login, amount), 0).Val()
	if !result {
		return fmt.Errorf("Unable to acquire lock '%s'", key)
	}
	return nil
}

func (r *RedisClient) UnlockPayouts() error {
	key := r.formatKey("payments", "lock")
	_, err := r.client.Del(key).Result()
	return err
}

func (r *RedisClient) IsPayoutsLocked() (bool, error) {
	_, err := r.client.Get(r.formatKey("payments", "lock")).Result()
	if err == redis.Nil {
		return false, nil
	} else if err != nil {
		return false, err
	} else {
		return true, nil
	}
}

type PendingPayment struct {
	Timestamp int64  `json:"timestamp"`
	Amount    int64  `json:"amount"`
	Address   string `json:"login"`
}

func (r *RedisClient) GetPendingPayments() []*PendingPayment {
	raw := r.client.ZRevRangeWithScores(r.formatKey("payments", "pending"), 0, -1)
	var result []*PendingPayment
	for _, v := range raw.Val() {
		// timestamp -> "address:amount"
		payment := PendingPayment{}
		payment.Timestamp = int64(v.Score)
		fields := strings.Split(v.Member.(string), ":")
		payment.Address = fields[0]
		payment.Amount, _ = strconv.ParseInt(fields[1], 10, 64)
		result = append(result, &payment)
	}
	return result
}

// UpdateBalance Deduct miner's balance for payment
func (r *RedisClient) UpdateBalance(login string, amount int64) error {
	tx := r.client.Multi()
	defer tx.Close()

	ts := util.MakeTimestamp() / 1000

	_, err := tx.Exec(func() error {
		tx.HIncrBy(r.formatKey("miners", login), "balance", (amount * -1))
		tx.HIncrBy(r.formatKey("miners", login), "pending", amount)
		tx.HIncrBy(r.formatKey("finances"), "balance", (amount * -1))
		tx.HIncrBy(r.formatKey("finances"), "pending", amount)
		tx.ZAdd(r.formatKey("payments", "pending"), redis.Z{Score: float64(ts), Member: util.Join(login, amount)})
		return nil
	})
	return err
}

func (r *RedisClient) RollbackBalance(login string, amount int64) error {
	tx := r.client.Multi()
	defer tx.Close()

	_, err := tx.Exec(func() error {
		tx.HIncrBy(r.formatKey("miners", login), "balance", amount)
		tx.HIncrBy(r.formatKey("miners", login), "pending", (amount * -1))
		tx.HIncrBy(r.formatKey("finances"), "balance", amount)
		tx.HIncrBy(r.formatKey("finances"), "pending", (amount * -1))
		tx.ZRem(r.formatKey("payments", "pending"), util.Join(login, amount))
		return nil
	})
	return err
}

func (r *RedisClient) WritePayment(login, txHash string, amount int64) error {
	tx := r.client.Multi()
	defer tx.Close()

	ts := util.MakeTimestamp() / 1000

	_, err := tx.Exec(func() error {
		tx.HIncrBy(r.formatKey("miners", login), "pending", (amount * -1))
		tx.HIncrBy(r.formatKey("miners", login), "paid", amount)
		tx.HIncrBy(r.formatKey("finances"), "pending", (amount * -1))
		tx.HIncrBy(r.formatKey("finances"), "paid", amount)
		tx.ZAdd(r.formatKey("payments", "all"), redis.Z{Score: float64(ts), Member: util.Join(txHash, login, amount)})
		tx.ZRemRangeByRank(r.formatKey("payments", "all"), 0, -10000)
		tx.ZAdd(r.formatKey("payments", login), redis.Z{Score: float64(ts), Member: util.Join(txHash, amount)})
		tx.ZRemRangeByRank(r.formatKey("payments", login), 0, -100)
		tx.ZRem(r.formatKey("payments", "pending"), util.Join(login, amount))
		tx.Del(r.formatKey("payments", "lock"))
		tx.HIncrBy(r.formatKey("paymentsTotal"), "all", 1)
		tx.HIncrBy(r.formatKey("paymentsTotal"), login, 1)
		return nil
	})
	return err
}

func (r *RedisClient) WriteReward(login string, amount int64, percent *big.Rat, immature bool, block *types.BlockData) error {
	if amount <= 0 {
		return nil
	}
	tx := r.client.Multi()
	defer tx.Close()

	addStr := util.Join(amount, percent, immature, block.Hash, block.Height, block.Timestamp)
	remStr := util.Join(amount, percent, !immature, block.Hash, block.Height, block.Timestamp)
	remscore := block.Timestamp - 3600*24*40 // Store the last 40 Days

	_, err := tx.Exec(func() error {
		tx.ZAdd(r.formatKey("rewards", login), redis.Z{Score: float64(block.Timestamp), Member: addStr})
		tx.ZRem(r.formatKey("rewards", login), remStr)
		tx.ZRemRangeByScore(r.formatKey("rewards", login), "-inf", "("+strconv.FormatInt(remscore, 10))

		return nil
	})
	return err
}



func (r *RedisClient) WriteImmatureBlock(block *types.BlockData, roundRewards map[string]int64) error {
	tx := r.client.Multi()
	defer tx.Close()

	exists := false
	if block.RoundHeight != block.Height {
		exist := tx.Exists(r.formatRound(block.RoundHeight, block.Nonce))
		if exist.Val() == true {
			exists = true
		}
	}

	_, err := tx.Exec(func() error {
		r.writeImmatureBlock(tx, block, exists)
		total := int64(0)
		for login, amount := range roundRewards {
			total += amount
			tx.HIncrBy(r.formatKey("miners", login), "immature", amount)
			tx.HSetNX(r.formatKey("credits", "immature", block.Height, block.Hash), login, strconv.FormatInt(amount, 10))
		}
		tx.HIncrBy(r.formatKey("finances"), "immature", total)
		return nil
	})

	if err != nil {
		return err
	}
	return err
}

func (r *RedisClient) WriteMaturedBlock(block *types.BlockData, roundRewards map[string]int64) error {
	creditKey := r.formatKey("credits", "immature", block.RoundHeight, block.Hash)
	tx, err := r.client.Watch(creditKey)
	// Must decrement immatures using existing log entry
	immatureCredits := tx.HGetAllMap(creditKey)
	if err != nil {
		return err
	}
	defer tx.Close()

	ts := util.MakeTimestamp() / 1000
	value := util.Join(block.Hash, ts, block.Reward)

	_, err = tx.Exec(func() error {
		r.writeMaturedBlock(tx, block)
		tx.ZAdd(r.formatKey("credits", "all"), redis.Z{Score: float64(block.Height), Member: value})

		// Decrement immature balances
		totalImmature := int64(0)
		for login, amountString := range immatureCredits.Val() {
			amount, _ := strconv.ParseInt(amountString, 10, 64)
			totalImmature += amount
			tx.HIncrBy(r.formatKey("miners", login), "immature", (amount * -1))
		}

		// Increment balances
		total := int64(0)
		for login, amount := range roundRewards {
			total += amount
			// NOTICE: Maybe expire round reward entry in 604800 (a week)?
			tx.HIncrBy(r.formatKey("miners", login), "balance", amount)
			tx.HSetNX(r.formatKey("credits", block.Height, block.Hash), login, strconv.FormatInt(amount, 10))
		}
		tx.Del(creditKey)
		tx.HIncrBy(r.formatKey("finances"), "balance", total)
		tx.HIncrBy(r.formatKey("finances"), "immature", (totalImmature * -1))
		tx.HSet(r.formatKey("finances"), "lastCreditHeight", strconv.FormatInt(block.Height, 10))
		tx.HSet(r.formatKey("finances"), "lastCreditHash", block.Hash)
		tx.HIncrBy(r.formatKey("finances"), "totalMined", block.RewardInShannon())
		tx.Expire(r.formatKey("credits", block.Height, block.Hash), 604800*time.Second)
		return nil
	})
	return err
}

func (r *RedisClient) WriteOrphan(block *types.BlockData) error {
	creditKey := r.formatKey("credits", "immature", block.RoundHeight, block.Hash)
	tx, err := r.client.Watch(creditKey)
	// Must decrement immatures using existing log entry
	immatureCredits := tx.HGetAllMap(creditKey)
	if err != nil {
		return err
	}
	defer tx.Close()

	_, err = tx.Exec(func() error {
		r.writeMaturedBlock(tx, block)

		// Decrement immature balances
		totalImmature := int64(0)
		for login, amountString := range immatureCredits.Val() {
			amount, _ := strconv.ParseInt(amountString, 10, 64)
			totalImmature += amount
			tx.HIncrBy(r.formatKey("miners", login), "immature", (amount * -1))
		}
		tx.Del(creditKey)
		tx.HIncrBy(r.formatKey("finances"), "immature", (totalImmature * -1))
		return nil
	})
	return err
}

func (r *RedisClient) WritePendingOrphans(blocks []*types.BlockData) error {
	tx := r.client.Multi()
	defer tx.Close()

	_, err := tx.Exec(func() error {
		for _, block := range blocks {
			exists := false
			exist := tx.Exists(r.formatRound(block.RoundHeight, block.Nonce))
			if exist.Val() == true {
				exists = true
			}
			r.writeImmatureBlock(tx, block, exists)
		}
		return nil
	})
	return err
}

func (r *RedisClient) writeImmatureBlock(tx *redis.Multi, block *types.BlockData, exists bool) {
	// Redis 2.8.x returns "ERR source and destination objects are the same"
	if exists && block.Height != block.RoundHeight {
		tx.Rename(r.formatRound(block.RoundHeight, block.Nonce), r.formatRound(block.Height, block.Nonce))
	}

	tx.ZRem(r.formatKey("blocks", "candidates"), block.CandidateKey)
	tx.ZAdd(r.formatKey("blocks", "immature"), redis.Z{Score: float64(block.Height), Member: block.Key()})
}

func (r *RedisClient) writeMaturedBlock(tx *redis.Multi, block *types.BlockData) {
	tx.Del(r.formatRound(block.RoundHeight, block.Nonce))
	tx.ZRem(r.formatKey("blocks", "immature"), block.ImmatureKey)
	tx.ZAdd(r.formatKey("blocks", "matured"), redis.Z{Score: float64(block.Height), Member: block.Key()})
}

func (r *RedisClient) IsMinerExists(login string) (bool, error) {
	return r.client.Exists(r.formatKey("miners", login)).Result()
}

func (r *RedisClient) GetMinerStats(login string, maxPayments int64) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	tx := r.client.Multi()
	defer tx.Close()

	cmds, err := tx.Exec(func() error {
		//tx.HGetAllMap(r.formatKey("miners", login))
		//tx.ZRevRangeWithScores(r.formatKey("payments", login), 0, maxPayments-1)
		//tx.HGet(r.formatKey("paymentsTotal"), login)
		//tx.HGet(r.formatKey("shares", "currentShares"), login)
		tx.LRange(r.formatKey("lastshares"), 0, r.pplns)
		//tx.ZRevRangeWithScores(r.formatKey("rewards", login), 0, 39)
		//tx.ZRevRangeWithScores(r.formatKey("rewards", login), 0, -1)
		return nil
	})

	if err != nil && err != redis.Nil {
		return nil, err
	} else {
		//result, _ := cmds[0].(*redis.StringStringMapCmd).Result()
		//stats["stats"] = convertStringMap(result)
		//payments := convertPaymentsResults(cmds[1].(*redis.ZSliceCmd))
		//stats["payments"] = payments
		//stats["paymentsTotal"], _ = cmds[2].(*redis.StringCmd).Int64()
		stats, _ = r.mysql.GetMinerStats(login, maxPayments)
		shares := cmds[0].(*redis.StringSliceCmd).Val()
		csh := 0
		for _, val := range shares {
			if val == login {
				csh++
			}
		}
		stats["roundShares"] = csh
	}

	return stats, nil
}

// Try to convert all numeric strings to int64
func convertStringMap(m map[string]string) map[string]interface{} {
	result := make(map[string]interface{})
	var err error
	for k, v := range m {
		result[k], err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			result[k] = v
		}
	}
	return result
}

// WARNING: Must run it periodically to flush out of window hashrate entries
func (r *RedisClient) FlushStaleStats(window, largeWindow time.Duration) (int64, error) {
	now := util.MakeTimestamp() / 1000
	max := fmt.Sprint("(", now-int64(window/time.Second))
	total, err := r.client.ZRemRangeByScore(r.formatKey("hashrate"), "-inf", max).Result()
	if err != nil {
		return total, err
	}

	var c int64
	miners := make(map[string]struct{})
	max = fmt.Sprint("(", now-int64(largeWindow/time.Second))

	for {
		var keys []string
		var err error
		c, keys, err = r.client.Scan(c, r.formatKey("hashrate", "*"), 100).Result()
		if err != nil {
			return total, err
		}
		for _, row := range keys {
			login := strings.Split(row, ":")[2]
			if _, ok := miners[login]; !ok {
				n, err := r.client.ZRemRangeByScore(r.formatKey("hashrate", login), "-inf", max).Result()
				if err != nil {
					return total, err
				}
				miners[login] = struct{}{}
				total += n
			}
		}
		if c == 0 {
			break
		}
	}
	return total, nil
}

func (r *RedisClient) CollectStats(smallWindow time.Duration, maxBlocks, maxPayments int64) (map[string]interface{}, error) {
	window := int64(smallWindow / time.Second)
	stats := make(map[string]interface{})

	tx := r.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(r.formatKey("hashrate"), "-inf", fmt.Sprint("(", now-window))
		tx.ZRangeWithScores(r.formatKey("hashrate"), 0, -1)
		tx.HGetAllMap(r.formatKey("stats"))
		//tx.ZRevRangeWithScores(r.formatKey("blocks", "candidates"), 0, -1)
		//tx.ZRevRangeWithScores(r.formatKey("blocks", "immature"), 0, -1)
		//tx.ZRevRangeWithScores(r.formatKey("blocks", "matured"), 0, maxBlocks-1)
		//tx.ZCard(r.formatKey("blocks", "candidates"))
		//tx.ZCard(r.formatKey("blocks", "immature"))
		//tx.ZCard(r.formatKey("blocks", "matured"))
		//tx.HGet(r.formatKey("paymentsTotal"), "all")
		//tx.ZRevRangeWithScores(r.formatKey("payments", "all"), 0, maxPayments-1)
		tx.LLen(r.formatKey("lastshares"))
		return nil
	})

	if (err != nil) && (err != redis.Nil) {
		return nil, err
	}

	candidates, immature, matured, totalMatured, paymentsAll, paymentAllCount, err := r.mysql.CollectStats(maxBlocks)

	result, _ := cmds[2].(*redis.StringStringMapCmd).Result()
	result["nShares"] = strconv.FormatInt(cmds[3].(*redis.IntCmd).Val(), 10)
	stats["stats"] = convertStringMap(result)
	// candidates := convertCandidateResults(cmds[3].(*redis.ZSliceCmd))
	stats["candidates"] = candidates
	stats["candidatesTotal"] = len(candidates)
	// stats["candidatesTotal"] = cmds[6].(*redis.IntCmd).Val()

	// immature := convertBlockResults(cmds[4].(*redis.ZSliceCmd))
	stats["immature"] = immature
	stats["immatureTotal"] = len(immature)
	//stats["immatureTotal"] = cmds[7].(*redis.IntCmd).Val()

	// matured := convertBlockResults(cmds[5].(*redis.ZSliceCmd))
	stats["matured"] = matured
	stats["maturedTotal"] = totalMatured
	// stats["maturedTotal"] = cmds[8].(*redis.IntCmd).Val()

	//payments := convertPaymentsResults(cmds[4].(*redis.ZSliceCmd))
	stats["payments"] = paymentsAll
	stats["paymentsTotal"] = paymentAllCount
	//stats["paymentsTotal"] , _= cmds[3].(*redis.StringCmd).Int64()


	totalHashrate, miners := convertMinersStats(window, cmds[1].(*redis.ZSliceCmd))
	stats["miners"] = miners
	stats["minersTotal"] = len(miners)
	stats["hashrate"] = totalHashrate
	return stats, nil
}

func (r *RedisClient) CollectWorkersStats(sWindow, lWindow time.Duration, login string, mapReportRate map[string]int64) (map[string]interface{}, error) {
	smallWindow := int64(sWindow / time.Second)
	largeWindow := int64(lWindow / time.Second)
	stats := make(map[string]interface{})

	tx := r.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(r.formatKey("hashrate", login), "-inf", fmt.Sprint("(", now-largeWindow))
		tx.ZRangeWithScores(r.formatKey("hashrate", login), 0, -1)
		tx.ZRevRangeWithScores(r.formatKey("rewards", login), 0, 39)
		tx.ZRevRangeWithScores(r.formatKey("rewards", login), 0, -1)
		return nil
	})

	if err != nil {
		return nil, err
	}

	totalHashrate := int64(0)
	currentHashrate := int64(0)
	online := int64(0)
	offline := int64(0)
	workers := convertWorkersStats(smallWindow, cmds[1].(*redis.ZSliceCmd), true)

	for id, worker := range workers {
		timeOnline := now - worker.startedAt
		if timeOnline < 600 {
			timeOnline = 600
		}

		boundary := timeOnline
		if timeOnline >= smallWindow {
			boundary = smallWindow
		}
		worker.HR = worker.HR / boundary

		boundary = timeOnline
		if timeOnline >= largeWindow {
			boundary = largeWindow
		}
		worker.TotalHR = worker.TotalHR / boundary

		if worker.LastBeat < (now - smallWindow/2) {
			worker.Offline = true
			offline++
		} else {
			online++
		}

		currentHashrate += worker.HR
		totalHashrate += worker.TotalHR
		if mapReportRate != nil {
			if reported , ok := mapReportRate[id]; ok {
				worker.Reported = reported
			}
		}
		workers[id] = worker
	}
	stats["workers"] = workers
	stats["workersTotal"] = len(workers)
	stats["workersOnline"] = online
	stats["workersOffline"] = offline
	stats["hashrate"] = totalHashrate
	stats["currentHashrate"] = currentHashrate

	stats["rewards"], _ = r.mysql.GetChartRewardList(login, 40)

	//stats["rewards"] = convertRewardResults(cmds[2].(*redis.ZSliceCmd)) // last 40
	rewards := convertRewardResults(cmds[3].(*redis.ZSliceCmd))         // all

	var dorew []*SumRewardData
	dorew = append(dorew, &SumRewardData{Name: "Last 60 minutes", Interval: 3600, Offset: 0})
	dorew = append(dorew, &SumRewardData{Name: "Last 12 hours", Interval: 3600 * 12, Offset: 0})
	dorew = append(dorew, &SumRewardData{Name: "Last 24 hours", Interval: 3600 * 24, Offset: 0})
	dorew = append(dorew, &SumRewardData{Name: "Last 7 days", Interval: 3600 * 24 * 7, Offset: 0})
	dorew = append(dorew, &SumRewardData{Name: "Last 30 days", Interval: 3600 * 24 * 30, Offset: 0})

	for _, reward := range rewards {

		for _, dore := range dorew {
			dore.Reward += 0
			if reward.Timestamp > now-dore.Interval {
				dore.Reward += reward.Reward
			}
		}
	}
	stats["sumrewards"] = dorew
	stats["24hreward"] = dorew[2].Reward
	return stats, nil
}


func (r *RedisClient) CollectWorkersStatsEx(sWindow, lWindow time.Duration, login string) (int64, int64, int64, int64, ) {
	smallWindow := int64(sWindow / time.Second)
	largeWindow := int64(lWindow / time.Second)

	tx := r.client.Multi()
	defer tx.Close()

	now := util.MakeTimestamp() / 1000

	cmds, err := tx.Exec(func() error {
		tx.ZRemRangeByScore(r.formatKey("hashrate", login), "-inf", fmt.Sprint("(", now-largeWindow))
		tx.ZRangeWithScores(r.formatKey("hashrate", login), 0, -1)
		return nil
	})

	if err != nil {
		return 0, 0, 0, 0
	}

	totalHashrate := int64(0)
	currentHashrate := int64(0)
	online := int64(0)
	offline := int64(0)
	workers := convertWorkersStats(smallWindow, cmds[1].(*redis.ZSliceCmd), false)

	for id, worker := range workers {
		timeOnline := now - worker.startedAt
		if timeOnline < 600 {
			timeOnline = 600
		}

		boundary := timeOnline
		if timeOnline >= smallWindow {
			boundary = smallWindow
		}
		worker.HR = worker.HR / boundary

		boundary = timeOnline
		if timeOnline >= largeWindow {
			boundary = largeWindow
		}
		worker.TotalHR = worker.TotalHR / boundary

		if worker.LastBeat < (now - smallWindow/2) {
			worker.Offline = true
			offline++
		} else {
			online++
		}

		currentHashrate += worker.HR
		totalHashrate += worker.TotalHR
		workers[id] = worker
	}

	return online, offline, totalHashrate , currentHashrate
}

func (r *RedisClient) CollectLuckStats(windows []int) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	tx := r.client.Multi()
	defer tx.Close()

	max := int64(windows[len(windows)-1])

	//cmds, err := tx.Exec(func() error {
	//	tx.ZRevRangeWithScores(r.formatKey("blocks", "immature"), 0, -1)
	//	tx.ZRevRangeWithScores(r.formatKey("blocks", "matured"), 0, max-1)
	//	return nil
	//})
	//if err != nil {
	//	return stats, err
	//}
	//blocks := convertBlockResults(cmds[0].(*redis.ZSliceCmd), cmds[1].(*redis.ZSliceCmd))

	blocks, err := r.mysql.CollectLuckStats(max)
	if err != nil {
		return stats, err
	}
	//log.Printf("%v %v",len(blocks),len(blocks2))

	//r.mysql.GetPayees("500000")
	calcLuck := func(max int) (int, float64, float64, float64) {
		var total int
		var sharesDiff, uncles, orphans float64
		for i, block := range blocks {
			if i > (max - 1) {
				break
			}
			if block.Uncle {
				uncles++
			}
			if block.Orphan {
				orphans++
			}
			sharesDiff += float64(block.TotalShares) / float64(block.Difficulty)
			total++
		}
		if total > 0 {
			sharesDiff /= float64(total)
			uncles /= float64(total)
			orphans /= float64(total)
		}
		return total, sharesDiff, uncles, orphans
	}
	for _, max := range windows {
		total, sharesDiff, uncleRate, orphanRate := calcLuck(max)
		row := map[string]float64{
			"luck": sharesDiff, "uncleRate": uncleRate, "orphanRate": orphanRate,
		}
		stats[strconv.Itoa(total)] = row
		if total < max {
			break
		}
	}
	return stats, nil
}

func convertCandidateResults(raw *redis.ZSliceCmd) []*types.BlockData {
	var result []*types.BlockData
	for _, v := range raw.Val() {
		// "nonce:powHash:mixDigest:timestamp:diff:totalShares"
		block := types.BlockData{}
		block.Height = int64(v.Score)
		block.RoundHeight = block.Height
		fields := strings.Split(v.Member.(string), ":")
		block.Nonce = fields[0]
		block.PowHash = fields[1]
		block.MixDigest = fields[2]
		block.Timestamp, _ = strconv.ParseInt(fields[3], 10, 64)
		block.Difficulty, _ = strconv.ParseInt(fields[4], 10, 64)
		block.TotalShares, _ = strconv.ParseInt(fields[5], 10, 64)
		block.CandidateKey = v.Member.(string)
		result = append(result, &block)
	}
	return result
}


func convertRewardResults(rows ...*redis.ZSliceCmd) []*types.RewardData {
	var result []*types.RewardData
	for _, row := range rows {
		for _, v := range row.Val() {
			// "amount:percent:immature:block.Hash:block.height"
			reward := types.RewardData{}
			reward.Timestamp = int64(v.Score)
			fields := strings.Split(v.Member.(string), ":")
			//block.UncleHeight, _ = strconv.ParseInt(fields[0], 10, 64)
			reward.BlockHash = fields[3]
			reward.Reward, _ = strconv.ParseInt(fields[0], 10, 64)
			reward.Percent, _ = strconv.ParseFloat(fields[1], 64)
			reward.Immature, _ = strconv.ParseBool(fields[2])
			reward.Height, _ = strconv.ParseInt(fields[4], 10, 64)
			result = append(result, &reward)
		}
	}
	return result
}

func convertBlockResults(rows ...*redis.ZSliceCmd) []*types.BlockData {
	var result []*types.BlockData
	for _, row := range rows {
		for _, v := range row.Val() {
			// "uncleHeight:orphan:nonce:blockHash:timestamp:diff:totalShares:rewardInWei"
			block := types.BlockData{}
			block.Height = int64(v.Score)
			block.RoundHeight = block.Height
			fields := strings.Split(v.Member.(string), ":")
			block.UncleHeight, _ = strconv.ParseInt(fields[0], 10, 64)
			block.Uncle = block.UncleHeight > 0
			block.Orphan, _ = strconv.ParseBool(fields[1])
			block.Nonce = fields[2]
			block.Hash = fields[3]
			block.Timestamp, _ = strconv.ParseInt(fields[4], 10, 64)
			block.Difficulty, _ = strconv.ParseInt(fields[5], 10, 64)
			block.TotalShares, _ = strconv.ParseInt(fields[6], 10, 64)
			block.RewardString = fields[7]
			block.ImmatureReward = fields[7]
			block.ImmatureKey = v.Member.(string)
			result = append(result, &block)
		}
	}
	return result
}

// Build per login workers's total shares map {'rig-1': 12345, 'rig-2': 6789, ...}
// TS => diff, id, ms
func convertWorkersStats(window int64, raw *redis.ZSliceCmd, divFlag bool) map[string]Worker {
	now := util.MakeTimestamp() / 1000
	workers := make(map[string]Worker)

	for _, v := range raw.Val() {
		// diff, id, loginCnt, ms, diff, hostname
		parts := strings.Split(v.Member.(string), ":")
		share, _ := strconv.ParseInt(parts[0], 10, 64)

		var hostname string
		if len(parts) > 3 {
			hostname = parts[5]
		} else {
			hostname = "unknown"
		}

		id := parts[1]
		score := int64(v.Score)
		worker := workers[id]

		worker.Size, _ = strconv.ParseInt(parts[2], 10, 64)
		if worker.Size < 1 { worker.Size=1 }
		// Add for large window
		if divFlag == true  {
			worker.TotalHR += share / worker.Size
			worker.WorkerDiff = share / worker.Size
		} else {
			worker.TotalHR += share
			// Addition from Mohannad Otaibi to report Difficulty
			worker.WorkerDiff = share
		}

		worker.WorkerHostname = hostname

		// End Mohannad Adjustments

		// Add for small window if matches
		if score >= now-window {
			if divFlag == true  {
				worker.HR += share / worker.Size
			} else {
				worker.HR += share
			}
		}

		if worker.LastBeat < score {
			worker.LastBeat = score
		}
		if worker.startedAt > score || worker.startedAt == 0 {
			worker.startedAt = score
		}


		workers[id] = worker
	}
	return workers
}

func convertMinersStats(window int64, raw *redis.ZSliceCmd) (int64, map[string]Miner) {
	now := util.MakeTimestamp() / 1000
	miners := make(map[string]Miner)
	totalHashrate := int64(0)

	for _, v := range raw.Val() {
		parts := strings.Split(v.Member.(string), ":")
		share, _ := strconv.ParseInt(parts[0], 10, 64)
		id := parts[1]
		score := int64(v.Score)
		miner := miners[id]
		miner.HR += share

		if miner.LastBeat < score {
			miner.LastBeat = score
		}
		if miner.startedAt > score || miner.startedAt == 0 {
			miner.startedAt = score
		}
		miners[id] = miner
	}

	for id, miner := range miners {
		timeOnline := now - miner.startedAt
		if timeOnline < 600 {
			timeOnline = 600
		}

		boundary := timeOnline
		if timeOnline >= window {
			boundary = window
		}
		miner.HR = miner.HR / boundary

		if miner.LastBeat < (now - window/2) {
			miner.Offline = true
		}
		totalHashrate += miner.HR
		miners[id] = miner
	}
	return totalHashrate, miners
}

func convertPaymentsResults(raw *redis.ZSliceCmd) []map[string]interface{} {
	var result []map[string]interface{}
	for _, v := range raw.Val() {
		tx := make(map[string]interface{})
		tx["timestamp"] = int64(v.Score)
		fields := strings.Split(v.Member.(string), ":")
		tx["tx"] = fields[0]
		// Individual or whole payments row
		if len(fields) < 3 {
			tx["amount"], _ = strconv.ParseInt(fields[1], 10, 64)
		} else {
			tx["address"] = fields[1]
			tx["amount"], _ = strconv.ParseInt(fields[2], 10, 64)
		}
		result = append(result, tx)
	}
	return result
}



/*
Timestamp  int64  `json:"x"`
TimeFormat string `json:"timeFormat"`
Amount     int64  `json:"amount"`
*/
func convertPaymentChartsResults(raw *redis.ZSliceCmd) []*PaymentCharts {
	var result []*PaymentCharts
	for _, v := range raw.Val() {
		pc := PaymentCharts{}
		pc.Timestamp = int64(v.Score)
		tm := time.Unix(pc.Timestamp, 0)
		pc.TimeFormat = tm.Format("2006-01-02") + " 00_00"
		fields := strings.Split(v.Member.(string), ":")
		pc.Amount, _ = strconv.ParseInt(fields[1], 10, 64)
		//fmt.Printf("%d : %s : %d \n", pc.Timestamp, pc.TimeFormat, pc.Amount)

		var chkAppend bool
		for _, pcc := range result {
			if pcc.TimeFormat == pc.TimeFormat {
				pcc.Amount += pc.Amount
				chkAppend = true
			}
		}
		if !chkAppend {
			pc.Timestamp -= int64(math.Mod(float64(v.Score), float64(86400)))
			result = append(result, &pc)
		}
	}
	return result
}

func (r *RedisClient) GetCurrentHashrate(login string) (int64, error) {
	hashrate := r.client.HGet(r.formatKey("currenthashrate", login), "hashrate")
	if hashrate.Err() == redis.Nil {
		return 0, nil
	} else if hashrate.Err() != nil {
		return 0, hashrate.Err()
	}
	return hashrate.Int64()
}

func (r *RedisClient) IsRoundNumber(roundHeight int64, nonce string) (bool, error) {
	return r.client.Exists(r.formatRound(roundHeight, nonce)).Result()
}

func (r *RedisClient) DeleteRoundBlock(roundHeight int64, nonce string) *redis.IntCmd {
	return r.client.Del(r.formatRound(roundHeight, nonce))
}

func (r *RedisClient) SetDB(db IMysqlDB) {
	r.mysql = db
}

func (r *RedisClient) GetReportedtHashrate(login string) (map[string]int64, error) {
	var result map[string]int64
	reportedRate := r.client.HGetAllMap(r.formatKey("report", login))
	if reportedRate.Err() == redis.Nil {
		return nil, nil
	} else if reportedRate.Err() != nil {
		return nil, reportedRate.Err()
	}

	now := util.MakeTimestamp() / 1000
	reportedMap, _ := reportedRate.Result()
	for workerId, rateStr := range reportedMap {
		val := strings.Split(rateStr,":")
		rate, _ := strconv.ParseInt(val[0], 10, 64)
		ts, _ := strconv.ParseInt(val[1], 10, 64)

		if ts + 600 > now {
			if result == nil { result = make(map[string]int64) }
			result[workerId] = rate
		}
	}
	return result, nil
}

func (r *RedisClient) GetAllReportedtHashrate(login string) (int64, error) {
	reportedRate := r.client.HGetAllMap(r.formatKey("report", login))
	if reportedRate.Err() == redis.Nil {
		return -1, nil
	} else if reportedRate.Err() != nil {
		return 0, reportedRate.Err()
	}

	var result int64
	now := util.MakeTimestamp() / 1000

	reportedMap, _ := reportedRate.Result()
	for _, rateStr := range reportedMap {
		val := strings.Split(rateStr,":")
		rate, _ := strconv.ParseInt(val[0], 10, 64)
		ts, _ := strconv.ParseInt(val[1], 10, 64)
		size, _ := strconv.ParseInt(val[2], 10, 64)

		if ts + 600 > now {
			result += rate * size
		}
	}
	return result, nil
}

func (r *RedisClient) SetReportedtHashrates(logins map[string]string, WorkerId string) error {
	tx := r.client.Multi()
	defer tx.Close()

	_, err := tx.Exec(func() error {
		for login, rateStr := range logins {
			r.client.HSet(r.formatKey("report", login), WorkerId, rateStr)
		}
		return nil
	})

	if err != nil {
		return err
	}
	return nil
}

