package lightning

import (
	"Coin/pkg/address"
	"Coin/pkg/block"
	"Coin/pkg/peer"
	"Coin/pkg/pro"
	"Coin/pkg/script"
	"context"
	"errors"
	"time"
)

// Version was copied directly from pkg/server.go. Only changed the function receiver and types
func (ln *LightningNode) Version(ctx context.Context, in *pro.VersionRequest) (*pro.Empty, error) {
	// Reject all outdated versions (this is not true to Satoshi Client)
	if in.Version != ln.Config.Version {
		return &pro.Empty{}, nil
	}
	// If addr map is full or does not contain addr of ver, reject
	newAddr := address.New(in.AddrMe, uint32(time.Now().UnixNano()))
	if ln.AddressDB.Get(newAddr.Addr) != nil {
		err := ln.AddressDB.UpdateLastSeen(newAddr.Addr, newAddr.LastSeen)
		if err != nil {
			return &pro.Empty{}, nil
		}
	} else if err := ln.AddressDB.Add(newAddr); err != nil {
		return &pro.Empty{}, nil
	}
	newPeer := peer.New(ln.AddressDB.Get(newAddr.Addr), in.Version, in.BestHeight)
	// Check if we are waiting for a ver in response to a ver, do not respond if this is a confirmation of peering
	pendingVer := newPeer.Addr.SentVer != time.Time{} && newPeer.Addr.SentVer.Add(ln.Config.VersionTimeout).After(time.Now())
	if ln.PeerDb.Add(newPeer) && !pendingVer {
		newPeer.Addr.SentVer = time.Now()
		_, err := newAddr.VersionRPC(&pro.VersionRequest{
			Version:    ln.Config.Version,
			AddrYou:    in.AddrYou,
			AddrMe:     ln.Address,
			BestHeight: ln.BlockHeight,
		})
		if err != nil {
			return &pro.Empty{}, err
		}
	}
	return &pro.Empty{}, nil
}

// OpenChannel is called by another lightning node that wants to open a channel with us
func (ln *LightningNode) OpenChannel(ctx context.Context, in *pro.OpenChannelRequest) (*pro.OpenChannelResponse, error) {
	//TODO
	peer := ln.PeerDb.Get(in.Address)
	if peer == nil {
		return nil, nil
	}
	_, ok := ln.Channels[peer]
	if ok {
		return nil, nil
	}
	fundingTransaction := block.DecodeTransaction(in.FundingTransaction)
	err := ln.ValidateAndSign(fundingTransaction)
	if err != nil {
		return nil, err
	}
	refundTransaction := block.DecodeTransaction(in.RefundTransaction)
	err = ln.ValidateAndSign(refundTransaction)
	if err != nil {
		return nil, err
	}
	// TODO create channel ?
	channel := &Channel{
		Funder:              false,
		FundingTransaction:  fundingTransaction,
		State:               0,
		CounterPartyPubKey:  in.PublicKey,
		MyTransactions:      nil,
		TheirTransactions:   nil,
		MyRevocationKeys:    make(map[string][]byte),
		TheirRevocationKeys: make(map[string]*RevocationInfo),
	}
	channel.MyTransactions = append(channel.MyTransactions, refundTransaction)
	channel.TheirTransactions = append(channel.TheirTransactions, refundTransaction)
	_, priK := GenerateRevocationKey()
	channel.MyRevocationKeys[refundTransaction.Hash()] = priK
	ln.Channels[peer] = channel

	resp := &pro.OpenChannelResponse{
		PublicKey:                ln.Id.GetPublicKeyBytes(),
		SignedFundingTransaction: block.EncodeTransaction(fundingTransaction),
		SignedRefundTransaction:  block.EncodeTransaction(refundTransaction),
	}

	return resp, nil
}

func (ln *LightningNode) GetUpdatedTransactions(ctx context.Context, in *pro.TransactionWithAddress) (*pro.UpdatedTransactions, error) {
	// TODO
	// validate the address we’ve been sent.
	tx, _ := block.DecodeTransactionWithAddress(in)
	peer := ln.PeerDb.Get(in.Address)
	if peer == nil {
		return nil, nil
	}
	channel, ok := ln.Channels[peer]
	if !ok {
		return nil, errors.New("do not have the channel")
	}
	// sign the transaction we’ve been sent, and add our signature to the slice of witnesses
	err := ln.ValidateAndSign(tx)
	if err != nil {
		return nil, err
	}
	// make our own version of this transaction where the counterparty’s coin is revocable.
	pubK, priK := GenerateRevocationKey()
	myVersionTx := ln.generateTransactionWithCorrectScripts(peer, tx, pubK)
	channel.TheirTransactions = append(channel.TheirTransactions, tx)
	channel.MyRevocationKeys[myVersionTx.Hash()] = priK

	resp := &pro.UpdatedTransactions{
		SignedTransaction:   block.EncodeTransaction(tx),
		UnsignedTransaction: block.EncodeTransaction(myVersionTx),
	}
	return resp, nil
}

func (ln *LightningNode) GetRevocationKey(ctx context.Context, in *pro.SignedTransactionWithKey) (*pro.RevocationKey, error) {
	// TODO
	peer := ln.PeerDb.Get(in.Address)
	if peer == nil {
		return nil, nil
	}
	channel, ok := ln.Channels[peer]
	if !ok {
		return nil, errors.New("do not have the channel")
	}
	signedTransaction := block.DecodeTransaction(in.SignedTransaction)
	channel.MyTransactions = append(channel.MyTransactions, signedTransaction)

	lockingScript := signedTransaction.Outputs[0].LockingScript
	scriptType, err := script.DetermineScriptType(lockingScript)
	if err != nil {
		return nil, err
	}
	// TODO which coin their revocation key belongs to ?
	// TODO If we’re the channel’s funder ?
	index := uint32(0)
	if channel.Funder {
		index = uint32(1)
	}

	revocationInfo := &RevocationInfo{
		RevKey:            in.RevocationKey,
		TransactionOutput: signedTransaction.Outputs[index],
		OutputIndex:       index,
		TransactionHash:   signedTransaction.Hash(),
		ScriptType:        scriptType,
	}
	channel.TheirRevocationKeys[string(in.RevocationKey)] = revocationInfo
	channel.State++

	//pubK, priK := GenerateRevocationKey()
	pk, _ := channel.MyRevocationKeys[signedTransaction.Hash()]
	return &pro.RevocationKey{
		Key: pk,
	}, nil
}
