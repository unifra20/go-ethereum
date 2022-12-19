// Copyright 2015 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package trie

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"os"
	"runtime"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	zktrie "github.com/scroll-tech/zktrie/trie"
	zkt "github.com/scroll-tech/zktrie/types"

	"github.com/scroll-tech/go-ethereum/common"
	"github.com/scroll-tech/go-ethereum/ethdb/leveldb"
	"github.com/scroll-tech/go-ethereum/ethdb/memorydb"
)

func newEmptyZkTrie() *ZkTrie {
	trie, _ := NewZkTrie(common.Hash{}, NewZktrieDatabase(memorydb.New()))
	return trie
}

// makeTestSecureTrie creates a large enough secure trie for testing.
func makeTestZkTrie() (*ZktrieDatabase, *ZkTrie, map[string][]byte) {
	// Create an empty trie
	triedb := NewZktrieDatabase(memorydb.New())
	trie, _ := NewZkTrie(common.Hash{}, triedb)

	// Fill it with some arbitrary data
	content := make(map[string][]byte)
	for i := byte(0); i < 255; i++ {
		// Map the same data under multiple keys
		key, val := common.LeftPadBytes([]byte{1, i}, 32), bytes.Repeat([]byte{i}, 32)
		content[string(key)] = val
		trie.Update(key, val)

		key, val = common.LeftPadBytes([]byte{2, i}, 32), bytes.Repeat([]byte{i}, 32)
		content[string(key)] = val
		trie.Update(key, val)

		// Add some other data to inflate the trie
		for j := byte(3); j < 13; j++ {
			key, val = common.LeftPadBytes([]byte{j, i}, 32), bytes.Repeat([]byte{j, i}, 16)
			content[string(key)] = val
			trie.Update(key, val)
		}
	}
	trie.Commit(nil)

	// Return the generated trie
	return triedb, trie, content
}

func TestZktrieDelete(t *testing.T) {
	t.Skip("var-len kv not supported")
	trie := newEmptyZkTrie()
	vals := []struct{ k, v string }{
		{"do", "verb"},
		{"ether", "wookiedoo"},
		{"horse", "stallion"},
		{"shaman", "horse"},
		{"doge", "coin"},
		{"ether", ""},
		{"dog", "puppy"},
		{"shaman", ""},
	}
	for _, val := range vals {
		if val.v != "" {
			trie.Update([]byte(val.k), []byte(val.v))
		} else {
			trie.Delete([]byte(val.k))
		}
	}
	hash := trie.Hash()
	exp := common.HexToHash("29b235a58c3c25ab83010c327d5932bcf05324b7d6b1185e650798034783ca9d")
	if hash != exp {
		t.Errorf("expected %x got %x", exp, hash)
	}
}

func TestZktrieGetKey(t *testing.T) {
	trie := newEmptyZkTrie()
	key := []byte("0a1b2c3d4e5f6g7h8i9j0a1b2c3d4e5f")
	value := []byte("9j8i7h6g5f4e3d2c1b0a9j8i7h6g5f4e")
	value2 := []byte("9j8i7h6g5f4e3d2c1b0a9j8i7h6g5f4e")
	trie.Update(key, value)

	trie.Commit(nil)

	kPreimage := zkt.NewByte32FromBytesPaddingZero(key)
	kHash, err := kPreimage.Hash()
	assert.Nil(t, err)

	actual := trie.Get(key)
	if !bytes.Equal(actual, value) {
		t.Errorf("Get did not return bar")
	}
	if k := trie.GetKey(kHash.Bytes()); !bytes.Equal(k, key) {
		t.Errorf("GetKey returned %q, want %q", k, key)
	}
	trie.Update(key, value2)
	actual = trie.Get(key)
	if !bytes.Equal(actual, value2) {
		t.Errorf("Get did not return bar")
	}
}

