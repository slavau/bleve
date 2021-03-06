//  Copyright (c) 2014 Couchbase, Inc.
//  Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
//  except in compliance with the License. You may obtain a copy of the License at
//    http://www.apache.org/licenses/LICENSE-2.0
//  Unless required by applicable law or agreed to in writing, software distributed under the
//  License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
//  either express or implied. See the License for the specific language governing permissions
//  and limitations under the License.

package upside_down

import (
	"bytes"
	"sort"

	"github.com/blevesearch/bleve/index/store"
)

// the functions in this file are only intended to be used by
// the bleve_dump utility and the debug http handlers
// if your application relies on them, you're doing something wrong
// they may change or be removed at any time

func (udc *UpsideDownCouch) dumpPrefix(kvreader store.KVReader, rv chan interface{}, prefix []byte) {
	start := prefix
	if start == nil {
		start = []byte{0}
	}
	it := kvreader.Iterator(start)
	defer func() {
		cerr := it.Close()
		if cerr != nil {
			rv <- cerr
		}
	}()
	key, val, valid := it.Current()
	for valid {

		if prefix != nil && !bytes.HasPrefix(key, prefix) {
			break
		}

		row, err := ParseFromKeyValue(key, val)
		if err != nil {
			rv <- err
			return
		}
		rv <- row

		it.Next()
		key, val, valid = it.Current()
	}
}

func (udc *UpsideDownCouch) DumpAll() chan interface{} {
	rv := make(chan interface{})
	go func() {
		defer close(rv)

		// start an isolated reader for use during the dump
		kvreader, err := udc.store.Reader()
		if err != nil {
			rv <- err
			return
		}
		defer func() {
			cerr := kvreader.Close()
			if cerr != nil {
				rv <- cerr
			}
		}()

		udc.dumpPrefix(kvreader, rv, nil)
	}()
	return rv
}

func (udc *UpsideDownCouch) DumpFields() chan interface{} {
	rv := make(chan interface{})
	go func() {
		defer close(rv)

		// start an isolated reader for use during the dump
		kvreader, err := udc.store.Reader()
		if err != nil {
			rv <- err
			return
		}
		defer func() {
			cerr := kvreader.Close()
			if cerr != nil {
				rv <- cerr
			}
		}()

		udc.dumpPrefix(kvreader, rv, []byte{'f'})
	}()
	return rv
}

type keyset [][]byte

func (k keyset) Len() int           { return len(k) }
func (k keyset) Swap(i, j int)      { k[i], k[j] = k[j], k[i] }
func (k keyset) Less(i, j int) bool { return bytes.Compare(k[i], k[j]) < 0 }

// DumpDoc returns all rows in the index related to this doc id
func (udc *UpsideDownCouch) DumpDoc(id string) chan interface{} {
	rv := make(chan interface{})

	go func() {
		defer close(rv)

		// start an isolated reader for use during the dump
		kvreader, err := udc.store.Reader()
		if err != nil {
			rv <- err
			return
		}
		defer func() {
			cerr := kvreader.Close()
			if cerr != nil {
				rv <- cerr
			}
		}()

		back, err := udc.backIndexRowForDoc(kvreader, id)
		if err != nil {
			rv <- err
			return
		}

		// no such doc
		if back == nil {
			return
		}
		// build sorted list of term keys
		keys := make(keyset, 0)
		for _, entry := range back.termEntries {
			tfr := NewTermFrequencyRow([]byte(*entry.Term), uint16(*entry.Field), id, 0, 0)
			key := tfr.Key()
			keys = append(keys, key)
		}
		sort.Sort(keys)

		// first add all the stored rows
		storedRowPrefix := NewStoredRow(id, 0, []uint64{}, 'x', []byte{}).ScanPrefixForDoc()
		udc.dumpPrefix(kvreader, rv, storedRowPrefix)

		// now walk term keys in order and add them as well
		if len(keys) > 0 {
			it := kvreader.Iterator(keys[0])
			defer func() {
				cerr := it.Close()
				if cerr != nil {
					rv <- cerr
				}
			}()

			for _, key := range keys {
				it.Seek(key)
				rkey, rval, valid := it.Current()
				if !valid {
					break
				}
				row, err := ParseFromKeyValue(rkey, rval)
				if err != nil {
					rv <- err
					return
				}
				rv <- row
			}
		}
	}()

	return rv
}
