package mysql

import (
	"database/sql"
	"fmt"
	"github.com/cellcrypto/open-dangnn-pool/storage/redis"
	"github.com/cellcrypto/open-dangnn-pool/storage/types"
	"github.com/cellcrypto/open-dangnn-pool/util"
	"github.com/cellcrypto/open-dangnn-pool/util/plogger"
	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common/math"
	_ "github.com/go-sql-driver/mysql"
	"log"
	"math/big"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Endpoint string `json:"endpoint"`
	UserName string `json:"user"`
	Password string `json:"password"`
	Database string  `json:"database"`
	Port	 int	`json:"port"`
	PoolSize int    `json:"poolSize"`

	Coin 	string  `json:"coin"`
	Threshold int64 `json:"threshold"`
	LogTableName string `json:"logTableName"`
}

type Database struct {
	Conn *sql.DB
	Redis *redis.RedisClient

	Config *Config
	DiffByShareValue int64
}

type Payees struct {
	Coin string
	Addr string
	Balance int64
	Payout_limit int64
}

type MinerChartSelect struct {
	Coin			string
	Addr 			string
	Share			int
	ShareCheckTime 	int64
}

type LogEntrie struct {
	Entries string
	Addr string
}

const (
	constImmaturedBlockErr = -2
	constCandidatesBlockErr = -1
	constCandidatesBlock = 0
	constImmatureBlock = 1
	constPeddingImmaturedBlock = 2
	constOrphanBlock=3
	constMatureBlock = 4
)

type ImmaturedState string
const (
	eMaturedBlock = ImmaturedState("MaturedBlock")
	eOrphanBlock  = ImmaturedState("OrphanBlock")
	eLostBlock		= ImmaturedState("LostBlock")
)

const constInsertCountSqlMax = 2000


func New(cfg *Config, proxyDiff int64,redis *redis.RedisClient) (*Database, error) {

	url := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s",
		cfg.UserName, cfg.Password, cfg.Endpoint, cfg.Port, cfg.Database)
	conn, err := sql.Open("mysql", url)
	if err != nil {
		println(err)
		return nil, err
	}

	db := &Database{
		Conn:       conn,
		Config : cfg,
		Redis: redis,
		DiffByShareValue: proxyDiff,
	}

	conn.SetMaxIdleConns(50)
	conn.SetMaxOpenConns(50)

	err = conn.Ping()
	if err != nil {
		return nil, err
	}

	return db, nil
}


func (d *Database) InsertSqlLog(sql *string) {
	conn := d.Conn

	_, err := conn.Exec(*sql)
	if err != nil {
		log.Fatal(err)
	}
	return
}


func (d *Database) WriteBlock(login, id string, params []string, diff, roundDiff int64, height uint64, window time.Duration, hostname string)  {
	conn := d.Conn

	diffTimes := int(diff / d.DiffByShareValue)
	if diffTimes > 1 {
		diffTimes = 1	// fixed to 1
	}
	nowTime := time.Now()

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(
		"INSERT INTO miner_info(`coin`,`login_addr`,`diff_times`,`blocks_found`,`hostname`,`share`,`last_share`) VALUES (?,?,?,?,?,?,?) ON DUPLICATE KEY UPDATE diff_times=diff_times+VALUES(diff_times),blocks_found=blocks_found+1,hostname=VALUES(hostname),share=share+VALUES(share),last_share=VALUES(last_share)",
		d.Config.Coin,login,diffTimes,1,hostname,diffTimes,nowTime)
	if err != nil {
		log.Println(d.Config.Coin,login,diffTimes,1,hostname,diffTimes,nowTime)
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}
}

