package namesys

import (
	"context"
	"fmt"
	"testing"
	"time"

	opts "github.com/ipsn/go-ipfs/namesys/opts"
	path "github.com/ipsn/go-ipfs/path"

	u "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-util"
	mockrouting "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-ipfs-routing/mock"
	record "github.com/ipsn/go-ipfs/gxlibs/github.com/libp2p/go-libp2p-record"
	routing "github.com/ipsn/go-ipfs/gxlibs/github.com/libp2p/go-libp2p-routing"
	ropts "github.com/ipsn/go-ipfs/gxlibs/github.com/libp2p/go-libp2p-routing/options"
	testutil "github.com/ipsn/go-ipfs/gxlibs/github.com/libp2p/go-testutil"
	proto "github.com/gogo/protobuf/proto"
	peer "github.com/ipsn/go-ipfs/gxlibs/github.com/libp2p/go-libp2p-peer"
	pstore "github.com/ipsn/go-ipfs/gxlibs/github.com/libp2p/go-libp2p-peerstore"
	ci "github.com/ipsn/go-ipfs/gxlibs/github.com/libp2p/go-libp2p-crypto"
	ds "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-datastore"
	dssync "github.com/ipsn/go-ipfs/gxlibs/github.com/ipfs/go-datastore/sync"
)

func testValidatorCase(t *testing.T, priv ci.PrivKey, kbook pstore.KeyBook, key string, val []byte, eol time.Time, exp error) {
	t.Helper()

	validator := IpnsValidator{kbook}

	p := path.Path("/ipfs/QmfM2r8seH2GiRaC4esTjeraXEachRt8ZsSeGaWTPLyMoG")
	entry, err := CreateRoutingEntryData(priv, p, 1, eol)
	if err != nil {
		t.Fatal(err)
	}

	data := val
	if data == nil {
		data, err = proto.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
	}
	err = validator.Validate(key, data)
	if err != exp {
		params := fmt.Sprintf("key: %s\neol: %s\n", key, eol)
		if exp == nil {
			t.Fatalf("Unexpected error %s for params %s", err, params)
		} else if err == nil {
			t.Fatalf("Expected error %s but there was no error for params %s", exp, params)
		} else {
			t.Fatalf("Expected error %s but got %s for params %s", exp, err, params)
		}
	}
}

func TestValidator(t *testing.T) {
	ts := time.Now()

	priv, id, _, _ := genKeys(t)
	priv2, id2, _, _ := genKeys(t)
	kbook := pstore.NewPeerstore()
	kbook.AddPubKey(id, priv.GetPublic())
	emptyKbook := pstore.NewPeerstore()

	testValidatorCase(t, priv, kbook, "/ipns/"+string(id), nil, ts.Add(time.Hour), nil)
	testValidatorCase(t, priv, kbook, "/ipns/"+string(id), nil, ts.Add(time.Hour*-1), ErrExpiredRecord)
	testValidatorCase(t, priv, kbook, "/ipns/"+string(id), []byte("bad data"), ts.Add(time.Hour), ErrBadRecord)
	testValidatorCase(t, priv, kbook, "/ipns/"+"bad key", nil, ts.Add(time.Hour), ErrKeyFormat)
	testValidatorCase(t, priv, emptyKbook, "/ipns/"+string(id), nil, ts.Add(time.Hour), ErrPublicKeyNotFound)
	testValidatorCase(t, priv2, kbook, "/ipns/"+string(id2), nil, ts.Add(time.Hour), ErrPublicKeyNotFound)
	testValidatorCase(t, priv2, kbook, "/ipns/"+string(id), nil, ts.Add(time.Hour), ErrSignature)
	testValidatorCase(t, priv, kbook, "//"+string(id), nil, ts.Add(time.Hour), ErrInvalidPath)
	testValidatorCase(t, priv, kbook, "/wrong/"+string(id), nil, ts.Add(time.Hour), ErrInvalidPath)
}

