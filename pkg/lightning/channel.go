package lightning

import (
	"Coin/pkg/block"
	"Coin/pkg/id"
	"Coin/pkg/peer"
	"Coin/pkg/pro"
	"Coin/pkg/script"
	"Coin/pkg/utils"
)

// Channel is our node's view of a channel
// Funder is whether we are the channel's funder
// FundingTransaction is the channel's funding transaction
// CounterPartyPubKey is the other node's public key
// State is the current state that we are at. On instantiation,
// the refund transaction is the transaction for state 0
// Transactions is the slice of transactions, indexed by state
// MyRevocationKeys is a mapping of my private revocation keys
// TheirRevocationKeys is a mapping of their private revocation keys
type Channel struct {
	Funder             bool
	FundingTransaction *block.Transaction
	State              int
	CounterPartyPubKey []byte

	MyTransactions    []*block.Transaction
	TheirTransactions []*block.Transaction

	MyRevocationKeys    map[string][]byte
	TheirRevocationKeys map[string]*RevocationInfo
}

type RevocationInfo struct {
	RevKey            []byte
	TransactionOutput *block.TransactionOutput
	OutputIndex       uint32
	TransactionHash   string
	ScriptType        int
}

// GenerateRevocationKey returns a new public, private key pair
func GenerateRevocationKey() ([]byte, []byte) {
	i, _ := id.CreateSimpleID()
	return i.GetPublicKeyBytes(), i.GetPrivateKeyBytes()
}

// CreateChannel creates a channel with another lightning node
// fee must be enough to cover two transactions! You will get back change from first
func (ln *LightningNode) CreateChannel(peer *peer.Peer, theirPubKey []byte, amount uint32, fee uint32) {
	// TODO
	channel := &Channel{
		Funder:              true,
		FundingTransaction:  nil,
		State:               0,
		CounterPartyPubKey:  theirPubKey,
		MyTransactions:      nil,
		TheirTransactions:   nil,
		MyRevocationKeys:    make(map[string][]byte),
		TheirRevocationKeys: make(map[string]*RevocationInfo),
	}

	ln.Channels[peer] = channel

	walletRequest := WalletRequest{
		Amount:             amount,
		Fee:                fee * 2,
		CounterPartyPubKey: theirPubKey,
	}

	fundingTx := ln.generateFundingTransaction(walletRequest)
	pubK, priK := GenerateRevocationKey()
	refundTx := ln.generateRefundTransaction(theirPubKey, fundingTx, fee, pubK)
	channel.MyRevocationKeys[refundTx.Hash()] = priK

	request := &pro.OpenChannelRequest{
		Address:            ln.Address,
		PublicKey:          ln.Id.GetPublicKeyBytes(),
		FundingTransaction: block.EncodeTransaction(fundingTx),
		RefundTransaction:  block.EncodeTransaction(refundTx),
	}
	resp, err := peer.Addr.OpenChannelRPC(request)
	if err != nil {
		panic("OpenChannelRPC error")
	}
	newFundingTx := block.DecodeTransaction(resp.GetSignedFundingTransaction())
	newRefundTx := block.DecodeTransaction(resp.GetSignedRefundTransaction())

	channel.FundingTransaction = newFundingTx
	channel.MyTransactions = append(channel.MyTransactions, newRefundTx)
	channel.TheirTransactions = append(channel.TheirTransactions, newRefundTx)

	sig, _ := utils.Sign(ln.Id.GetPrivateKey(), []byte(fundingTx.Hash()))
	fundingTx.Witnesses = append(fundingTx.Witnesses, sig)

	go func() {
		ln.BroadcastTransaction <- fundingTx
	}()

}

// UpdateState is called to update the state of a channel.
func (ln *LightningNode) UpdateState(peer *peer.Peer, tx *block.Transaction) {
	// TODO
	request := &pro.TransactionWithAddress{
		Transaction: block.EncodeTransaction(tx),
		Address:     ln.Address,
	}
	updateTx, err := peer.Addr.GetUpdatedTransactionsRPC(request)
	if err != nil {
		panic("GetUpdatedTransactionsRPC error")
	}
	signedTx := block.DecodeTransaction(updateTx.SignedTransaction)
	unsignedTx := block.DecodeTransaction(updateTx.UnsignedTransaction)
	channel, ok := ln.Channels[peer]
	if !ok {
		panic("GetUpdatedTransactionsRPC error")
	}

	channel.MyTransactions = append(channel.MyTransactions, signedTx)

	sig, _ := utils.Sign(ln.Id.GetPrivateKey(), []byte(unsignedTx.Hash()))
	unsignedTx.Witnesses = append(unsignedTx.Witnesses, sig)
	channel.TheirTransactions = append(channel.TheirTransactions, unsignedTx)

	pubK, _ := channel.MyRevocationKeys[signedTx.Hash()]
	revRequest := &pro.SignedTransactionWithKey{
		SignedTransaction: block.EncodeTransaction(unsignedTx),
		RevocationKey:     pubK,
		Address:           ln.Address,
	}
	revKey, err := peer.Addr.GetRevocationKeyRPC(revRequest)
	if err != nil {
		panic("GetUpdatedTransactionsRPC error")
	}
	theirRevKey := revKey.Key
	channel.State++

	lockingScript := unsignedTx.Outputs[0].LockingScript
	scriptType, err := script.DetermineScriptType(lockingScript)
	if err != nil {
		panic("GetUpdatedTransactionsRPC error")
	}
	index := uint32(0)
	if channel.Funder {
		index = uint32(1)
	}

	revocationInfo := &RevocationInfo{
		RevKey:            theirRevKey,
		TransactionOutput: unsignedTx.Outputs[index],
		OutputIndex:       index,
		TransactionHash:   unsignedTx.Hash(),
		ScriptType:        scriptType,
	}
	//channel.MyRevocationKeys[unsignedTx.Hash()] = priK
	channel.TheirRevocationKeys[string(theirRevKey)] = revocationInfo
}
