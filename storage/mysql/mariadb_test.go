package mysql

import (
	"github.com/cellcrypto/open-ethereum-pool/rpc"
	"github.com/cellcrypto/open-ethereum-pool/storage/types"
	"github.com/cellcrypto/open-ethereum-pool/util"
	"math/big"
	"os"
	"strconv"
	"strings"
	"testing"
)

var db *Database

func TestMain(m *testing.M) {
	db, _ =New(&Config{
		Endpoint: "127.0.0.1",
		UserName: "root",
		Password: "thtm007!@#",
		Database: "pool",
		Port:     3308,
		PoolSize: 30,
		Coin: "",
	},2000000000, nil)

	c := m.Run()
	os.Exit(c)
}

func TestCreditsBlocksCheck(t *testing.T)  {

	Daemon := "http://127.0.0.1:8545"
	Timeout := "10s"
	rpc := rpc.NewRPCClient("BlockChecker", Daemon, Timeout)

	conn := db.Conn
	rows, err := conn.Query("SELECT height,hash,reward,`timestamp` FROM credits_blocks")
	if err != nil {
		t.Error("select error")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			height,hash,reward,timestamp string
		)

		err := rows.Scan(&height, &hash, &reward, &timestamp)
		if err != nil {
			t.Errorf("Mysql rows.Scan() error: %v", err)
			return
		}

		iHeight, _:= strconv.ParseInt(height,10,64)
		block, err := rpc.GetBlockByHeight(iHeight)
		if block.Hash != hash {
			if len(block.Uncles) == 0 {
				blockHeight, _ := strconv.ParseInt(strings.Replace(block.Number, "0x", "", -1), 16, 64)
				t.Errorf("not found block index(%v:%v) hash: %v %v", iHeight, blockHeight, block.Hash,hash)
			}

			// 엉클 블록?
			uncleFlag := false
			for _, v := range block.Uncles {
				if v == hash {
					uncleFlag = true
					break
				}
			}

			if uncleFlag == false {
				//
				blockHeight, _ := strconv.ParseInt(strings.Replace(block.Number, "0x", "", -1), 16, 64)
				t.Errorf("not found block index(%v:%v) hash: %v %v", iHeight, blockHeight, block.Hash,hash)
				continue
			}
			continue
		}


		blockHeight, _ := strconv.ParseInt(strings.Replace(block.Number, "0x", "", -1), 16, 64)
		// Basic block creation reward
		createReward := types.GetConstReward(blockHeight)

		// Rewards including Uncle.
		uncleReward := new(big.Int)
		uncleReward = uncleReward.Mul(types.GetRewardForUncle(blockHeight), big.NewInt(int64(len(block.Uncles))))

		// Transaction Fee Processing Rewards
		amount := big.NewInt(0)

		for _, tx := range block.Transactions {
			receipt, err := rpc.GetTxReceipt(tx.Hash)
			if err != nil {
				t.Errorf("rpc network failed error: %v", err)
				return
			}
			if receipt != nil {
				gasUsed := util.String2Big(receipt.GasUsed)
				gasPrice := util.String2Big(tx.GasPrice)
				fee := new(big.Int).Mul(gasUsed, gasPrice)
				amount.Add(amount, fee)
			}
		}

		createReward.Add(createReward.Add(createReward,uncleReward),amount)
		dbReward, boo := new(big.Int).SetString(reward, 10)
		if !boo {
			return
		}
		if createReward.Cmp(dbReward) != 0 {
			t.Errorf("not matched: %v %v",dbReward,createReward)
		}

	}

	return

}
