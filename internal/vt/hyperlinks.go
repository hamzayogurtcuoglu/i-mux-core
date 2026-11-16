// Copyright (c) 2021 Will <author@will.party>
// Copyright (c) 2022 Jeff Chiang <pobomp@gmail.com>
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF MIND, USE, DATA OR PROFITS, WHETHER
// IN AN ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING
// OUT OF OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package vt

import (
	"strconv"
	"strings"
	"sync/atomic"
)

// The hyperlink table, ported from tmux 3.8's hyperlinks.c. tmux limits hyperlinks across the
// whole server; here the limit is per screen.

// Hyperlink limits from hyperlinks.c.
const (
	MaxHyperlinks   = 5000
	MaxHyperlinkURI = 1024
)

type hyperlinkKey struct {
	internalID string
	uri        string
}

type hyperlinkEntry struct {
	inner      uint32
	internalID string
	externalID string
	uri        string
}

// nextExternalID numbers hyperlinks across the whole process, as tmux numbers them across its
// server: the id a client terminal is sent is "tmux" and the number in hexadecimal.
var nextExternalID atomic.Int64

// Hyperlinks stores a screen's OSC 8 hyperlinks, mapping per-cell integer link
// IDs to (URI, id) pairs with FIFO eviction at MaxHyperlinks.
type Hyperlinks struct {
	nextInner uint32
	list      []*hyperlinkEntry
	byInner   map[uint32]*hyperlinkEntry
	byURI     map[hyperlinkKey]*hyperlinkEntry
}

// NewHyperlinks returns an empty hyperlink table.
func NewHyperlinks() *Hyperlinks {
	hl := &Hyperlinks{}
	hl.Reset()
	return hl
}

// Reset clears all stored hyperlinks and resets the inner counter to 1.
func (hl *Hyperlinks) Reset() {
	hl.nextInner = 1
	hl.list = nil
	hl.byInner = make(map[uint32]*hyperlinkEntry)
	hl.byURI = make(map[hyperlinkKey]*hyperlinkEntry)
}

// Put stores or reuses an OSC 8 hyperlink and returns its cell link ID (0 if
// the escaped URI exceeds MaxHyperlinkURI). Named hyperlinks (internalID != "")
// with identical escaped (id, uri) share the same inner ID; anonymous
// hyperlinks always receive a fresh inner ID.
func (hl *Hyperlinks) Put(uriIn, internalIDIn string) uint32 {
	uri := utf8Strvis(uriIn, visOctal|visCStyle)
	if len(uri) > MaxHyperlinkURI {
		return 0
	}
	var internalID string
	if internalIDIn != "" {
		internalID = utf8Strvis(internalIDIn, visOctal|visCStyle)
		if hlu, ok := hl.byURI[hyperlinkKey{internalID: internalID, uri: uri}]; ok {
			return hlu.inner
		}
	}
	hlu := &hyperlinkEntry{
		inner:      hl.nextInner,
		internalID: internalID,
		externalID: "tmux" + strings.ToUpper(strconv.FormatInt(nextExternalID.Add(1), 16)),
		uri:        uri,
	}
	hl.nextInner++
	hl.list = append(hl.list, hlu)
	hl.byInner[hlu.inner] = hlu
	if internalID != "" {
		hl.byURI[hyperlinkKey{internalID: internalID, uri: uri}] = hlu
	}
	// As tmux: once the count reaches the limit, the oldest goes.
	if len(hl.list) == MaxHyperlinks {
		old := hl.list[0]
		hl.list = hl.list[1:]
		delete(hl.byInner, old.inner)
		if old.internalID != "" {
			delete(hl.byURI, hyperlinkKey{internalID: old.internalID, uri: old.uri})
		}
	}
	return hlu.inner
}

// Get looks up a cell link ID and returns its escaped URI and id.
func (hl *Hyperlinks) Get(inner uint32) (uri, internalID string, ok bool) {
	if hl == nil || inner == 0 {
		return "", "", false
	}
	hlu, ok := hl.byInner[inner]
	if !ok {
		return "", "", false
	}
	return hlu.uri, hlu.internalID, true
}

// External looks up a cell link ID and returns its escaped URI and the id a client terminal is
// sent for it (tmux's external id: unique across the server, given to every link, named or not).
func (hl *Hyperlinks) External(inner uint32) (uri, externalID string, ok bool) {
	if hl == nil || inner == 0 {
		return "", "", false
	}
	hlu, ok := hl.byInner[inner]
	if !ok {
		return "", "", false
	}
	return hlu.uri, hlu.externalID, true
}
