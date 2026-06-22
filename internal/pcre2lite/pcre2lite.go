// Package pcre2lite is the cgo core of go-pcre2-lite. It embeds a trimmed
// PCRE2 8-bit interpreter (JIT permanently disabled) and exposes a small,
// byte-oriented, concurrency-safe Go API over a stable C ABI.
//
// All offsets are UTF-8 byte offsets, never rune indices. A compiled *Regexp
// is safe for concurrent matching; each match borrows its own scratch from an
// internal pool so no per-match state is shared.
package pcre2lite

/*
#cgo CFLAGS: -I${SRCDIR} -DPCRE2_CODE_UNIT_WIDTH=8 -DHAVE_CONFIG_H -DPCRE2_STATIC
#cgo CFLAGS: -std=c99 -O2 -fvisibility=hidden
#include <stdlib.h>
#include "wrapper.h"
*/
import "C"

import (
	"runtime"
	"sync"
	"unsafe"
)

// cUnset mirrors PCRE2_UNSET (SIZE_MAX) used to mark non-participating groups.
var cUnset = ^C.size_t(0)

// Version returns a descriptive version string for the embedded PCRE2 build.
func Version() string {
	return C.GoString(C.p2l_version())
}

// Regexp is a compiled pattern. It is safe for concurrent use by multiple
// goroutines. Call Close to release the underlying C memory; a finalizer acts
// only as a leak safety net.
type Regexp struct {
	mu     sync.RWMutex
	closed bool

	ptr  *C.p2l_regex
	pool scratchPool

	pattern    string
	utf        bool
	groupCount int // number of groups including group 0

	nameToNumber map[string]int
	numberToName map[int]string
	orderedNames []string
}

// Compile compiles pattern with the given options.
func Compile(pattern string, opts CompileOptions) (*Regexp, error) {
	pb := []byte(pattern)
	var cerr C.p2l_error
	ptr := C.p2l_compile(bytePtr(pb), C.size_t(len(pb)), opts.cmask(),
		C.uint32_t(opts.MatchLimit), C.uint32_t(opts.DepthLimit), &cerr)
	runtime.KeepAlive(pb)
	if ptr == nil {
		return nil, &CompileError{
			Pattern: pattern,
			Code:    int(cerr.code),
			Offset:  int(cerr.offset),
			Message: C.GoString(&cerr.message[0]),
		}
	}

	r := &Regexp{
		ptr:        ptr,
		pattern:    pattern,
		utf:        opts.UTF,
		groupCount: int(C.p2l_capture_count(ptr)),
	}
	r.pool.re = ptr

	if nc := int(C.p2l_name_count(ptr)); nc > 0 {
		r.nameToNumber = make(map[string]int, nc)
		r.numberToName = make(map[int]string, nc)
		buf := make([]C.char, 256)
		for i := 0; i < nc; i++ {
			var num C.uint32_t
			n := C.p2l_group_name(ptr, C.size_t(i), &buf[0], C.size_t(len(buf)), &num)
			if n < 0 {
				continue
			}
			name := C.GoStringN(&buf[0], n)
			r.nameToNumber[name] = int(num)
			r.numberToName[int(num)] = name
			r.orderedNames = append(r.orderedNames, name)
		}
	}

	runtime.SetFinalizer(r, (*Regexp).finalize)
	return r, nil
}

// MustCompile is like Compile but panics on error.
func MustCompile(pattern string, opts CompileOptions) *Regexp {
	re, err := Compile(pattern, opts)
	if err != nil {
		panic(err)
	}
	return re
}

func (o CompileOptions) cmask() C.uint32_t {
	var m C.uint32_t
	if o.UTF {
		m |= C.uint32_t(C.P2L_OPT_UTF)
	}
	if o.UCP {
		m |= C.uint32_t(C.P2L_OPT_UCP)
	}
	if o.Caseless {
		m |= C.uint32_t(C.P2L_OPT_CASELESS)
	}
	if o.Multiline {
		m |= C.uint32_t(C.P2L_OPT_MULTILINE)
	}
	if o.DotAll {
		m |= C.uint32_t(C.P2L_OPT_DOTALL)
	}
	if o.Extended {
		m |= C.uint32_t(C.P2L_OPT_EXTENDED)
	}
	if o.Ungreedy {
		m |= C.uint32_t(C.P2L_OPT_UNGREEDY)
	}
	if o.Anchored {
		m |= C.uint32_t(C.P2L_OPT_ANCHORED)
	}
	if o.DollarEndOnly {
		m |= C.uint32_t(C.P2L_OPT_DOLLAR_ENDONLY)
	}
	if o.FirstLine {
		m |= C.uint32_t(C.P2L_OPT_FIRSTLINE)
	}
	if o.NoAutoCapture {
		m |= C.uint32_t(C.P2L_OPT_NO_AUTO_CAPTURE)
	}
	if o.EndAnchored {
		m |= C.uint32_t(C.P2L_OPT_ENDANCHORED)
	}
	if o.AllowEmpty {
		m |= C.uint32_t(C.P2L_OPT_ALLOW_EMPTY_CLASS)
	}
	if o.DupNames {
		m |= C.uint32_t(C.P2L_OPT_DUPNAMES)
	}
	if o.NeverUCP {
		m |= C.uint32_t(C.P2L_OPT_NEVER_UCP)
	}
	return m
}

