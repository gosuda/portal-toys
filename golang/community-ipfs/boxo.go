package main

import (
	"context"
	"path/filepath"

	"github.com/ipfs/boxo/bitswap"
	"github.com/ipfs/boxo/blockservice"
	blockstore "github.com/ipfs/boxo/blockstore"
	dag "github.com/ipfs/boxo/ipld/merkledag"
	"github.com/ipfs/go-cid"
	ds "github.com/ipfs/go-datastore"
	dspebble "github.com/ipfs/go-ds-pebble"
	format "github.com/ipfs/go-ipld-format"
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
	dbPath := filepath.Join(dir, "ipfs-pebble")
	dstore, err := dspebble.NewDatastore(dbPath, nil)
	if err != nil {
		return nil, err
	}
	store := blockstore.NewBlockstore(dstore)
	bs := bitswap.New(context.Background(), nil, nil, store)
	blockServ := blockservice.New(store, bs)

	dagSvc := dag.NewDAGService(blockServ)
	return &ipfsLedger{
		ds:    dstore,
		store: store,
		dag:   dagSvc,
	}, nil
}

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
