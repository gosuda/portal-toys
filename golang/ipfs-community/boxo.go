package main

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/ipfs/boxo/bitswap"
	bnet "github.com/ipfs/boxo/bitswap/network"
	bsnet "github.com/ipfs/boxo/bitswap/network/bsnet"
	"github.com/ipfs/boxo/blockservice"
	blockstore "github.com/ipfs/boxo/blockstore"
	dag "github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	dspebble "github.com/ipfs/go-ds-pebble"
	format "github.com/ipfs/go-ipld-format"
	"github.com/libp2p/go-libp2p"
)

// ipfsLedger wraps a Boxo DAG service backed by a Pebble datastore.
// It stores arbitrary blobs as IPFS blocks and returns their CIDs.
type ipfsLedger struct {
	ds    ds.Batching
	store blockstore.Blockstore
	dag   format.DAGService
}

// openIPFSLedger initializes a Boxo DAGService backed by Pebble (go-ds-pebble).
// dir is the directory where Pebble files will be stored.
func openIPFSLedger(dir string) (*ipfsLedger, error) {
	if dir == "" {
		return nil, nil
	}
	ctx := context.Background()

	dbPath := filepath.Join(dir, "ipfs-pebble")
	dstore, err := dspebble.NewDatastore(dbPath, nil)
	if err != nil {
		return nil, err
	}
	bstore := blockstore.NewBlockstore(dstore)

	host, err := libp2p.New()
	if err != nil {
		return nil, err
	}
	bsnet := bsnet.NewFromIpfsHost(host)
	bsnet = bnet.New(nil, bsnet, nil)
	bswap := bitswap.New(ctx, bsnet, nil, bstore,
		bitswap.SetSendDontHaves(true),
		bitswap.ProviderSearchDelay(time.Second),
	)

	blockServ := blockservice.New(bstore, bswap)
	dagSvc := dag.NewDAGService(blockServ)
	return &ipfsLedger{
		ds:    dstore,
		store: bstore,
		dag:   dagSvc,
	}, nil
}

var rootKey = ds.NewKey("root-cid")

// PutRaw stores a raw byte slice as a block and returns its CID.
func (l *ipfsLedger) PutRaw(ctx context.Context, data []byte) (cid.Cid, error) {
	if l == nil || l.dag == nil {
		return cid.Cid{}, nil
	}
	node := dag.NewRawNode(data)
	if err := l.dag.Add(ctx, node); err != nil {
		return cid.Cid{}, err
	}
	return node.Cid(), nil
}

// GetRaw fetches the raw bytes for a given CID.
func (l *ipfsLedger) GetRaw(ctx context.Context, c cid.Cid) ([]byte, error) {
	if l == nil || l.dag == nil {
		return nil, nil
	}
	nd, err := l.dag.Get(ctx, c)
	if err != nil {
		return nil, err
	}
	if rn, ok := nd.(*dag.RawNode); ok {
		return rn.RawData(), nil
	}
	return nd.RawData(), nil
}

// SetRootCID stores the "root" CID for the current logical state in the datastore.
func (l *ipfsLedger) SetRootCID(ctx context.Context, c cid.Cid) error {
	if l == nil || l.ds == nil {
		return nil
	}
	if !c.Defined() {
		return nil
	}
	return l.ds.Put(ctx, rootKey, []byte(c.String()))
}

// RootCID returns the last stored root CID, if any.
func (l *ipfsLedger) RootCID(ctx context.Context) (cid.Cid, error) {
	if l == nil || l.ds == nil {
		return cid.Cid{}, nil
	}
	data, err := l.ds.Get(ctx, rootKey)
	if err != nil {
		if errors.Is(err, ds.ErrNotFound) {
			return cid.Cid{}, nil
		}
		return cid.Cid{}, err
	}
	c, err := cid.Parse(string(data))
	if err != nil {
		return cid.Cid{}, err
	}
	return c, nil
}