// Match reports whether input contains any match. It performs no allocation in
// the common path (boolean fast path).
func (r *Regexp) Match(input []byte) (bool, error) {
	rc, err := r.runMatch(input, 0, 0, nil)
	if err != nil {
		return false, err
	}
	return rc > 0, nil
}

// MatchString is the string form of Match. It does not copy the input.
func (r *Regexp) MatchString(input string) (bool, error) {
	return r.Match(stringBytes(input))
}

// Find returns the leftmost match at or after the byte offset start, or nil if
// there is no match.
func (r *Regexp) Find(input []byte, start int) (*Match, error) {
	return r.FindFrom(input, start, 0)
}

// FindFrom is Find with explicit per-call match options.
func (r *Regexp) FindFrom(input []byte, start int, opts MatchOption) (*Match, error) {
	if start < 0 {
		start = 0
	}
	if start > len(input) {
		return nil, nil
	}
	spans := make([]C.p2l_span, r.groupCount)
	rc, err := r.runMatch(input, start, opts, spans)
	if err != nil {
		return nil, err
	}
	if rc == 0 {
		return nil, nil
	}
	return &Match{Input: input, Groups: spansToGo(spans)}, nil
}

// findAllBatch is the number of matches gathered per cgo call by FindAll and
// Iter. It bounds the temporary span buffer (findAllBatch*groupCount spans) and
// amortizes the cgo boundary cost across many small matches.
const findAllBatch = 256

// FindAll returns successive non-overlapping matches. limit < 0 means all;
// limit == 0 returns nil. Empty matches are handled per the PCRE2 convention.
//
// Matching is performed in batches inside a single cgo call each, so the number
// of C round trips is roughly ceil(N/findAllBatch) rather than N. Each batch
// decodes into one backing []Span, so allocations scale with the number of
// batches, not the number of matches.
func (r *Regexp) FindAll(input []byte, limit int) ([]Match, error) {
	if limit == 0 {
		return nil, nil
	}
	stride := r.groupCount
	batch := findAllBatch
	if limit > 0 && limit < batch {
		batch = limit
	}
	buf := make([]C.p2l_span, batch*stride)

	var out []Match
	start := 0
	var opts MatchOption
	for {
		n, more, next, nextOpts, err := r.runMatchAll(input, start, opts, buf, batch)
		if err != nil {
			return out, err
		}
		if n > 0 {
			backing := make([]Span, n*stride)
			for mi := 0; mi < n; mi++ {
				base := mi * stride
				decodeSpans(buf[base:base+stride], backing[base:base+stride])
				out = append(out, Match{Input: input, Groups: backing[base : base+stride : base+stride]})
				if limit > 0 && len(out) >= limit {
					return out, nil
				}
			}
		}
		if !more {
			break
		}
		start = next
		opts = nextOpts
	}
	return out, nil
}

// runMatchAll performs one batched C match call, returning the number of matches
// written into buf, whether more remain, and the resume cursor/options. The read
// lock is held for the whole batch so Close waits for it to finish.
func (r *Regexp) runMatchAll(subject []byte, start int, opts MatchOption, buf []C.p2l_span, maxMatches int) (n int, more bool, next int, nextOpts MatchOption, err error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return 0, false, 0, 0, ErrClosed
	}
	ms := r.pool.get()
	if ms == nil {
		return 0, false, 0, 0, ErrNoMemory
	}

	rc := C.p2l_match_all(r.ptr, bytePtr(subject), C.size_t(len(subject)),
		C.size_t(start), C.uint32_t(opts), ms.c,
		&buf[0], C.size_t(len(buf)), C.size_t(maxMatches),
		&ms.matchCnt, &ms.nextStart, &ms.nextOpts, &ms.cerr)
	runtime.KeepAlive(subject)

	n = int(ms.matchCnt)
	next = int(ms.nextStart)
	nextOpts = MatchOption(ms.nextOpts)
	if int(rc) < 0 {
		err = translateMatchError(int(rc), &ms.cerr)
	} else {
		more = rc == 1
	}
	r.pool.put(ms)
	return n, more, next, nextOpts, err
}

