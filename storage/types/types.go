package types

import (
	"github.com/cellcrypto/open-dangnn-pool/util"
	"github.com/ethereum/go-ethereum/common/math"
	"math/big"
)

type BlockData struct {
	Height         int64    `json:"height"`
	Timestamp      int64    `json:"timestamp"`
	Difficulty     int64    `json:"difficulty"`
	TotalShares    int64    `json:"shares"`
	Uncle          bool     `json:"uncle"`
	UncleHeight    int64    `json:"uncleHeight"`
	Orphan         bool     `json:"orphan"`
	Hash           string   `json:"hash"`
	Nonce          string   `json:"-"`
	PowHash        string   `json:"-"`
	MixDigest      string   `json:"-"`
	Reward         *big.Int `json:"-"`
	ExtraReward    *big.Int `json:"-"`
	ImmatureReward string   `json:"-"`
	RewardString   string   `json:"reward"`
	RoundHeight    int64    `json:"-"`
	CandidateKey   string
	ImmatureKey    string
	State		   int
}

type MinerCharts struct {
	Timestamp      int64  `json:"x"`
	TimeFormat     string `json:"timeFormat"`
	MinerHash      int64  `json:"minerHash"`
	MinerLargeHash int64  `json:"minerLargeHash"`
	WorkerOnline   string `json:"workerOnline"`
	Share			int64 `json:"minerShare"`
	MinerReportHash int64 `json:"minerReportHash"`
}

type RewardData struct {
	Height    int64   `json:"blockheight"`
	Timestamp int64   `json:"timestamp"`
	BlockHash string  `json:"blockhash"`
	Reward    int64   `json:"reward"`
	Percent   float64 `json:"percent"`
	Immature  bool    `json:"immature"`
}

type CreditsImmatrue struct {
	Addr string
	Amount int64
}

type InboundIpList struct {
	Ip      string
	Allowed bool // true: allow false: deny
	Desc	string
}

type InboundIdList struct {
	Id      string
	Allowed bool // true: allow false: deny
	Alarm	string	// none, slack, mail
	Desc	string
}

type UserInfo struct {
	Username string `json:"username"`
	Access string `json:"access"`
}

type DevSubList struct {
	DevAddr 	string
	SubAddr 	string
	Amount		int64
}

var (
	GenesisReword =   math.MustParseBig256("3000000000000000000")	// 300DGC = 3ETH
	CarratReward =    math.MustParseBig256("3300000000000000000")	// 330DGC = 3.3ETH
	DiffByShareValue            = int64(2000000000)
	CarrathardforkheightMainnet = int64(400000)
	CarrathardforkheightTestnet = int64(641800)
)

func GetConstReward(height int64, mainnet bool) *big.Int {
	if mainnet == true {
		if height >= CarrathardforkheightMainnet {
			return new(big.Int).Set(CarratReward)
		}

	} else {
		if height >= CarrathardforkheightTestnet {
			return new(big.Int).Set(CarratReward)
		}
	}
	return new(big.Int).Set(GenesisReword)
}

func GetRewardForUncle(height int64, mainnet bool) *big.Int {
	reward := GetConstReward(height, mainnet)
	return new(big.Int).Div(reward, new(big.Int).SetInt64(32))
}

func GetUncleReward(uHeight, height int64, mainnet bool) *big.Int {
	reward := GetConstReward(height, mainnet)
	k := height - uHeight
	reward.Mul(big.NewInt(8-k), reward)
	reward.Div(reward, big.NewInt(8))
	return reward
}

func (b *BlockData) RewardInShannon() int64 {
	reward := new(big.Int).Div(b.Reward, util.Shannon)
	return reward.Int64()
}

func (b *BlockData) SerializeHash() string {
	if len(b.Hash) > 0 {
		return b.Hash
	} else {
		return "0x0"
	}
}

func (b *BlockData) RoundKey() string {
	return util.Join(b.RoundHeight, b.Hash)
}

func (b *BlockData) Key() string {
	return util.Join(b.UncleHeight, b.Orphan, b.Nonce, b.SerializeHash(), b.Timestamp, b.Difficulty, b.TotalShares, b.Reward)
}
