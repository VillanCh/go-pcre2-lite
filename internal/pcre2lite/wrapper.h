/*
 * wrapper.h - stable, minimal C ABI over the trimmed PCRE2 8-bit interpreter.
 *
 * Design goals (see task section 8):
 *   - small and stable; decoupled from PCRE2 internal structures
 *   - never expose pcre2_code / pcre2_match_data to Go
 *   - one call returns the complete set of capture offsets
 *   - explicit ownership and lifetime
 *
 * JIT is permanently disabled: this ABI exposes no JIT entry points and the
 * underlying library is built with SUPPORT_JIT undefined.
 */
#ifndef P2L_WRAPPER_H
#define P2L_WRAPPER_H

#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/* Opaque compiled pattern. Read-only after a successful p2l_compile and safe
   for concurrent matching as long as each concurrent match uses its own
   p2l_scratch. */
typedef struct p2l_regex p2l_regex;

/* Opaque per-call match state (wraps a PCRE2 match data block). Not safe for
   concurrent use; create one per goroutine or pool them. */
typedef struct p2l_scratch p2l_scratch;

typedef struct {
    int    code;          /* PCRE2 error code (0 if none) */
    size_t offset;        /* error offset within the pattern (compile only) */
    char   message[256];  /* NUL-terminated human readable message */
} p2l_error;

/* A byte-offset half-open span [start, end). An unmatched optional group is
   reported with start == end == P2L_UNSET. */
typedef struct {
    size_t start;
    size_t end;
} p2l_span;

/* Sentinel marking an unset (non-participating) capture group. Mirrors
   PCRE2_UNSET (SIZE_MAX). */
#define P2L_UNSET ((size_t)-1)

/* Compile option bit flags. Mapped to PCRE2 compile options in wrapper.c. */
enum {
    P2L_OPT_UTF             = 1u << 0,  /* PCRE2_UTF */
    P2L_OPT_UCP             = 1u << 1,  /* PCRE2_UCP */
    P2L_OPT_CASELESS        = 1u << 2,  /* PCRE2_CASELESS */
    P2L_OPT_MULTILINE       = 1u << 3,  /* PCRE2_MULTILINE */
    P2L_OPT_DOTALL          = 1u << 4,  /* PCRE2_DOTALL */
    P2L_OPT_EXTENDED        = 1u << 5,  /* PCRE2_EXTENDED */
    P2L_OPT_UNGREEDY        = 1u << 6,  /* PCRE2_UNGREEDY */
    P2L_OPT_ANCHORED        = 1u << 7,  /* PCRE2_ANCHORED */
    P2L_OPT_DOLLAR_ENDONLY  = 1u << 8,  /* PCRE2_DOLLAR_ENDONLY */
    P2L_OPT_FIRSTLINE       = 1u << 9,  /* PCRE2_FIRSTLINE */
    P2L_OPT_NO_AUTO_CAPTURE = 1u << 10, /* PCRE2_NO_AUTO_CAPTURE */
    P2L_OPT_ENDANCHORED     = 1u << 11, /* PCRE2_ENDANCHORED */
    P2L_OPT_ALLOW_EMPTY_CLASS = 1u << 12, /* PCRE2_ALLOW_EMPTY_CLASS */
    P2L_OPT_DUPNAMES        = 1u << 13, /* PCRE2_DUPNAMES */
    P2L_OPT_NEVER_UCP       = 1u << 14, /* PCRE2_NEVER_UCP */
};

/* Match option bit flags. Mapped to PCRE2 match options in wrapper.c. */
enum {
    P2L_MOPT_ANCHORED         = 1u << 0, /* PCRE2_ANCHORED */
    P2L_MOPT_NOTBOL           = 1u << 1, /* PCRE2_NOTBOL */
    P2L_MOPT_NOTEOL           = 1u << 2, /* PCRE2_NOTEOL */
    P2L_MOPT_NOTEMPTY         = 1u << 3, /* PCRE2_NOTEMPTY */
    P2L_MOPT_NOTEMPTY_ATSTART = 1u << 4, /* PCRE2_NOTEMPTY_ATSTART */
    P2L_MOPT_ENDANCHORED      = 1u << 5, /* PCRE2_ENDANCHORED */
};