// Iter is a stateful, batched matcher over a single subject. It pulls matches in
// chunks (one cgo call per chunk) so iterating many small matches avoids a cgo
// round trip per match. An Iter is not safe for concurrent use; create one per
// goroutine. Each chunk decodes into a fresh backing slice, so a *Match returned
// by Next stays valid indefinitely (it does not alias a buffer reused later).
type Iter struct {
	r      *Regexp
	input  []byte
	stride int
	buf    []C.p2l_span // reusable C-span scratch for one chunk

	backing []Span // backing for the current chunk only (fresh each fill)
	n       int    // matches decoded in the current chunk
	pos     int    // next match index within the current chunk
	done    bool

	start int
	opts  MatchOption
	err   error
}

// NewIter returns a batched iterator over input starting at byte offset start
// with the given resume options (pass 0 for a fresh scan).
func (r *Regexp) NewIter(input []byte, start int, opts MatchOption) *Iter {
	if start < 0 {
		start = 0
	}
	stride := r.groupCount
	return &Iter{
		r:      r,
		input:  input,
		stride: stride,
		buf:    make([]C.p2l_span, findAllBatch*stride),
		start:  start,
		opts:   opts,
	}
}

// Next returns the next match, or nil when the input is exhausted or an error
// occurred (check Err).
func (it *Iter) Next() *Match {
	if it.pos >= it.n {
		if it.done || it.err != nil {
			return nil
		}
		if !it.fill() || it.n == 0 {
			return nil
		}
	}
	base := it.pos * it.stride
	it.pos++
	return &Match{Input: it.input, Groups: it.backing[base : base+it.stride : base+it.stride]}
}

// Err returns the first error encountered during iteration, if any.
func (it *Iter) Err() error { return it.err }

func (it *Iter) fill() bool {
	n, more, next, nextOpts, err := it.r.runMatchAll(it.input, it.start, it.opts, it.buf, findAllBatch)
	if err != nil {
		it.err = err
		it.done = true
		return false
	}
	if n > 0 {
		it.backing = make([]Span, n*it.stride)
		for mi := 0; mi < n; mi++ {
			base := mi * it.stride
			decodeSpans(it.buf[base:base+it.stride], it.backing[base:base+it.stride])
		}
	}
	it.n = n
	it.pos = 0
	it.start = next
	it.opts = nextOpts
	if !more {
		it.done = true
	}
	return true
}

// runMatch executes a single C match. The read lock is held across the C call
// so that Close blocks until all in-flight matches complete. The per-call C
// out-parameters (error and span count) live inside the pooled scratch so the
// boolean path performs zero Go heap allocations.
func (r *Regexp) runMatch(subject []byte, start int, opts MatchOption, spans []C.p2l_span) (int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.closed {
		return 0, ErrClosed
	}

	ms := r.pool.get()
	if ms == nil {
		return 0, ErrNoMemory
	}

	var (
		spanPtr *C.p2l_span
		spanCap C.size_t
	)
	if len(spans) > 0 {
		spanPtr = &spans[0]
		spanCap = C.size_t(len(spans))
	}

	rc := C.p2l_match(r.ptr, bytePtr(subject), C.size_t(len(subject)),
		C.size_t(start), C.uint32_t(opts), ms.c,
		spanPtr, spanCap, &ms.spanCnt, &ms.cerr)
	runtime.KeepAlive(subject)

	var err error
	if int(rc) < 0 {
		err = translateMatchError(int(rc), &ms.cerr)
	}
	r.pool.put(ms)
	return int(rc), err
}

func translateMatchError(rc int, cerr *C.p2l_error) error {
	switch rc {
	case int(C.P2L_ERR_MATCHLIMIT):
		return ErrMatchLimit
	case int(C.P2L_ERR_DEPTHLIMIT):
		return ErrDepthLimit
	case int(C.P2L_ERR_BADUTF):
		return ErrBadUTF
	case int(C.P2L_ERR_NOMEMORY):
		return ErrNoMemory
	case int(C.P2L_ERR_PARTIAL):
		return ErrPartial
	case int(C.P2L_ERR_SHORTBUF):
		return ErrShortBuffer
	default:
		return &MatchError{
			Code:    int(cerr.code),
			Message: C.GoString(&cerr.message[0]),
			kind:    ErrInternal,
		}
	}
}