func (d *Database) WriteShare(login, id string, params []string, diff int64, height uint64, window time.Duration, hostname string) error {
	conn := d.Conn
	diffTimes := int(diff / d.DiffByShareValue)
	if diffTimes > 1 {
		diffTimes = 1	// fixed to 1
	}

	nowTime := time.Now()

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(
		"INSERT INTO miner_info(`coin`,`login_addr`,`diff_times`,`hostname`,`share`,`last_share`) VALUES (?,?,?,?,?,?)  ON DUPLICATE KEY UPDATE diff_times=diff_times+VALUES(diff_times),hostname=VALUES(hostname),share=share+VALUES(share),last_share=VALUES(last_share)",
		d.Config.Coin,login,diffTimes,hostname,diffTimes,nowTime)
	if err != nil {
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	return nil
}


func (d *Database) WriteCandidates(height uint64, params []string, nowTime string,ts int64, roundDiff int64, totalShares int64)  {
	conn := d.Conn

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	_, err = tx.Exec(
		"INSERT INTO blocks(`state`, `coin`,`round_height`,`nonce`,`height`,`hash_no_nonce`,`mix_digest`,`round_diff`,`total_share`,`timestamp`,`insert_time`) VALUES (?,?,?,?,?,?,?,?,?,?,?)",
		constCandidatesBlock, d.Config.Coin, height, params[0], height, params[1], params[2], roundDiff, totalShares, ts, nowTime)
	if err != nil {
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}
}


func (d *Database) GetCandidates(maxHeight int64) ([]*types.BlockData, error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT round_height,nonce,hash_no_nonce,mix_digest,round_diff,total_share,insert_time,`timestamp` FROM blocks WHERE state=0 AND coin=? AND round_height < ?", d.Config.Coin, maxHeight)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []*types.BlockData
	for rows.Next() {
		var (
			height                         int64
			nonce,hashNoNonce, mixDigest string
			roundDiff, totalShare       int64
			insertTime                  string
			timestamp					int64
		)

		err := rows.Scan(&height,&nonce,&hashNoNonce,&mixDigest,&roundDiff,&totalShare,&insertTime,&timestamp)
		if err != nil {
			log.Printf("mysql GetCandidates:rows.Scan() error: %v",err)
			return nil, err
		}

		block := types.BlockData{}
		block.Height = height
		block.RoundHeight = height
		block.Nonce = nonce
		block.PowHash = hashNoNonce
		block.MixDigest = mixDigest
		block.Timestamp = timestamp
		block.Difficulty = roundDiff
		block.TotalShares = totalShare
		//block.candidateKey = v.Member.(string)
		result = append(result, &block)
	}

	return result, nil
}

func (d *Database) WritePendingOrphans(blocks []*types.BlockData) error {
	r := d.Redis

	for _, block := range blocks {
		exist, err := r.IsRoundNumber(block.RoundHeight, block.Nonce)
		if err != nil {
			plogger.InsertLog("WritePendingOrphans():Failed IsRoundNumber Error: " + err.Error(), plogger.LogTypePendingBlock, plogger.LogErrorNothingRoundBlock, block.RoundHeight, block.Height, "", "")
		 	return err
		}

		if !exist {
			plogger.InsertLog(fmt.Sprintf("WritePendingOrphans:IsRoundNumber not exist. block.RoundHeight: %v, block.Nonce:%v", block.RoundHeight, block.Nonce), plogger.LogTypePendingBlock, plogger.LogErrorNothingRoundBlock, block.RoundHeight, block.Height, "", "")
			continue
		}

		err = d.writePendingOrphans(block)
		if err != nil {
			return err
		}
	}

	return nil
}

func (d *Database) writePendingOrphans(block *types.BlockData) error {
	// height,
	// b.UncleHeight, b.Orphan, b.Nonce, b.serializeHash(), b.Timestamp, b.Difficulty, b.TotalShares, b.Reward

	conn := d.Conn

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	ret, err := tx.Exec("UPDATE blocks SET `state`=?,`height`=?,`uncle_height`=?,`orphan`=?,`hash`=?,`timestamp`=?,`diff`=?,`reward`=? WHERE state=0 AND round_height=? AND nonce=? AND coin=?",
		constPeddingImmaturedBlock, block.Height,block.UncleHeight, block.Orphan, block.SerializeHash(), block.Timestamp, block.Difficulty, block.Reward.String(), block.RoundHeight, block.Nonce, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}

	if ok,_ := ret.RowsAffected(); ok <= 0  {
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func (d *Database) WriteImmatureError(block *types.BlockData, blockState int, errNum int) error {
	conn := d.Conn

	errState := 0
	switch errNum {
	case 1: errState = constCandidatesBlockErr
	case 2: errState = constImmaturedBlockErr
	}

	_, err := conn.Exec("UPDATE blocks SET `state`=? WHERE state=? AND round_height=? AND nonce=? and coin=?", errState, blockState, block.RoundHeight, block.Nonce, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}

	// NO NEED
	//if errNum == 2 {
	//	// There is no round+block information of Redis during compensation block check.
	//	// Think of it as a lost block.
	//	immatureCredits, _:= d.selectCreditsImmature(block.RoundHeight,block.Hash)
	//
	//	if len(immatureCredits) > 0 {
	//		d.calcuCreditsImmature(block, immatureCredits,eLostBlock)
	//	}
	//}

	return err
}

func (d *Database) WriteImmatureBlock(block *types.BlockData, roundRewards map[string]int64, percents map[string]*big.Rat) error {
	r := d.Redis

	exist, err := r.IsRoundNumber(block.RoundHeight, block.Nonce)
	if err != nil {
		plogger.InsertLog("writeImmatureBlock():Failed IsRoundNumber Error: " + err.Error(), plogger.LogTypePendingBlock, plogger.LogErrorNothingRoundBlock, block.RoundHeight, block.Height, "", "")
		return err
	}
	if !exist {
		plogger.InsertLog(fmt.Sprintf("WriteImmatureBlock:IsRoundNumber not exist. block.RoundHeight: %v, block.Nonce:%v", block.RoundHeight, block.Nonce), plogger.LogTypePendingBlock, plogger.LogErrorNothingRoundBlock, block.RoundHeight, block.Height, "", "")
		//return err
	}

	// Change the block to immaturedBlock.
	err = d.writeImmatureBlock(block)
	if err != nil {
		plogger.InsertLog("writeImmatureBlock():Failed to change immatured block." + err.Error(), plogger.LogTypePendingBlock, plogger.LogErrorNothingRoundBlock, block.RoundHeight, block.Height, "", "")
		return err
	}

	// Write the reward in the DB. miner_info,credits
	total, err := d.writeImmatureReward(block, roundRewards, percents)
	if err != nil {
		plogger.InsertLog("writeImmatureReward():Failed to enter immatured reward." + err.Error(), plogger.LogTypePendingBlock, plogger.LogErrorNothingRoundBlock, block.RoundHeight, block.Height, "", "")
		return err
	}
	// complete (finaces)
	err = d.writeFinances(total)

	return err
}

func (d *Database) writeFinances(total int64) error {
	conn := d.Conn
	_, err := conn.Exec("INSERT INTO finances(`coin`, `immature`) VALUES (?,?) ON DUPLICATE KEY UPDATE immature=immature+VALUES(immature)", d.Config.Coin, total)
	if err != nil {
		return err
	}
	return nil
}

func (d *Database) writeImmatureReward(block *types.BlockData, roundRewards map[string]int64, percents map[string]*big.Rat) (int64, error) {
	total := int64(0)
	count := int64(0)
	var (
		insertCnt			int64 = 0
		minerRewardSql		strings.Builder
		creditsRewardSql	strings.Builder
		blocksInfoSql		string
	)

	var logEntries []LogEntrie
	for login, amount := range roundRewards {
		total += amount
		count++

		per := new(big.Rat)
		if val, ok := percents[login]; ok {
			per = val
		}

		if insertCnt == 0 {
			minerRewardSql.Reset()
			creditsRewardSql.Reset()
			minerRewardSql.WriteString( fmt.Sprintf("INSERT INTO miner_info(`coin`, `login_addr`, `immature`) VALUES (\"%v\",\"%v\",\"%v\")", d.Config.Coin, login, amount) )
			creditsRewardSql.WriteString( fmt.Sprintf("INSERT INTO credits_immature(`coin`, `round_height`, `height`, `hash`, `login_addr`, `amount`, `percent`, `timestamp`) VALUES (\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\")", d.Config.Coin, block.RoundHeight, block.Height, block.Hash, login, strconv.FormatInt(amount, 10), per.FloatString(9), block.Timestamp) )

			logEntries = make([]LogEntrie,1)
			logEntries[0].Addr = login
			logEntries[0].Entries = fmt.Sprintf("IMMATURE REWARD+ %v: %v: %v Shannon", block.RoundKey(), login, amount)
		} else {
			minerRewardSql.WriteString( fmt.Sprintf(",(\"%v\",\"%v\",\"%v\")", d.Config.Coin, login, amount) )
			creditsRewardSql.WriteString( fmt.Sprintf(",(\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\")", d.Config.Coin, block.RoundHeight, block.Height, block.Hash, login, strconv.FormatInt(amount, 10), per.FloatString(9), block.Timestamp) )

			newLog := LogEntrie{
				Entries: fmt.Sprintf("IMMATURE REWARD+ %v: %v: %v Shannon", block.RoundKey(), login, amount),
				Addr:    login,
			}
			logEntries = append(logEntries, newLog)
		}
		insertCnt++

		if insertCnt > constInsertCountSqlMax {
			minerRewardSql.WriteString( fmt.Sprintf(" ON DUPLICATE KEY UPDATE immature=immature+VALUES(immature)") )
			blocksInfoSql = fmt.Sprintf("UPDATE blocks SET total_immatured_cnt=%v, total_immatured=%v WHERE state=%v AND round_height=%v AND nonce=\"%v\" AND coin=\"%v\"", count, total, constImmatureBlock, block.RoundHeight, block.Nonce, d.Config.Coin)
			err := d.insertImmaturedBlock(minerRewardSql.String(), creditsRewardSql.String(), blocksInfoSql)
			if err != nil {
				return total - insertCnt, err
			}
			insertCnt = 0

			for _, logEntrie := range logEntries {
				plogger.InsertLog(logEntrie.Entries, plogger.LogTypePendingBlock, plogger.LogErrorNothing, block.RoundHeight, block.Height, logEntrie.Addr, "")
			}

		}
	}

	if insertCnt > 0 {
		minerRewardSql.WriteString( fmt.Sprintf(" ON DUPLICATE KEY UPDATE immature=immature+VALUES(immature)") )
		blocksInfoSql = fmt.Sprintf("UPDATE blocks SET total_immatured_cnt=%v, total_immatured=%v WHERE state=%v AND round_height=%v AND nonce=\"%v\" AND coin=\"%v\"", count, total, constImmatureBlock, block.RoundHeight, block.Nonce, d.Config.Coin)
		err := d.insertImmaturedBlock(minerRewardSql.String(), creditsRewardSql.String(), blocksInfoSql)
		if err != nil {
			return total - insertCnt, err
		}
		insertCnt = 0
		for _, logEntrie := range logEntries {
			plogger.InsertLog(logEntrie.Entries, plogger.LogTypePendingBlock, plogger.LogErrorNothing, block.RoundHeight, block.Height, logEntrie.Addr, "")
		}
	}
	return total, nil
}

func (d *Database) writeImmatureBlock(block *types.BlockData) error {
	conn := d.Conn

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	ret, err := tx.Exec(
		"UPDATE blocks SET `state`=?,`height`=?,`uncle_height`=?,`orphan`=?,`hash`=?,`timestamp`=?,`reward`=? WHERE state=0 AND round_height=? AND nonce=? AND coin=?",
		constImmatureBlock, block.Height,block.UncleHeight, block.Orphan, block.SerializeHash(), block.Timestamp, block.Reward.String(), block.RoundHeight, block.Nonce, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}

	if ok, _ := ret.RowsAffected(); ok <= 0 {
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	return err
}

func (d *Database) insertImmaturedBlock(minerRewardSql string, creditsRewardSql string, blocksInfoSql string) error {
	conn := d.Conn

	txRound, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer txRound.Rollback()

	_, err = txRound.Exec(minerRewardSql)
	if err != nil {
		return err
	}

	_, err = txRound.Exec(creditsRewardSql)
	if err != nil {
		return err
	}

	_, err = txRound.Exec(blocksInfoSql)
	if err != nil {
		return err
	}

	err = txRound.Commit()
	if err != nil {
		log.Fatal(err)
	}

	return nil
}


func (d *Database) GetImmatureBlocks(maxHeight int64) ([]*types.BlockData, error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT state,round_height,height,uncle_height,orphan,nonce,hash,`timestamp`,round_diff,total_share,reward FROM blocks WHERE state in (?,?) AND round_height < ? AND coin=?",constImmatureBlock, constPeddingImmaturedBlock, maxHeight, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []*types.BlockData
	for rows.Next() {
		var (
			state int
			height, roundHeight, uncleHeight int64
			nonce,hash                       string
			roundDiff, totalShare       	int64
			timestamp                  		int64
			orphan 							string
			reward				string
		)

		err := rows.Scan(&state, &roundHeight, &height, &uncleHeight, &orphan, &nonce, &hash, &timestamp, &roundDiff, &totalShare, &reward)
		if err != nil {
			log.Printf("mysql GetImmatureBlocks:rows.Scan() error: %v",err)
			return nil, err
		}

		block := d.convertBlockResults(state, height, roundHeight, uncleHeight, orphan, nonce, hash, timestamp, roundDiff, totalShare, reward)
		result = append(result, &block)
	}

	return result, nil
}


func (d *Database) writeOrphans(block *types.BlockData) error {
	conn := d.Conn

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	ret, err := tx.Exec(
		"UPDATE blocks SET `state`=?,`height`=?,`uncle_height`=?,`orphan`=?,`hash`=?,`timestamp`=?,`diff`=?,`reward`=? WHERE state=? AND round_height=? AND nonce=? AND coin=?",
		constOrphanBlock, block.Height,block.UncleHeight, block.Orphan, block.SerializeHash(), block.Timestamp, block.Difficulty, block.Reward, block.State, block.RoundHeight, block.Nonce, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}

	if ok,_ := ret.RowsAffected(); ok <= 0  {
		return err
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	return nil
}

func (d *Database) selectCreditsImmature(roundHeight int64, hash string) ([]*types.CreditsImmatrue,error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT login_addr,amount FROM credits_immature WHERE round_height=? AND hash=? AND coin=?",roundHeight,hash, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []*types.CreditsImmatrue
	for rows.Next() {
		var (
			addr string
			amount int64
		)

		err := rows.Scan(&addr,&amount)
		if err != nil {
			log.Printf("mysql selectCreditsImmature:rows.Scan() error: %v",err)
			return nil, err
		}

		credits := types.CreditsImmatrue{
			Addr:   addr,
			Amount: amount,
		}
		result = append(result, &credits)
	}

	return result, nil
}

func (d *Database) updateCreditsImmature(creditsImmatureSql string, totalImmature int64) error {
	conn := d.Conn
	txRound, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer txRound.Rollback()

	_, err = txRound.Exec(creditsImmatureSql)
	if err != nil {
		return err
	}

	_, err = txRound.Exec("INSERT INTO finances(`coin`, `immature`) VALUES (?,?) ON DUPLICATE KEY UPDATE immature=immature+VALUES(immature)", d.Config.Coin, totalImmature)
	if err != nil {
		return err
	}

	err = txRound.Commit()
	if err != nil {
		log.Fatal(err)
	}
	return nil
}

func (d *Database) WriteOrphan(block *types.BlockData) error {
	immatureCredits, _:= d.selectCreditsImmature(block.RoundHeight,block.Hash)

	err := d.writeOrphans(block)
	if err != nil {
		return err
	}

	// Delete Redis share information.
	d.Redis.DeleteRoundBlock(block.RoundHeight, block.Nonce)

	d.calcuCreditsImmature(block, immatureCredits, eOrphanBlock)

	return nil
}

func (d *Database) calcuCreditsImmature(block *types.BlockData, immatureCredits []*types.CreditsImmatrue, orphan ImmaturedState) {
	conn := d.Conn

	res, err := conn.Exec("DELETE FROM credits_immature WHERE coin=? AND round_height=? AND hash=?", d.Config.Coin, block.RoundHeight, block.Hash)
	if err != nil {
		log.Printf("mysql calcuCreditsImmature:Exec() error: %v", err)
		return
	}
	count, err := res.RowsAffected()
	if err != nil {
		log.Printf("mysql calcuCreditsImmature:RowsAffected() error: %v", err)
		return
	}

	if  count <= 0 {
		fmt.Printf("round height:%d hash:%s\n", block.RoundHeight, block.Hash)
		return
	}

	var (
		updateCnt          int
		creditsImmatureSql strings.Builder
	)

	totalImmature := int64(0)
	var logEntries []LogEntrie
	// Subtract immature compensation information.
	for _, data := range immatureCredits {
		if updateCnt == 0 {
			creditsImmatureSql.Reset()
			creditsImmatureSql.WriteString( fmt.Sprintf("INSERT INTO miner_info(`coin`, `login_addr`, `immature`) VALUES (\"%v\",\"%v\",\"%v\")", d.Config.Coin, data.Addr, data.Amount*-1) )
			totalImmature = data.Amount

			logEntries = make([]LogEntrie, 1)
			logEntries[0].Addr = data.Addr
			logEntries[0].Entries = fmt.Sprintf("IMMATURE(%v)- %v: %v: %v Shannon", orphan, block.RoundKey(), data.Addr, data.Amount)
		} else {
			creditsImmatureSql.WriteString( fmt.Sprintf(",(\"%v\",\"%v\",\"%v\")", d.Config.Coin, data.Addr, data.Amount * -1) )
			totalImmature += data.Amount

			newLog := LogEntrie{
				Entries: fmt.Sprintf("IMMATURE(%v)- %v: %v: %v Shannon", orphan, block.RoundKey(), data.Addr, data.Amount),
				Addr:    data.Addr,
			}
			logEntries = append(logEntries, newLog)
		}
		updateCnt++

		if updateCnt > constInsertCountSqlMax {
			creditsImmatureSql.WriteString( fmt.Sprintf(" ON DUPLICATE KEY UPDATE immature=immature+VALUES(immature)") )
			d.updateCreditsImmature(creditsImmatureSql.String(), totalImmature * -1)
			totalImmature = 0
			updateCnt = 0
		}
	}

	if updateCnt > 0 {
		creditsImmatureSql.WriteString( fmt.Sprintf(" ON DUPLICATE KEY UPDATE immature=immature+VALUES(immature)") )

		d.updateCreditsImmature(creditsImmatureSql.String(), totalImmature * -1)
		updateCnt = 0
	}

	if len(logEntries) > 0 {
		var logSubType int
		switch orphan {
		case eMaturedBlock: logSubType = plogger.LogSubTypeImmaturedBlock
		case eOrphanBlock: logSubType = plogger.LogSubTypeOrphanBlcok
		case eLostBlock: logSubType = plogger.LogSubTypeLostBlcok
		}
		for _, logEntrie := range logEntries {
			plogger.InsertLog(logEntrie.Entries, plogger.LogTypeMaturedBlock, logSubType, block.RoundHeight, block.Height, logEntrie.Addr, "")
		}
	}
}

func (d *Database) makeMaturedBlcokSQL(block *types.BlockData,roundRewards map[string]int64, percents map[string]*big.Rat) (string, string, string){

	var (
		creditsBalanceSql strings.Builder
		minerBalanceSql strings.Builder
		financesSql string
		insertCnt int
	)

	// Increment balances
	total := int64(0)
	if len(roundRewards) > 0 {
		for login, amount := range roundRewards {
			total += amount

			per := new(big.Rat)
			if val, ok := percents[login]; ok {
				per = val
			}

			if insertCnt == 0 {
				creditsBalanceSql.Reset()
				minerBalanceSql.Reset()
				creditsBalanceSql.WriteString(fmt.Sprintf("INSERT INTO credits_balance(coin, round_height, height, hash, login_addr, amount, percent, `timestamp`) VALUES " +
					"(\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\")", d.Config.Coin, block.RoundHeight, block.Height, block.Hash, login, strconv.FormatInt(amount, 10), per.FloatString(9), block.Timestamp))
				minerBalanceSql.WriteString(fmt.Sprintf("INSERT INTO miner_info(coin, login_addr, balance) VALUES (\"%v\",\"%v\",\"%v\")",d.Config.Coin, login, strconv.FormatInt(amount, 10)))
			} else {
				creditsBalanceSql.WriteString(fmt.Sprintf(",(\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\",\"%v\")", d.Config.Coin, block.RoundHeight, block.Height, block.Hash, login, strconv.FormatInt(amount, 10), per.FloatString(9), block.Timestamp))
				minerBalanceSql.WriteString(fmt.Sprintf(",(\"%v\",\"%v\",\"%v\")", d.Config.Coin, login, strconv.FormatInt(amount, 10)))
			}
			insertCnt++
		}

		creditsBalanceSql.WriteString(" ON DUPLICATE KEY UPDATE insert_cnt=insert_cnt+1,amount=VALUES(amount)")
		minerBalanceSql.WriteString(" ON DUPLICATE KEY UPDATE balance=balance+VALUES(balance)")
		financesSql = fmt.Sprintf("UPDATE finances SET balance=balance+%v,last_height=%v,last_hash=\"%v\",total_mined=total_mined+%v WHERE coin=\"%v\"",
							total, strconv.FormatInt(block.Height, 10), block.Hash, block.RewardInShannon(), d.Config.Coin)
	} else {
		financesSql = fmt.Sprintf("UPDATE finances SET last_height=%v,last_hash=\"%v\",total_mined=total_mined+%v WHERE coin=\"%v\"",
			strconv.FormatInt(block.Height, 10), block.Hash, block.RewardInShannon(), d.Config.Coin)
	}

	return creditsBalanceSql.String(), minerBalanceSql.String(), financesSql
}

func (d *Database) writeMaturedBlock(block *types.BlockData, creditsBalanceSql, minerBalanceSql, financesSql string) error {
	conn := d.Conn

	txRound, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer txRound.Rollback()

	_, err = txRound.Exec(creditsBalanceSql)
	if err != nil {
		return err
	}

	_, err = txRound.Exec(minerBalanceSql)
	if err != nil {
		return err
	}

	_, err = txRound.Exec(financesSql)
	if err != nil {
		return err
	}

	// creditsBlockSql = fmt.Sprintf("INSERT INTO IGNORE credits_block(height,hash,reward) VALUES (?,?,?)")
	_, err = txRound.Exec("INSERT IGNORE INTO credits_blocks(height,hash,coin,reward) VALUE (?,?,?,?)",block.Height, block.Hash, d.Config.Coin, block.Reward.String())
	if err != nil {
		return err
	}

	// blocksInfoSql = fmt.Sprintf("UPDATE blocks SET state=? WHERE state=? AND round_height=? AND nonce=?")
	_, err = txRound.Exec("UPDATE blocks SET `state`=?,`height`=?,`uncle_height`=?,`orphan`=?,`hash`=?,`timestamp`=?,`diff`=?, `reward`=? WHERE state=? AND round_height=? AND nonce=? AND coin=?",
		constMatureBlock, block.Height,	block.UncleHeight, block.Orphan, block.SerializeHash(), block.Timestamp, block.Difficulty, block.Reward.String(), block.State, block.RoundHeight, block.Nonce, d.Config.Coin)
	if err != nil {
		return err
	}

	err = txRound.Commit()
	if err != nil {
		log.Fatal(err)
	}

	return nil
}

// WriteMaturedBlock If the reward miner is more than 20,000, you need to increase the query capacity or modify it!!
func (d *Database) WriteMaturedBlock(block *types.BlockData, roundRewards map[string]int64, percents map[string]*big.Rat) error {
	start := time.Now()
	immatureCredits, _:= d.selectCreditsImmature(block.RoundHeight, block.Hash)

	// Let's write a query for the contents to be saved in advance.
	creditsBalanceSql, minerBalanceSql, financesSql := d.makeMaturedBlcokSQL(block, roundRewards, percents)

	// commit to db
	err := d.writeMaturedBlock(block, creditsBalanceSql, minerBalanceSql, financesSql)
	if err != nil {
		return err
	}

	// Delete Redis share information.
	d.Redis.DeleteRoundBlock(block.RoundHeight, block.Nonce)

	d.calcuCreditsImmature(block, immatureCredits, eMaturedBlock)
	log.Printf("!@#!@#!@#! writeMaturedBlock execute time: %s count: %d", time.Since(start), len(roundRewards))
	return nil
}

func (d *Database) CollectStats(maxBlocks int64) ([]*types.BlockData, []*types.BlockData, []*types.BlockData, int, []map[string]interface{}, int64, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT state,round_height,height,uncle_height,orphan,nonce,hash,`timestamp`,round_diff,total_share,reward FROM blocks WHERE state in (?,?) AND coin=? ORDER BY height DESC", constCandidatesBlock, constImmatureBlock, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var (
		resultCandidates []*types.BlockData
		resultImmature []*types.BlockData
		resultMatured []*types.BlockData
		resultMaturedCount				int
	)

	for rows.Next() {
		var (
			state                            int
			height, roundHeight, uncleHeight int64
			nonce, hash                      string
			roundDiff, totalShare            int64
			timestamp                        int64
			orphan                           string
			reward                           string
		)

		err := rows.Scan(&state, &roundHeight, &height, &uncleHeight, &orphan, &nonce, &hash, &timestamp, &roundDiff, &totalShare, &reward)
		if err != nil {
			log.Printf("mysql CollectStats:rows.Scan() error: %v",err)
			return nil, nil, nil, 0, nil, 0, err
		}

		block := d.convertBlockResults(state, height, roundHeight, uncleHeight, orphan, nonce, hash, timestamp, roundDiff, totalShare, reward)
		if block.State == constCandidatesBlock {
			resultCandidates = append(resultCandidates, &block)
		} else {
			resultImmature = append(resultImmature, &block)
		}
	}

	rows2, err := conn.Query("SELECT state,round_height,height,uncle_height,orphan,nonce,hash,`timestamp`,round_diff,total_share,reward FROM blocks WHERE coin=? AND state=? ORDER BY height DESC LIMIT ?", d.Config.Coin, constMatureBlock, maxBlocks)
	if err != nil {
		log.Fatal(err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var (
			state                            int
			height, roundHeight, uncleHeight int64
			nonce, hash                      string
			roundDiff, totalShare            int64
			timestamp                        int64
			orphan                           string
			reward                           string
		)

		err := rows2.Scan(&state, &roundHeight, &height, &uncleHeight, &orphan, &nonce, &hash, &timestamp, &roundDiff, &totalShare, &reward)
		if err != nil {
			log.Printf("mysql CollectStats:rows2.Scan() error: %v", err)
			return nil, nil, nil, 0, nil, 0, err
		}

		block := d.convertBlockResults(state, height, roundHeight, uncleHeight, orphan, nonce, hash, timestamp, roundDiff, totalShare, reward)
		resultMatured = append(resultMatured, &block)
	}

	rows3, err := conn.Query("SELECT count(*) FROM blocks WHERE coin=? AND state=?", d.Config.Coin, constMatureBlock)
	if err != nil {
		log.Fatal(err)
	}
	defer rows3.Close()

	if rows3.Next() {
		err := rows3.Scan(&resultMaturedCount)
		if err != nil {
			log.Printf("mysql CollectStats:rows3.Scan() error: %v", err)
			return nil, nil, nil, 0,  nil, 0, err
		}
	}

	resultPayment, paymentCount, _ := d.GetAllPayments(maxBlocks)

	return resultCandidates, resultImmature, resultMatured, resultMaturedCount, resultPayment, paymentCount, nil
}

func (d *Database) CollectLuckStats(windowMax int64) ([]*types.BlockData,error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT state,round_height,height,uncle_height,orphan,nonce,hash,`timestamp`,round_diff,total_share,reward FROM blocks WHERE state=? AND coin=? ORDER BY height DESC", constImmatureBlock, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []*types.BlockData
	for rows.Next() {
		var (
			state int
			height, roundHeight, uncleHeight int64
			nonce,hash                       string
			roundDiff, totalShare       	int64
			timestamp                  		int64
			orphan 							string
			reward				string
		)

		err := rows.Scan(&state, &roundHeight, &height, &uncleHeight, &orphan, &nonce, &hash, &timestamp, &roundDiff, &totalShare, &reward)
		if err != nil {
			log.Printf("mysql CollectLuckStats:rows.Scan() error: %v",err)
			return nil, err
		}

		block := d.convertBlockResults(state, height, roundHeight, uncleHeight, orphan, nonce, hash, timestamp, roundDiff, totalShare, reward)
		result = append(result, &block)
	}

	rows2, err := conn.Query("SELECT state,round_height,height,uncle_height,orphan,nonce,hash,`timestamp`,round_diff,total_share,reward FROM blocks WHERE state=? AND coin=? ORDER BY height DESC LIMIT ?", constMatureBlock, d.Config.Coin, windowMax)
	if err != nil {
		log.Fatal(err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var (
			state                            int
			height, roundHeight, uncleHeight int64
			nonce, hash                      string
			roundDiff, totalShare            int64
			timestamp                        int64
			orphan                           string
			reward                           string
		)

		err := rows2.Scan(&state, &roundHeight, &height, &uncleHeight, &orphan, &nonce, &hash, &timestamp, &roundDiff, &totalShare, &reward)
		if err != nil {
			log.Printf("mysql CollectLuckStats:rows2.Scan() error: %v", err)
			return nil, err
		}

		block := d.convertBlockResults(state, height, roundHeight, uncleHeight, orphan, nonce, hash, timestamp, roundDiff, totalShare, reward)
		result = append(result, &block)
	}

	return result, nil
}

func (d *Database) convertBlockResults(state int, height int64, roundHeight int64, uncleHeight int64, orphan string, nonce string, hash string, timestamp int64, roundDiff int64, totalShare int64, reward string) types.BlockData {
	block := types.BlockData{}
	block.State = state
	block.Height = height
	block.RoundHeight = roundHeight
	block.UncleHeight = uncleHeight
	block.Uncle = block.UncleHeight > 0
	block.Orphan, _ = strconv.ParseBool(orphan)
	block.Nonce = nonce
	block.Hash = hash
	block.Timestamp = timestamp
	block.Difficulty = roundDiff
	block.TotalShares = totalShare
	block.RewardString = reward
	block.ImmatureReward = reward
	block.ImmatureKey = ""
	return block
}


func (d *Database) GetPayees(max string) ([]*Payees, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT coin,login_addr, balance, payout_limit FROM miner_info WHERE ((payout_limit = 0 AND balance > ?) or (payout_limit > 0 AND balance > payout_limit) ) AND coin=? AND payout_lock = 0", max, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []*Payees
	for rows.Next() {
		var (
			coin string
			loginAddr string
			balance     int64
			payoutLimit int64
		)

		err := rows.Scan(&coin, &loginAddr, &balance, &payoutLimit)
		if err != nil {
			log.Printf("mysql GetPayees:rows.Scan() error: %v",err)
			return nil, err
		}

		result = append(result, &Payees{
			Coin: 		  coin,
			Addr:         loginAddr,
			Balance:      balance,
			Payout_limit: payoutLimit,
		})
	}

	return result, nil
}

// UpdateBalance Confirm the reward coin with the miner's wallet address.
func (d *Database) UpdateBalance(login string, amount int64, gasFee int64, coin string) (int, error) {
	conn := d.Conn

	ts := util.MakeTimestamp()

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	ret, err := tx.Exec(
		"UPDATE miner_info SET payout_lock=?,balance=balance-?,pending=pending+? WHERE coin=? AND login_addr=? AND payout_lock = 0",
		ts, amount + gasFee, amount, coin, login)	// gasFee is also removed.
	if err != nil {
		log.Fatal(err)
	}

	rowsAffected, err := ret.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rowsAffected <= 0 {
		return 1, err
	}

	_, err = tx.Exec(
		"UPDATE finances SET balance=balance-?,pending=pending+?,gas_fee=gas_fee+? WHERE coin=?",
		amount + gasFee, amount, gasFee, coin)
	if err != nil {
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	return 0, nil
}

func (d *Database) WritePayment(login, txHash string, amount int64,gasFee int64, coin string, from string) error {
	nowTime := util.MakeTimestamp() / 1000
	conn := d.Conn

	tx, err := conn.Begin()
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	ret, err := tx.Exec(
		"UPDATE miner_info SET payout_lock=?,pending=pending-?,paid=paid+?,payout_cnt=payout_cnt+1,payout_last=now() WHERE coin=? AND login_addr=? AND payout_lock > 0",
		0, amount, amount, coin, login)
	if err != nil {
		log.Fatal(err)
	}
	_, err = tx.Exec(
		"UPDATE finances SET pending=pending-?,paid=paid+?,payout_cnt=payout_cnt+1 WHERE coin=?",
		amount, amount, coin)
	if err != nil {
		log.Fatal(err)
	}
	_, err = tx.Exec(
		"INSERT INTO payments_all(login_addr,`from`,tx_hash,amount,tx_fee,`timestamp`,coin) VALUE (?,?,?,?,?,?,?)",
		login, from, txHash, amount, gasFee, nowTime, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	// defer stmt.Close() // danger!

	rowsAffected, err := ret.RowsAffected()
	if rowsAffected <= 0 {
		return err
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	return nil
}

func (d *Database) GetAllMinerAccount(duration time.Duration, minerChartIntvSec int64) ([]*MinerChartSelect, error) {
	ts := util.MakeTimestamp() / 1000 + minerChartIntvSec
	now := time.Now()
	nowTime := now.Add(-duration)

	conn := d.Conn
	rows, err := conn.Query("SELECT coin, login_addr, share, share_check FROM miner_info WHERE last_share > ? AND share_check < ? AND coin=?", nowTime, ts, d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []*MinerChartSelect
	for rows.Next() {
		var (
			coin 		string
			loginAddr  	string
			share 		int
			shareCheck 	int64
		)

		err := rows.Scan(&coin, &loginAddr, &share, &shareCheck)
		if err != nil {
			log.Printf("mysql GetAllMinerAccount:rows.Scan() error: %v",err)
			return nil, err
		}

		result = append(result, &MinerChartSelect{
			Coin: 			coin,
			Addr:           loginAddr,
			Share: 			share,
			ShareCheckTime: shareCheck,
		})
	}
	return result, nil
}

func (d *Database) CheckTimeMinerCharts(miner *MinerChartSelect, ts int64, minerChartIntvSec int64) bool {
	if ts < miner.ShareCheckTime + minerChartIntvSec {
		return false
	}

	conn := d.Conn
	ret,err := conn.Exec("UPDATE miner_info SET share_check=?,share=0 WHERE login_addr=? AND share_check=? AND coin=?", ts, miner.Addr, miner.ShareCheckTime, miner.Coin)
	if err != nil {
		log.Fatal(err)
	}

	if ok,_ := ret.RowsAffected(); ok <= 0  {
		return false
	}

	return true
}

func (d *Database) WriteMinerCharts(time1 int64, time2, k string, hash, largeHash, workerOnline int64, share int64, report int64) error {
	conn := d.Conn
	_, err := conn.Exec("INSERT INTO miner_charts(login_addr,time,time2,hash,large_hash,report_hash,share,work_online,coin) VALUE (?,?,?,?,?,?,?,?,?)",k, time1, time2,hash, largeHash, report, share, workerOnline, d.Config.Coin)
	if err != nil {
		return err
	}

	return nil
}

func (d *Database) GetMinerStats(login string, maxPayments int64) (map[string]interface{}, error) {
	stats := make(map[string]interface{})
	var (
		paymentsTotal int64
		err error
	)
	stats["stats"], paymentsTotal, err = d.getMinerInfo(login)
	if err != nil {
		return nil, err
	}
	stats["payments"], err = d.getMinerPayments(login, maxPayments)
	if err != nil {
		return nil, err
	}
	stats["paymentsTotal"] = paymentsTotal

	return stats, nil
}

func (d *Database) getMinerInfo(login string) (map[string]interface{}, int64, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT balance, pending, paid, immature, matured, blocks_found, last_share, payout_limit, payout_cnt FROM miner_info WHERE coin=? AND login_addr=?", d.Config.Coin, login)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	result := make(map[string]interface{})
	minerPaymentCnt := int64(0)
	for rows.Next() {
		var (
			balance, pending, paid, immature, matured, blocksFound, lastShare, payoutLimit string
		)

		err := rows.Scan(&balance, &pending, &paid, &immature, &matured, &blocksFound, &lastShare, &payoutLimit, &minerPaymentCnt)
		if err != nil {
			log.Printf("mysql GetMinerInfo:rows.Scan() error: %v",err)
			return nil, 0, err
		}

		d.convertStringMap(result, "balance", balance)
		d.convertStringMap(result, "pending", pending)
		d.convertStringMap(result, "paid", paid)
		d.convertStringMap(result, "immature", immature)
		d.convertStringMap(result, "matured", matured)
		d.convertStringMap(result, "blocksFound", blocksFound)

		amountInShannon, _:= strconv.ParseInt(payoutLimit,10,64)
		if  amountInShannon > d.Config.Threshold {
			d.convertStringMap(result, "payoutLimit", payoutLimit)
		} else {
			d.convertStringMap(result, "payoutLimit", strconv.FormatInt(d.Config.Threshold, 10))
		}

		intlastShare := util.MakeTimestampDB2(lastShare) / 1000
		d.convertStringMap(result, "lastShare", strconv.FormatInt(intlastShare, 10))
	}
	return result, minerPaymentCnt, nil
}

func (d *Database) getMinerPayments(login string, maxPayments int64) ([]map[string]interface{}, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT tx_hash, amount, tx_fee, `timestamp`, insert_time FROM payments_all WHERE coin=? AND login_addr=? ORDER BY seq DESC LIMIT ? ", d.Config.Coin, login, maxPayments)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var (
			txHash, amount, txFee, timestamp, insertTime string
		)

		err := rows.Scan(&txHash, &amount, &txFee, &timestamp, &insertTime)
		if err != nil {
			log.Printf("mysql getMinerPayments:rows.Scan() error: %v",err)
			return nil, err
		}

		tx := make(map[string]interface{})
		//tx["timestamp"] = int64(1639376142)
		//tx["tx"] = txHash
		//tx["address"] = login
		//tx["amount"], _ = strconv.ParseInt(amount, 10, 64)
		// timestamp := util.MakeTimestampDB2(insertTime) / 1000
		d.convertStringMap(tx, "timeFormat", insertTime)
		d.convertStringMap(tx, "timestamp", timestamp)
		d.convertStringMap(tx, "x", timestamp)
		d.convertStringMap(tx, "tx", txHash)
		d.convertStringMap(tx, "address", login)
		d.convertStringMap(tx, "amount", amount)
		d.convertStringMap(tx, "tx_fee", txFee)

		result = append(result, tx)
	}
	return result, nil
}

func (d *Database) GetAllPayments(maxPayments int64) ([]map[string]interface{}, int64, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT login_addr,tx_hash,amount,`timestamp`,insert_time FROM payments_all WHERE coin=? ORDER BY seq DESC LIMIT ? ", d.Config.Coin, maxPayments)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var result []map[string]interface{}
	for rows.Next() {
		var (
			address, txHash, amount, timestamp, insertTime string
		)

		err := rows.Scan(&address, &txHash, &amount, &timestamp, &insertTime)
		if err != nil {
			log.Printf("mysql getMinerPayments:rows.Scan() error: %v",err)
			return nil, 0, err
		}

		tx := make(map[string]interface{})
		//tx["timestamp"] = int64(1639376142)
		//tx["tx"] = txHash
		//tx["address"] = login
		//tx["amount"], _ = strconv.ParseInt(amount, 10, 64)
		d.convertStringMap(tx, "timeFormat", insertTime)
		d.convertStringMap(tx, "timestamp", timestamp)
		d.convertStringMap(tx, "x", timestamp)
		d.convertStringMap(tx, "tx", txHash)
		d.convertStringMap(tx, "address", address)
		d.convertStringMap(tx, "amount", amount)

		result = append(result, tx)
	}

	rows2, err := conn.Query("SELECT payout_cnt FROM finances WHERE coin=?", d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows2.Close()

	var count int64

	for rows2.Next() {
		err := rows2.Scan(&count)
		if err != nil {
			log.Printf("mysql GetAllPayments:rows2.Scan() error: %v",err)
			return nil, 0, err
		}
	}
	return result, count, nil
}


func (d *Database) getMinerPaymentCount(login string) (int64, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT count(*) FROM payments_all WHERE coin=? AND login_addr=? ", d.Config.Coin, login)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var count int64

	for rows.Next() {
		err := rows.Scan(&count)
		if err != nil {
			log.Printf("mysql getMinerPaymentCount:rows.Scan() error: %v",err)
			return 0, err
		}
	}
	return count, nil
}

func (d *Database) convertStringMap(result map[string]interface{},key string,value string) {
	var err error
	result[key], err = strconv.ParseInt(value, 10, 64)
	if err != nil {
		result[key] = value
	}
}

func (d *Database) GetMinerCharts(hashNum int64, chartIntv int64, login string, ts int64) (stats []*types.MinerCharts, err error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT `time`,time2,hash,large_hash,report_hash,share,work_online FROM miner_charts WHERE coin=? AND login_addr=? AND `time` > ? ORDER BY time desc LIMIT ? ", d.Config.Coin, login, ts - 172800, hashNum)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var (
		result []*types.MinerCharts
		first bool
	)
	for rows.Next() {
		var (
			time  			int64
			time2 			string
			hash        int64
			largeHash  int64
			reportHash int64
			share      int64
			workOnline string
		)

		err := rows.Scan(&time, &time2, &hash, &largeHash, &reportHash, &share, &workOnline)
		if err != nil {
			log.Printf("mysql GetMinerCharts:rows.Scan() error: %v",err)
			return nil, err
		}

		if !first {
			first = true
			if time + chartIntv + 300 < ts {
				result = append(result, &types.MinerCharts{
					Timestamp:       ts,
				})
			}
		}

		result = append(result, &types.MinerCharts{
			Timestamp:       time,
			TimeFormat:      time2,
			MinerHash:       hash,
			MinerLargeHash:  largeHash,
			WorkerOnline:    workOnline,
			Share:           share,
			MinerReportHash: reportHash,
		})
	}

	return result, nil
}

func (d *Database) GetChartRewardList(login string, maxList int) ([]*types.RewardData, error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT `timestamp`,amount,percent,hash,height FROM credits_immature WHERE coin=? AND login_addr=? ORDER BY timestamp desc LIMIT ? ", d.Config.Coin, login, maxList)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	//var result []*types.RewardData
	var resultImmature []*types.RewardData
	var resultBalance []*types.RewardData
	for rows.Next() {
		var (
			timestamp,amount,percent,hash,height 			string
		)

		err := rows.Scan(&timestamp, &amount, &percent, &hash, &height )
		if err != nil {
			log.Printf("mysql GetChartRewardList:rows.Scan() error: %v",err)
			return nil, err
		}

		retTimestamp, _ := strconv.ParseInt(timestamp, 10, 64)
		retReward, _ := strconv.ParseInt(amount, 10, 64)
		retHeight, _ := strconv.ParseInt(height, 10, 64)
		retPercent, _ := strconv.ParseFloat(percent, 64)
		resultImmature = append(resultImmature, &types.RewardData{
			Height:    retHeight,
			Timestamp: retTimestamp,
			BlockHash: hash,
			Reward:    retReward,
			Percent:   retPercent,
			Immature:  true,
		})
	}

	rows2, err := conn.Query("SELECT `timestamp`,amount,percent,hash,height FROM credits_balance WHERE coin=? AND login_addr=? ORDER BY timestamp desc LIMIT ? ", d.Config.Coin, login, maxList)
	if err != nil {
		log.Fatal(err)
	}
	defer rows2.Close()

	for rows2.Next() {
		var (
			timestamp,amount,percent,hash,height 			string
		)

		err := rows2.Scan(&timestamp, &amount, &percent, &hash, &height )
		if err != nil {
			log.Printf("mysql GetChartRewardList:rows2.Scan() error: %v",err)
			return nil, err
		}

		retTimestamp, _ := strconv.ParseInt(timestamp, 10, 64)
		retReward, _ := strconv.ParseInt(amount, 10, 64)
		retHeight, _ := strconv.ParseInt(height, 10, 64)
		retPercent, _ := strconv.ParseFloat(percent, 64)
		resultBalance = append(resultBalance, &types.RewardData{
			Height:    retHeight,
			Timestamp: retTimestamp,
			BlockHash: hash,
			Reward:    retReward,
			Percent:   retPercent,
			Immature:  false,
		})
	}

	for i, v := range resultImmature {
		for i2, v2 := range resultBalance {
			if v.Height == v2.Height && v.BlockHash == v2.BlockHash {
				resultImmature[i] = resultBalance[i2]
			}
		}
	}

	return resultImmature, nil
}



func (d *Database) GetPoolBalanceByOnce(maxHeight, minHeight int64, coin string) (*big.Int, int64, error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT ifnull(sum(cast(reward AS dec(50))),0),count(*) FROM credits_blocks WHERE coin=? AND height BETWEEN ? AND ?", coin, minHeight, maxHeight)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			sumReward string
			count int64
		)

		err := rows.Scan(&sumReward, &count)
		if err != nil {
			log.Printf("mysql GetPoolBalanceByOnce:rows.Scan() error: %v", err)
			return nil, 0, err
		}

		//reward, _ := strconv.ParseInt(sumReward,10,64)
		result := math.MustParseBig256(sumReward)
		result = result.Div(result, big.NewInt(maxHeight-minHeight))
		result = result.Div(result, big.NewInt(1000000000))

		return result, count, nil
	}

	return big.NewInt(0), 0, nil
}

func (d *Database) IsMinerExists(login string) (bool,int64,error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT login_addr,payout_limit FROM miner_info WHERE coin=? AND login_addr=?",d.Config.Coin, login)
	if err != nil {
		return true, 0, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			loginAddr, payoutLimit string
		)

		err := rows.Scan(&loginAddr, &payoutLimit)
		if err != nil {
			log.Printf("mysql ChoiceSubMiner:rows.Scan() error: %v", err)
			return false, 0, nil
		}

		settingPayout, _ := strconv.ParseInt(payoutLimit, 10, 64)
		return true, settingPayout, nil
	}
	return false, 0, nil
}


func (d *Database) GetIpInboundList() ([]*types.InboundIpList, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT ip,rule,`desc` FROM inbound_ip WHERE coin=?",d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	result := make([]*types.InboundIpList,0)

	for rows.Next() {
		var (
			ip,rule,desc string

		)
		err := rows.Scan(&ip, &rule, &desc)
		if err != nil {
			log.Printf("mysql GetIpInboundList:rows.Scan() error: %v", err)
			return nil, err
		}
		allowed := false
		if rule == "allow" {
			allowed = true
		}
		result = append(result, &types.InboundIpList{
			Ip:      ip,
			Allowed: allowed,
			Desc: desc,
		})
	}

	return result, nil
}

func (d *Database) SaveIpInbound(ip,rule string) bool {
	conn := d.Conn

	ret,err := conn.Exec("INSERT INTO inbound_ip(coin,ip,rule) VALUES (?,?,?)", d.Config.Coin, ip, rule)
	if err != nil {
		log.Printf("mysql SaveIpInbound:Exec() error: %v", err)
		return false
	}

	if ok,_ := ret.RowsAffected(); ok <= 0  {
		return false
	}

	return true
}

func (d *Database) DelIpInbound(ip string) bool {
	conn := d.Conn

	_,err := conn.Exec("DELETE FROM inbound_ip WHERE coin=? AND ip=?", d.Config.Coin, ip)
	if err != nil {
		log.Printf("mysql DelIpInbound:Exec() error: %v", err)
		return false
	}

	return true
}

func (d *Database) IsIdInboundId(devID string) bool {
	conn := d.Conn
	rows, err := conn.Query("SELECT id FROM inbound_id WHERE coin=? AND id=?",d.Config.Coin, devID)
	if err != nil {
		return false
	}
	defer rows.Close()
	for rows.Next() {
		return  true
	}
	return false
}


func (d *Database) GetIdInboundList() ([]*types.InboundIdList, error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT id,rule,alarm,`desc` FROM inbound_id WHERE coin=?",d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	result := make([]*types.InboundIdList,0)

	for rows.Next() {
		var (
			id,rule,alarm,desc string
		)
		err := rows.Scan(&id, &rule, &alarm, &desc)
		if err != nil {
			log.Printf("mysql GetIdInboundList:rows.Scan() error: %v", err)
			return nil, err
		}
		allowed := false
		if rule == "allow" {
			allowed = true
		}
		result = append(result, &types.InboundIdList{
			Id:      id,
			Allowed: allowed,
			Alarm: alarm,
			Desc: desc,
		})
	}

	return result, nil
}

func (d *Database) SaveIdInbound(id,rule,alarm,desc string) bool {
	conn := d.Conn

	ret,err := conn.Exec("INSERT INTO inbound_id(coin,id,rule,alarm,`desc`) VALUES (?,?,?,?,?)", d.Config.Coin, id, rule, alarm, desc)
	if err != nil {
		log.Printf("mysql SaveIpInbound:Exec() error: %v", err)
		return false
	}

	if ok,_ := ret.RowsAffected(); ok <= 0  {
		return false
	}

	return true
}

func (d *Database) UpdateIdInboundAlarm(id,alarm string) bool {
	conn := d.Conn
	//The location (d.Config.Coin) does not need to be set.
	_,err := conn.Exec("UPDATE inbound_id SET alarm=? WHERE coin=? AND id=?", alarm, d.Config.Coin, id)
	if err != nil {
		log.Fatal(err)
	}

	return true
}

func (d *Database) UpdateIdInboundDesc(id,desc string) bool {
	conn := d.Conn
	//The location (d.Config.Coin) does not need to be set.
	_,err := conn.Exec("UPDATE inbound_id SET `desc`=? WHERE coin=? AND id=?", desc, d.Config.Coin, id)
	if err != nil {
		log.Fatal(err)
	}

	return true
}


func (d *Database) DelIdInbound(id string) bool {
	conn := d.Conn

	_,err := conn.Exec("DELETE FROM inbound_id WHERE coin=? AND id=?", d.Config.Coin, id)
	if err != nil {
		log.Printf("mysql DelIpInbound:Exec() error: %v", err)
		return false
	}

	return true
}


func (d *Database) GetLikeMinerSubList(addr string) ([]*types.DevSubList, error) {
	conn := d.Conn

	var (
		result []*types.DevSubList
	)

	rows, err := conn.Query("SELECT login_addr,sub_addr,weight FROM miner_sub WHERE coin=? AND login_addr like ?", d.Config.Coin, "%" + addr + "%")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			devAddr, subAddr string
			weight  int64
		)

		err := rows.Scan(&devAddr, &subAddr, &weight)
		if err != nil {
			log.Printf("mysql ChoiceSubMiner:rows.Scan() error: %v", err)
			return nil, err
		}

		if weight <= 0 { weight = 1 }

		if result == nil {
			result = make([]*types.DevSubList,0)
		}

		result = append(result, &types.DevSubList{
			DevAddr: devAddr,
			SubAddr: subAddr,
			Amount:  weight,
		})
	}

	return result, err
}


func (d *Database)  GetMinerSubInfo(devId string) ([]*types.DevSubList, error) {
	conn := d.Conn

	var (
		result []*types.DevSubList
	)

	rows, err := conn.Query("SELECT login_addr,sub_addr,weight FROM miner_sub WHERE coin=? AND login_addr=?", d.Config.Coin, devId)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			devAddr, subAddr string
			weight  int64
		)

		err := rows.Scan(&devAddr, &subAddr, &weight)
		if err != nil {
			log.Printf("mysql GetMinerSubList:rows.Scan() error: %v", err)
			return nil, err
		}

		if weight <= 0 { weight = 1 }

		if result == nil {
			result = make([]*types.DevSubList,0)
		}

		result = append(result, &types.DevSubList{
			DevAddr: devAddr,
			SubAddr: subAddr,
			Amount:  weight,
		})
	}

	return result, err
}


func (d *Database)  GetMinerSubList() ([]*types.DevSubList, error) {
	conn := d.Conn

	result := make([]*types.DevSubList,0)

	rows, err := conn.Query("SELECT login_addr,sub_addr,weight FROM miner_sub WHERE coin=?", d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			devAddr, subAddr string
			weight  int64
		)

		err := rows.Scan(&devAddr, &subAddr, &weight)
		if err != nil {
			log.Printf("mysql GetMinerSubList:rows.Scan() error: %v", err)
			return nil, err
		}

		if weight <= 0 { weight = 1 }

		result = append(result, &types.DevSubList{
			DevAddr: devAddr,
			SubAddr: subAddr,
			Amount:  weight,
		})
	}

	return result, err
}