func TestZkTrieConcurrency(t *testing.T) {
	t.Skip("ZkTrieConcurrency not supported")
	// Create an initial trie and copy if for concurrent access
	_, trie, _ := makeTestZkTrie()

	threads := runtime.NumCPU()
	tries := make([]*ZkTrie, threads)
	for i := 0; i < threads; i++ {
		cpy := *trie
		tries[i] = &cpy
	}
	// Start a batch of goroutines interactng with the trie
	pend := new(sync.WaitGroup)
	pend.Add(threads)
	for i := 0; i < threads; i++ {
		go func(index int) {
			defer pend.Done()

			for j := byte(0); j < 255; j++ {
				// Map the same data under multiple keys
				key, val := common.LeftPadBytes([]byte{byte(index), 1, j}, 32), bytes.Repeat([]byte{j}, 32)
				tries[index].Update(key, val)

				key, val = common.LeftPadBytes([]byte{byte(index), 2, j}, 32), bytes.Repeat([]byte{j}, 32)
				tries[index].Update(key, val)

				// Add some other data to inflate the trie
				for k := byte(3); k < 13; k++ {
					key, val = common.LeftPadBytes([]byte{byte(index), k, j}, 32), bytes.Repeat([]byte{k, j}, 16)
					tries[index].Update(key, val)
				}
			}
			tries[index].Commit(nil)
		}(i)
	}
	// Wait for all threads to finish
	pend.Wait()
}

func tempDBZK(b *testing.B) (string, *Database) {
	dir, err := ioutil.TempDir("", "zktrie-bench")
	assert.NoError(b, err)

	diskdb, err := leveldb.New(dir, 256, 0, "", false)
	assert.NoError(b, err)
	config := &Config{Cache: 256, Preimages: true, Zktrie: true}
	return dir, NewDatabaseWithConfig(diskdb, config)
}

const benchElemCountZk = 10000

func BenchmarkZkTrieGet(b *testing.B) {
	_, tmpdb := tempDBZK(b)
	zkTrie, _ := NewZkTrie(common.Hash{}, NewZktrieDatabaseFromTriedb(tmpdb))
	defer func() {
		ldb := zkTrie.db.db.diskdb.(*leveldb.Database)
		ldb.Close()
		os.RemoveAll(ldb.Path())
	}()

	k := make([]byte, 32)
	for i := 0; i < benchElemCountZk; i++ {
		binary.LittleEndian.PutUint64(k, uint64(i))

		err := zkTrie.TryUpdate(k, k)
		assert.NoError(b, err)
	}

	zkTrie.Commit(nil)
	zkTrie.db.db.Commit(common.Hash{}, true, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		binary.LittleEndian.PutUint64(k, uint64(i%benchElemCountZk))
		_, err := zkTrie.TryGet(k)
		assert.NoError(b, err, common.Bytes2Hex(k))
	}
	b.StopTimer()
}

func BenchmarkZkTrieUpdate(b *testing.B) {
	_, tmpdb := tempDBZK(b)
	zkTrie, _ := NewZkTrie(common.Hash{}, NewZktrieDatabaseFromTriedb(tmpdb))
	defer func() {
		ldb := zkTrie.db.db.diskdb.(*leveldb.Database)
		ldb.Close()
		os.RemoveAll(ldb.Path())
	}()

	k := make([]byte, 32)
	v := make([]byte, 32)
	b.ReportAllocs()

	for i := 0; i < benchElemCountZk; i++ {
		binary.LittleEndian.PutUint64(k, uint64(i))
		err := zkTrie.TryUpdate(k, k)
		assert.NoError(b, err)
	}
	binary.LittleEndian.PutUint64(k, benchElemCountZk/2)

	zkTrie.Commit(nil)
	zkTrie.db.db.Commit(common.Hash{}, true, nil)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		binary.LittleEndian.PutUint64(k, uint64(i))
		binary.LittleEndian.PutUint64(v, 0xffffffff+uint64(i))
		err := zkTrie.TryUpdate(k, v)
		assert.NoError(b, err)
	}
	b.StopTimer()
}

