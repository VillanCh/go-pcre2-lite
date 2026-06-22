package pcre2lite

// #include "wrapper.h"
import "C"

// These helpers expose the raw C match-option flag values so tests can assert
// that the Go MatchOption constants stay in sync with the C ABI.

func cP2LMOptAnchored() uint32        { return uint32(C.P2L_MOPT_ANCHORED) }
func cP2LMOptNotBOL() uint32          { return uint32(C.P2L_MOPT_NOTBOL) }
func cP2LMOptNotEOL() uint32          { return uint32(C.P2L_MOPT_NOTEOL) }
func cP2LMOptNotEmpty() uint32        { return uint32(C.P2L_MOPT_NOTEMPTY) }
func cP2LMOptNotEmptyAtStart() uint32 { return uint32(C.P2L_MOPT_NOTEMPTY_ATSTART) }
func cP2LMOptEndAnchored() uint32     { return uint32(C.P2L_MOPT_ENDANCHORED) }