// Close releases the C resources held by the Regexp. It is idempotent and safe
// to call concurrently with matches: it waits for in-flight matches to finish
// (via the write lock) before freeing. Matches started after Close return
// ErrClosed.
func (r *Regexp) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	r.pool.freeAll()
	if r.ptr != nil {
		C.p2l_free(r.ptr)
		r.ptr = nil
		r.pool.re = nil
	}
	runtime.SetFinalizer(r, nil)
	return nil
}

func (r *Regexp) finalize() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	r.closed = true
	r.pool.freeAll()
	if r.ptr != nil {
		C.p2l_free(r.ptr)
		r.ptr = nil
		r.pool.re = nil
	}
}

// CaptureCount returns the number of groups including group 0 (the whole match).
func (r *Regexp) CaptureCount() int { return r.groupCount }

// SubexpCount returns the number of capturing subpatterns, excluding group 0.
func (r *Regexp) SubexpCount() int { return r.groupCount - 1 }

// String returns the original pattern source.
func (r *Regexp) String() string { return r.pattern }

// UTF reports whether the pattern was compiled in UTF mode.
func (r *Regexp) UTF() bool { return r.utf }

// NamedGroupNumber returns the group number for a named group.
func (r *Regexp) NamedGroupNumber(name string) (int, bool) {
	n, ok := r.nameToNumber[name]
	return n, ok
}

// NumberedGroupName returns the name of a numbered group, if it has one.
func (r *Regexp) NumberedGroupName(num int) (string, bool) {
	s, ok := r.numberToName[num]
	return s, ok
}

// OrderedNames returns the named groups in PCRE2 name-table order.
func (r *Regexp) OrderedNames() []string {
	return append([]string(nil), r.orderedNames...)
}

func spansToGo(spans []C.p2l_span) []Span {
	out := make([]Span, len(spans))
	decodeSpans(spans, out)
	return out
}

// decodeSpans converts a chunk of C spans into Go Spans in place (no allocation).
// src and dst must have the same length.
func decodeSpans(src []C.p2l_span, dst []Span) {
	for i := range src {
		if src[i].start == cUnset || src[i].end == cUnset {
			dst[i] = Span{SpanUnset, SpanUnset}
			continue
		}
		dst[i] = Span{Start: int(src[i].start), End: int(src[i].end)}
	}
}

// matchScratch bundles a reusable C match-data block with the per-call C
// out-parameters. Keeping cerr and spanCnt here (rather than as locals whose
// address is taken on every call) keeps the boolean match path allocation-free.
type matchScratch struct {
	c       *C.p2l_scratch
	cerr    C.p2l_error
	spanCnt C.size_t

	// Out-parameters for the batched p2l_match_all path. Kept here so their
	// addresses point into the pooled scratch rather than escaping locals.
	matchCnt  C.size_t
	nextStart C.size_t
	nextOpts  C.uint32_t
}

// scratchPool reuses match scratches to avoid per-match C allocation. It also
// tracks every allocated scratch so Close can free them deterministically.
type scratchPool struct {
	mu   sync.Mutex
	free []*matchScratch
	all  []*matchScratch
	re   *C.p2l_regex
}

func (p *scratchPool) get() *matchScratch {
	p.mu.Lock()
	if n := len(p.free); n > 0 {
		s := p.free[n-1]
		p.free = p.free[:n-1]
		p.mu.Unlock()
		return s
	}
	re := p.re
	p.mu.Unlock()
	if re == nil {
		return nil
	}
	c := C.p2l_scratch_new(re)
	if c == nil {
		return nil
	}
	ms := &matchScratch{c: c}
	p.mu.Lock()
	p.all = append(p.all, ms)
	p.mu.Unlock()
	return ms
}

func (p *scratchPool) put(s *matchScratch) {
	if s == nil {
		return
	}
	p.mu.Lock()
	p.free = append(p.free, s)
	p.mu.Unlock()
}

func (p *scratchPool) freeAll() {
	p.mu.Lock()
	for _, s := range p.all {
		C.p2l_scratch_free(s.c)
	}
	p.all = nil
	p.free = nil
	p.mu.Unlock()
}

func bytePtr(b []byte) *C.uint8_t {
	if len(b) == 0 {
		return nil
	}
	return (*C.uint8_t)(unsafe.Pointer(&b[0]))
}

// stringBytes returns a read-only []byte view of s without copying. The result
// must never be mutated and is only passed to C for reading during a call.
func stringBytes(s string) []byte {
	if len(s) == 0 {
		return nil
	}
	return unsafe.Slice(unsafe.StringData(s), len(s))
}