func TestZktrieBatchUpdate(t *testing.T) {
	var expectedHash = []string{
		"0x03000ecf278f3c3309f2a3a091b4d20b5e01f2b4e8f5b2a44bd4e2e67aa9a3d5",
		"0x184f1ddad37cfd775697681e541249fa901a18e99e1149ffc5ac5317b4ee634f",
		"0x15038bd9094511ca2d948ef6baab8a215230f9232b3b0d4ec98cdeaef0a23b0e",
		"0x20d534c970d260d6d34ce804dda9c547467cfbfdcc0724fcbe06ab94ebdaaead",
		"0x1656f1a65cf751b9e5abd5fc26abeaf8d8bc14aa22fa2036be58e4ffd057374c",
		"0x2b544a0ba4489541d5fe29d6cdaf81c3da0f60c4efe3649895d0e2b80f769914",
		"0x08a8112469472c978749285878f1720fdf7cc36ce0dbb37638caaed699aa5aad",
	}
	key := make([]byte, 32)
	value := make([]byte, 32)
	trie1 := newEmptyZkTrie()
	trie2 := newEmptyZkTrie()

	h0 := trie1.Hash()
	fmt.Println(h0.Hex())
	for i := 0; i < 1000; i++ {
		binary.LittleEndian.PutUint64(key, uint64(i))
		binary.LittleEndian.PutUint64(value, uint64(i))

		err := trie1.TryUpdate(key, value)
		assert.NoError(t, err)

		err = trie2.TryUpdate(key, value)
		assert.NoError(t, err)

		h, _, _ := trie1.Commit(nil)
		if i < len(expectedHash) {
			assert.Equal(t, expectedHash[i], h.Hex())
		}
	}
	assert.Equal(t, trie1.Hash().Hex(), trie2.Hash().Hex())
}

func TestZkTrieDelete(t *testing.T) {
	key := make([]byte, 32)
	value := make([]byte, 32)
	trie1 := newEmptyZkTrie()

	var count int = 6
	var hashes []common.Hash
	hashes = append(hashes, trie1.Hash())
	for i := 0; i < count; i++ {
		binary.LittleEndian.PutUint64(key, uint64(i))
		binary.LittleEndian.PutUint64(value, uint64(i))
		err := trie1.TryUpdate(key, value)
		assert.NoError(t, err)
		hashes = append(hashes, trie1.Hash())
	}

	binary.LittleEndian.PutUint64(key, uint64(0xffffff))
	err := trie1.TryDelete(key)
	assert.Equal(t, err, zktrie.ErrKeyNotFound)

	trie1.Commit(nil)
	trie1.Tree().PrintGraphViz(trie1.Tree().Root())
	for i := count - 1; i >= 0; i-- {
		binary.LittleEndian.PutUint64(key, uint64(i))
		err := trie1.TryDelete(key)
		assert.NoError(t, err)
		hash := trie1.Hash()
		assert.Equal(t, hash.Hex(), hashes[i].Hex())
	}
}
func BenchmarkHashparallel(b *testing.B) {
	key := make([]byte, 32)
	value := make([]byte, 32)
	trie1 := newEmptyZkTrie()

	for i := 0; i < 10000; i++ {
		binary.LittleEndian.PutUint64(key, uint64(i))
		err := trie1.TryUpdate(key, key)
		assert.NoError(b, err)
	}
	trie1.Commit(nil)

	var count int = 100
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		trie1.Commit(nil)
		for i := 0; i < count; i++ {
			binary.LittleEndian.PutUint64(key, uint64(i))
			binary.LittleEndian.PutUint64(value, uint64(j*count+i))
			err := trie1.TryUpdate(key, value)
			assert.NoError(b, err)
		}
		trie1.Hash()
	}
	b.StopTimer()
}
func BenchmarkHashLine(b *testing.B) {
	key := make([]byte, 32)
	value := make([]byte, 32)
	trie1 := newEmptyZkTrie()

	for i := 0; i < 10000; i++ {
		binary.LittleEndian.PutUint64(key, uint64(i))
		err := trie1.TryUpdate(key, key)
		assert.NoError(b, err)
	}
	trie1.Commit(nil)

	var count int = 99
	b.ResetTimer()
	for j := 0; j < b.N; j++ {
		trie1.Commit(nil)
		for i := 0; i < count; i++ {
			binary.LittleEndian.PutUint64(key, uint64(i))
			binary.LittleEndian.PutUint64(value, uint64(j*count+i))
			err := trie1.TryUpdate(key, value)
			assert.NoError(b, err)
		}
		trie1.Hash()
	}
	b.StopTimer()
}
