package mysql

import (
	"fmt"
	"github.com/cellcrypto/open-dangnn-pool/rpc"
	"github.com/cellcrypto/open-dangnn-pool/storage/types"
	"github.com/cellcrypto/open-dangnn-pool/util"
	"log"
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
		Password: "test!@#",
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

	var (
		countBlock int64
		countUncle int64
	)
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

			// Uncle Block?
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
				countUncle++
				continue
			}

			uncleBlock, err := rpc.GetBlockByHash(hash)
			if err != nil || hash != uncleBlock.Hash {
				blockHeight, _ := strconv.ParseInt(strings.Replace(block.Number, "0x", "", -1), 16, 64)
				uncleHeight, _ := strconv.ParseInt(strings.Replace(uncleBlock.Number, "0x", "", -1), 16, 64)
				t.Errorf("not found uncle block index(%v:%v) hash: %v %v", uncleHeight, blockHeight, uncleBlock.Hash,hash)
				countUncle++
				continue
			}

			if len(uncleBlock.Transactions) > 0 {
				return
			}

			uncleHeight, _ := strconv.ParseInt(strings.Replace(uncleBlock.Number, "0x", "", -1), 16, 64)
			// Basic block creation reward
			var createReward = types.GetUncleReward(uncleHeight , iHeight)

			dbReward, boo := new(big.Int).SetString(reward, 10)
			if !boo {
				return
			}
			if createReward.Cmp(dbReward) != 0 {
				t.Errorf("not matched: %v %v",dbReward,createReward)
			}
			fmt.Println("createReward:",createReward,"reward:",reward)
			countUncle++
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

		countBlock++
	}

	return
}


func TestPayoutTxCheck(t *testing.T)  {

	Daemon := "http://127.0.0.1:8545"
	Timeout := "10s"
	rpc := rpc.NewRPCClient("BlockChecker", Daemon, Timeout)

	var (
		count int64
	)
	conn := db.Conn
	rows, err := conn.Query("SELECT tx_hash,amount FROM payments_all")
	if err != nil {
		t.Error("select error")
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			txHash,reword string
		)

		err := rows.Scan(&txHash, &reword)
		if err != nil {
			t.Errorf("Mysql rows.Scan() error: %v", err)
			return
		}

		txReceipt, err := rpc.GetTxReceipt(txHash)
		if err != nil {
			t.Errorf("no have transaction receipt tx:%v err: %v", txHash, err)
			return
		}

		if txReceipt == nil {
			t.Errorf("not have transaction receipt tx:%v err: %v", txHash, err)
			return
		}

		if txReceipt.Confirmed() {
			if txReceipt.Successful() {
				log.Printf("Payout tx successful for %s", txReceipt.TxHash)

			} else {
				t.Errorf("Payout tx failed for tx:%v err: %v", txHash, err)
				return
			}
		} else {
			t.Errorf("Payout tx failed for tx:%v err: %v", txHash, err)
			return
		}

		// Let's see if it's an Uncle Block.
		blockNumber, _ := strconv.ParseInt(strings.Replace(txReceipt.BlockNumber, "0x", "", -1), 16, 64)
		block, err := rpc.GetBlockByHeight(blockNumber)
		if block.Hash != txReceipt.BlockHash {
			t.Errorf("Block hash is different It can be an uncle block (num:%v) (%v,%v) err: %v", blockNumber, block.Hash, txReceipt.BlockHash, err)
			return
		}

		count++
	}

	log.Printf("Payout tx total success:%v", count)
	return
}