func (d *Database) SaveSubIdIndex(devId, subId string, amount int64) bool {
	conn := d.Conn

	ret,err := conn.Exec("INSERT INTO miner_sub(coin,login_addr,sub_addr,weight) VALUES (?, ?, ?, ?) ON DUPLICATE KEY UPDATE weight=VALUES(weight)", d.Config.Coin, devId, subId, amount)
	if err != nil {
		log.Printf("mysql SaveSubIdIndex:Exec() error: %v", err)
		return false
	}

	if ok,_ := ret.RowsAffected(); ok <= 0  {
		return false
	}

	return true
}


func (d *Database) DelSubIdIndex(devId, subId string) bool {
	conn := d.Conn

	_,err := conn.Exec("DELETE FROM miner_sub WHERE coin=? AND login_addr=? AND sub_addr=?", d.Config.Coin, devId, subId)
	if err != nil {
		log.Printf("mysql DelIpInbound:Exec() error: %v", err)
		return false
	}

	return true
}


func (d *Database) GetBanWhitelist() (mapset.Set, error) {
	conn := d.Conn

	rows, err := conn.Query("SELECT ip_addr FROM ban_whitelist WHERE coin=?",d.Config.Coin)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	result := mapset.NewSet()

	for rows.Next() {
		var (
			ip string
		)
		err := rows.Scan(&ip)
		if err != nil {
			log.Printf("mysql GetBanWhitelist:rows.Scan() error: %v", err)
			return nil, err
		}

		result.Add(ip)
	}

	return result, nil
}