/* Negative result codes from p2l_match. P2L_RC_NOMATCH is returned as 0. */
enum {
    P2L_ERR_PARTIAL    = -2,
    P2L_ERR_BADUTF     = -3,
    P2L_ERR_MATCHLIMIT = -4,
    P2L_ERR_DEPTHLIMIT = -5,
    P2L_ERR_NOMEMORY   = -6,
    P2L_ERR_BADOPTION  = -7,
    P2L_ERR_INTERNAL   = -8,
    P2L_ERR_NULLARG    = -9,
    P2L_ERR_SHORTBUF   = -10,
};

/*
 * Compile a pattern. pattern uses an explicit length and may contain NUL bytes.
 * options is a bitwise OR of P2L_OPT_*. match_limit/depth_limit of 0 select the
 * library defaults. On failure returns NULL and fills *error.
 */
p2l_regex *p2l_compile(const uint8_t *pattern, size_t pattern_len,
                       uint32_t options, uint32_t match_limit,
                       uint32_t depth_limit, p2l_error *error);

void p2l_free(p2l_regex *regex);

/* Scratch lifecycle. A scratch is sized for a particular regex. */
p2l_scratch *p2l_scratch_new(const p2l_regex *regex);
void p2l_scratch_free(p2l_scratch *scratch);

/*
 * Run a single match starting at start_offset.
 *
 * Returns:
 *   > 0  match; the value is one more than the highest matched group number.
 *     0  no match.
 *   < 0  error or limit hit (one of P2L_ERR_*); *error may carry details.
 *
 * If spans != NULL and span_capacity is large enough, all capture offsets
 * (group 0..capture_count) are written and *span_count is set to the number of
 * groups. Pass spans == NULL for a boolean match (no offsets copied). If the
 * buffer is too small, returns P2L_ERR_SHORTBUF and sets *span_count to the
 * required size.
 *
 * scratch may be NULL, in which case a temporary match block is allocated for
 * the call. Providing a pooled scratch avoids per-call C allocation.
 */
int p2l_match(const p2l_regex *regex, const uint8_t *subject, size_t subject_len,
              size_t start_offset, uint32_t options, p2l_scratch *scratch,
              p2l_span *spans, size_t span_capacity, size_t *span_count,
              p2l_error *error);

/*
 * Find successive non-overlapping matches in a single call, amortizing the cgo
 * boundary cost across many small matches. Capture spans for each match are
 * written contiguously into spans (each match uses (capture_count+1) entries);
 * span_capacity is the total number of p2l_span the buffer can hold.
 *
 * Matching starts at start_offset with in_options (0 for a fresh scan; pass the
 * values returned via out_next_* to resume the next batch). On return:
 *   *out_match_count   = number of matches written this call
 *   *out_next_start    = byte offset to resume from
 *   *out_next_options  = match options to resume with
 *
 * Returns:
 *    1  buffer full; more matches may remain (call again with the out_next_*)
 *    0  input exhausted, no more matches
 *   <0  error or limit hit (one of P2L_ERR_*); *out_match_count holds the
 *       matches gathered before the error.
 *
 * Empty matches are handled per the standard PCRE2 global-match algorithm.
 */
int p2l_match_all(const p2l_regex *regex, const uint8_t *subject,
                  size_t subject_len, size_t start_offset, uint32_t in_options,
                  p2l_scratch *scratch, p2l_span *spans, size_t span_capacity,
                  size_t max_matches, size_t *out_match_count,
                  size_t *out_next_start, uint32_t *out_next_options,
                  p2l_error *error);

/* Metadata cached at compile time. */
size_t p2l_capture_count(const p2l_regex *regex); /* number of groups incl. 0 */
size_t p2l_name_count(const p2l_regex *regex);

/*
 * Enumerate named groups. index in [0, p2l_name_count). Writes the NUL
 * terminated group name into name_buf and the group number into *out_number.
 * Returns the name length (excluding NUL) on success, or a negative P2L_ERR_*.
 */
int p2l_group_name(const p2l_regex *regex, size_t index,
                   char *name_buf, size_t name_buf_len, uint32_t *out_number);

const char *p2l_version(void);

#ifdef __cplusplus
}
#endif

#endif /* P2L_WRAPPER_H */