func TestResolverValidation(t *testing.T) {
	ctx := context.Background()
	rid := testutil.RandIdentityOrFatal(t)
	dstore := dssync.MutexWrap(ds.NewMapDatastore())
	peerstore := pstore.NewPeerstore()

	vstore := newMockValueStore(rid, dstore, peerstore)
	resolver := NewIpnsResolver(vstore)

	// Create entry with expiry in one hour
	priv, id, _, ipnsDHTPath := genKeys(t)
	ts := time.Now()
	p := path.Path("/ipfs/QmfM2r8seH2GiRaC4esTjeraXEachRt8ZsSeGaWTPLyMoG")
	entry, err := CreateRoutingEntryData(priv, p, 1, ts.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	// Make peer's public key available in peer store
	err = peerstore.AddPubKey(id, priv.GetPublic())
	if err != nil {
		t.Fatal(err)
	}

	// Publish entry
	err = PublishEntry(ctx, vstore, ipnsDHTPath, entry)
	if err != nil {
		t.Fatal(err)
	}

	// Resolve entry
	resp, _, err := resolver.resolveOnce(ctx, id.Pretty(), opts.DefaultResolveOpts())
	if err != nil {
		t.Fatal(err)
	}
	if resp != p {
		t.Fatalf("Mismatch between published path %s and resolved path %s", p, resp)
	}

	// Create expired entry
	expiredEntry, err := CreateRoutingEntryData(priv, p, 1, ts.Add(-1*time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	// Publish entry
	err = PublishEntry(ctx, vstore, ipnsDHTPath, expiredEntry)
	if err != nil {
		t.Fatal(err)
	}

	// Record should fail validation because entry is expired
	_, _, err = resolver.resolveOnce(ctx, id.Pretty(), opts.DefaultResolveOpts())
	if err == nil {
		t.Fatal("ValidateIpnsRecord should have returned error")
	}

	// Create IPNS record path with a different private key
	priv2, id2, _, ipnsDHTPath2 := genKeys(t)

	// Make peer's public key available in peer store
	err = peerstore.AddPubKey(id2, priv2.GetPublic())
	if err != nil {
		t.Fatal(err)
	}

	// Publish entry
	err = PublishEntry(ctx, vstore, ipnsDHTPath2, entry)
	if err != nil {
		t.Fatal(err)
	}

	// Record should fail validation because public key defined by
	// ipns path doesn't match record signature
	_, _, err = resolver.resolveOnce(ctx, id2.Pretty(), opts.DefaultResolveOpts())
	if err == nil {
		t.Fatal("ValidateIpnsRecord should have failed signature verification")
	}

	// Publish entry without making public key available in peer store
	priv3, id3, pubkDHTPath3, ipnsDHTPath3 := genKeys(t)
	entry3, err := CreateRoutingEntryData(priv3, p, 1, ts.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	err = PublishEntry(ctx, vstore, ipnsDHTPath3, entry3)
	if err != nil {
		t.Fatal(err)
	}

	// Record should fail validation because public key is not available
	// in peer store or on network
	_, _, err = resolver.resolveOnce(ctx, id3.Pretty(), opts.DefaultResolveOpts())
	if err == nil {
		t.Fatal("ValidateIpnsRecord should have failed because public key was not found")
	}

	// Publish public key to the network
	err = PublishPublicKey(ctx, vstore, pubkDHTPath3, priv3.GetPublic())
	if err != nil {
		t.Fatal(err)
	}

	// Record should now pass validation because resolver will ensure
	// public key is available in the peer store by looking it up in
	// the DHT, which causes the DHT to fetch it and cache it in the
	// peer store
	_, _, err = resolver.resolveOnce(ctx, id3.Pretty(), opts.DefaultResolveOpts())
	if err != nil {
		t.Fatal(err)
	}
}

func genKeys(t *testing.T) (ci.PrivKey, peer.ID, string, string) {
	sr := u.NewTimeSeededRand()
	priv, _, err := ci.GenerateKeyPairWithReader(ci.RSA, 1024, sr)
	if err != nil {
		t.Fatal(err)
	}

	// Create entry with expiry in one hour
	pid, err := peer.IDFromPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pubkDHTPath, ipnsDHTPath := IpnsKeysForID(pid)

	return priv, pid, pubkDHTPath, ipnsDHTPath
}

type mockValueStore struct {
	r         routing.ValueStore
	kbook     pstore.KeyBook
	Validator record.Validator
}

func newMockValueStore(id testutil.Identity, dstore ds.Datastore, kbook pstore.KeyBook) *mockValueStore {
	serv := mockrouting.NewServer()
	r := serv.ClientWithDatastore(context.Background(), id, dstore)
	return &mockValueStore{r, kbook, record.NamespacedValidator{
		"ipns": IpnsValidator{kbook},
		"pk":   record.PublicKeyValidator{},
	}}
}

func (m *mockValueStore) GetValue(ctx context.Context, k string, opts ...ropts.Option) ([]byte, error) {
	data, err := m.r.GetValue(ctx, k, opts...)
	if err != nil {
		return data, err
	}

	if err = m.Validator.Validate(k, data); err != nil {
		return nil, err
	}

	return data, err
}

func (m *mockValueStore) GetPublicKey(ctx context.Context, p peer.ID) (ci.PubKey, error) {
	pk := m.kbook.PubKey(p)
	if pk != nil {
		return pk, nil
	}

	pkkey := routing.KeyForPublicKey(p)
	val, err := m.GetValue(ctx, pkkey)
	if err != nil {
		return nil, err
	}

	pk, err = ci.UnmarshalPublicKey(val)
	if err != nil {
		return nil, err
	}

	return pk, m.kbook.AddPubKey(p, pk)
}

func (m *mockValueStore) PutValue(ctx context.Context, k string, d []byte, opts ...ropts.Option) error {
	return m.r.PutValue(ctx, k, d, opts...)
}