func (d *Database) UpdatePayoutLimit(login string,dgcValue string) bool {
	conn := d.Conn
	//The location (d.Config.Coin) does not need to be set.
	_,err := conn.Exec("UPDATE miner_info SET payout_limit=? WHERE login_addr=?", dgcValue, login)
	if err != nil {
		log.Fatal(err)
	}

	return true
}

func (d *Database) CreateAccount(user string,pass []byte, access string) bool {
	conn := d.Conn
	//The location (d.Config.Coin) does not need to be set.
	ret,err := conn.Exec("INSERT INTO account(id,password,access) VALUES (?,?,?)", user, pass, access)
	if err != nil {
		log.Printf("mysql CreateAccount:Exec() error: %v", err)
		return false
	}

	if ok,_ := ret.RowsAffected(); ok <= 0  {
		return false
	}

	return true
}

func (d *Database) ChangeAccountAccess(user string, access string) bool {
	conn := d.Conn
	//The location (d.Config.Coin) does not need to be set.
	_,err := conn.Exec("UPDATE account SET access=? WHERE id=? ", access, user)
	if err != nil {
		log.Printf("mysql ChangeAccountAccess:Exec() error: %v", err)
		return false
	}

	return true
}

func (d *Database) ChangeAccountPassword(user string, pass []byte) bool {
	conn := d.Conn
	//The location (d.Config.Coin) does not need to be set.
	_,err := conn.Exec("UPDATE account SET password=? WHERE id=? ", pass, user)
	if err != nil {
		log.Printf("mysql ChangeAccountPassword:Exec() error: %v", err)
		return false
	}

	return true
}

func (d *Database) DeleteAccount(user string) bool {
	conn := d.Conn

	_,err := conn.Exec("DELETE FROM account WHERE id=? ", user)
	if err != nil {
		log.Printf("mysql DeleteAccount:Exec() error: %v", err)
		return false
	}

	return true
}

func (d *Database) GetAccountPassword(id string) (string, string, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT password,access FROM account WHERE id=?", id)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			password, access string
		)
		err := rows.Scan(&password, &access)
		if err != nil {
			log.Printf("mysql GetAccountPassword:rows.Scan() error: %v", err)
			return "", "", err
		}

		return password, access, nil
	}

	return "", "", nil
}

func (d *Database) GetAccountList() ([]*types.UserInfo, error) {
	conn := d.Conn
	rows, err := conn.Query("SELECT id,access FROM account")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	var (
		result []*types.UserInfo
	)


	for rows.Next() {
		var (
			id, access string
		)
		err := rows.Scan(&id, &access)
		if err != nil {
			log.Printf("mysql GetAccountPassword:rows.Scan() error: %v", err)
			return nil, err
		}

		userInfo := &types.UserInfo{
			Username: id,
			Access:   access,
		}
		result = append(result, userInfo)
	}

	return result, nil
}


func (d *Database) GetAlarmInfo() (map[string]*types.InboundIdList, error){
	conn := d.Conn

	rows, err := conn.Query("SELECT id, alarm,`desc` FROM inbound_id WHERE coin=? and alarm!=? ",d.Config.Coin,"none")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	result := make(map[string]*types.InboundIdList,0)

	for rows.Next() {
		var (
			id,alarm,desc string
		)
		err := rows.Scan(&id, &alarm, &desc)
		if err != nil {
			log.Printf("mysql GetAlarmInfo:rows.Scan() error: %v", err)
			return nil, err
		}

		if alarm == "slack" || alarm == "mail" {
			result[id] = &types.InboundIdList{
				Id:      id,
				Alarm: alarm,
				Desc: desc,
			}
		}
	}

	return result, nil
}

func (d *Database) GetBlockBalanceMinMax() (int64, int64) {
	conn := d.Conn

	rows, err := conn.Query("SELECT min(seq),max(seq) FROM credits_balance")
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			minSeq,maxSeq int64
		)
		err := rows.Scan(&minSeq, &maxSeq)
		if err != nil {
			log.Printf("mysql GetBlockBalanceMinMax:rows.Scan() error: %v", err)
			return 0, 0
		}

		return minSeq, maxSeq
	}

	return 0, 0
}


func (d *Database) DeleteBlockBalance(min,max int64) (int64) {
	conn := d.Conn

	rows ,err := conn.Exec("DELETE FROM `credits_balance` WHERE seq BETWEEN ? AND ?  ", min, max)
	if err != nil {
		log.Printf("mysql DeleteAccount:Exec() error: %v", err)
		return 0
	}

	res, err := rows.RowsAffected()
	if err != nil {
		return 0
	}
	return res
}