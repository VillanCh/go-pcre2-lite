/*
 * wrapper.c - implementation of the stable C ABI declared in wrapper.h.
 *
 * Built against the trimmed PCRE2 8-bit interpreter. PCRE2_CODE_UNIT_WIDTH is
 * defined to 8 by the cgo CFLAGS, so the unsuffixed pcre2_* macros resolve to
 * the _8 variants. JIT is never referenced.
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "pcre2.h"
#include "wrapper.h"

#define P2L_STRINGIFY2(x) #x
#define P2L_STRINGIFY(x) P2L_STRINGIFY2(x)

struct p2l_regex {
    pcre2_code          *code;
    pcre2_match_context *mcontext;   /* read-only during matching; may be NULL */
    uint32_t             capture_count;   /* capturing subpatterns, excluding 0 */
    uint32_t             name_count;
    uint32_t             name_entry_size;
    PCRE2_SPTR           name_table;      /* points into code, valid until free */
    int                  utf;             /* nonzero when compiled with PCRE2_UTF */
};

struct p2l_scratch {
    pcre2_match_data *md;
};

static void clear_error(p2l_error *error) {
    if (error != NULL) {
        error->code = 0;
        error->offset = 0;
        error->message[0] = '\0';
    }
}

static void fill_error(p2l_error *error, int code, size_t offset) {
    if (error == NULL) {
        return;
    }
    error->code = code;
    error->offset = offset;
    PCRE2_UCHAR buf[256];
    int n = pcre2_get_error_message(code, buf, sizeof(buf) / sizeof(buf[0]));
    if (n > 0) {
        size_t len = (size_t)n;
        if (len >= sizeof(error->message)) {
            len = sizeof(error->message) - 1;
        }
        memcpy(error->message, buf, len);
        error->message[len] = '\0';
    } else {
        error->message[0] = '\0';
    }
}

static uint32_t translate_compile_options(uint32_t options) {
    uint32_t out = 0;
    if (options & P2L_OPT_UTF)               out |= PCRE2_UTF;
    if (options & P2L_OPT_UCP)               out |= PCRE2_UCP;
    if (options & P2L_OPT_CASELESS)          out |= PCRE2_CASELESS;
    if (options & P2L_OPT_MULTILINE)         out |= PCRE2_MULTILINE;
    if (options & P2L_OPT_DOTALL)            out |= PCRE2_DOTALL;
    if (options & P2L_OPT_EXTENDED)          out |= PCRE2_EXTENDED;
    if (options & P2L_OPT_UNGREEDY)          out |= PCRE2_UNGREEDY;
    if (options & P2L_OPT_ANCHORED)          out |= PCRE2_ANCHORED;
    if (options & P2L_OPT_DOLLAR_ENDONLY)    out |= PCRE2_DOLLAR_ENDONLY;
    if (options & P2L_OPT_FIRSTLINE)         out |= PCRE2_FIRSTLINE;
    if (options & P2L_OPT_NO_AUTO_CAPTURE)   out |= PCRE2_NO_AUTO_CAPTURE;
    if (options & P2L_OPT_ENDANCHORED)       out |= PCRE2_ENDANCHORED;
    if (options & P2L_OPT_ALLOW_EMPTY_CLASS) out |= PCRE2_ALLOW_EMPTY_CLASS;
    if (options & P2L_OPT_DUPNAMES)          out |= PCRE2_DUPNAMES;
    if (options & P2L_OPT_NEVER_UCP)         out |= PCRE2_NEVER_UCP;
    return out;
}

static uint32_t translate_match_options(uint32_t options) {
    uint32_t out = 0;
    if (options & P2L_MOPT_ANCHORED)         out |= PCRE2_ANCHORED;
    if (options & P2L_MOPT_NOTBOL)           out |= PCRE2_NOTBOL;
    if (options & P2L_MOPT_NOTEOL)           out |= PCRE2_NOTEOL;
    if (options & P2L_MOPT_NOTEMPTY)         out |= PCRE2_NOTEMPTY;
    if (options & P2L_MOPT_NOTEMPTY_ATSTART) out |= PCRE2_NOTEMPTY_ATSTART;
    if (options & P2L_MOPT_ENDANCHORED)      out |= PCRE2_ENDANCHORED;
    return out;
}

static int map_match_error(int rc, p2l_error *error) {
    fill_error(error, rc, 0);
    if (rc == PCRE2_ERROR_PARTIAL)            return P2L_ERR_PARTIAL;
    if (rc == PCRE2_ERROR_MATCHLIMIT)         return P2L_ERR_MATCHLIMIT;
    if (rc == PCRE2_ERROR_DEPTHLIMIT)         return P2L_ERR_DEPTHLIMIT;
    if (rc == PCRE2_ERROR_NOMEMORY)           return P2L_ERR_NOMEMORY;
    if (rc == PCRE2_ERROR_BADUTFOFFSET)       return P2L_ERR_BADUTF;
    /* The UTF validity errors occupy a contiguous range. */
    if (rc <= PCRE2_ERROR_UTF8_ERR1 && rc >= PCRE2_ERROR_UTF32_ERR2) {
        return P2L_ERR_BADUTF;
    }
    return P2L_ERR_INTERNAL;
}

p2l_regex *p2l_compile(const uint8_t *pattern, size_t pattern_len,
                       uint32_t options, uint32_t match_limit,
                       uint32_t depth_limit, p2l_error *error) {
    clear_error(error);

    if (pattern == NULL && pattern_len > 0) {
        if (error != NULL) {
            error->code = P2L_ERR_NULLARG;
            snprintf(error->message, sizeof(error->message), "pattern is NULL");
        }
        return NULL;
    }

    static const uint8_t empty = 0;
    const uint8_t *pat = (pattern == NULL) ? &empty : pattern;

    int errcode = 0;
    PCRE2_SIZE erroffset = 0;
    pcre2_code *code = pcre2_compile((PCRE2_SPTR)pat, pattern_len,
                                     translate_compile_options(options),
                                     &errcode, &erroffset, NULL);
    if (code == NULL) {
        fill_error(error, errcode, erroffset);
        return NULL;
    }

    p2l_regex *r = (p2l_regex *)calloc(1, sizeof(*r));
    if (r == NULL) {
        pcre2_code_free(code);
        if (error != NULL) {
            error->code = P2L_ERR_NOMEMORY;
            snprintf(error->message, sizeof(error->message), "out of memory");
        }
        return NULL;
    }
    r->code = code;

    uint32_t cc = 0;
    pcre2_pattern_info(code, PCRE2_INFO_CAPTURECOUNT, &cc);
    r->capture_count = cc;
    pcre2_pattern_info(code, PCRE2_INFO_NAMECOUNT, &r->name_count);
    pcre2_pattern_info(code, PCRE2_INFO_NAMEENTRYSIZE, &r->name_entry_size);
    pcre2_pattern_info(code, PCRE2_INFO_NAMETABLE, &r->name_table);
    r->utf = (translate_compile_options(options) & PCRE2_UTF) ? 1 : 0;

    if (match_limit > 0 || depth_limit > 0) {
        r->mcontext = pcre2_match_context_create(NULL);
        if (r->mcontext == NULL) {
            pcre2_code_free(code);
            free(r);
            if (error != NULL) {
                error->code = P2L_ERR_NOMEMORY;
                snprintf(error->message, sizeof(error->message),
                         "out of memory (match context)");
            }
            return NULL;
        }
        if (match_limit > 0) {
            pcre2_set_match_limit(r->mcontext, match_limit);
        }
        if (depth_limit > 0) {
            pcre2_set_depth_limit(r->mcontext, depth_limit);
        }
    }

    return r;
}

void p2l_free(p2l_regex *regex) {
    if (regex == NULL) {
        return;
    }
    if (regex->mcontext != NULL) {
        pcre2_match_context_free(regex->mcontext);
    }
    if (regex->code != NULL) {
        pcre2_code_free(regex->code);
    }
    free(regex);
}

p2l_scratch *p2l_scratch_new(const p2l_regex *regex) {
    if (regex == NULL || regex->code == NULL) {
        return NULL;
    }
    p2l_scratch *s = (p2l_scratch *)calloc(1, sizeof(*s));
    if (s == NULL) {
        return NULL;
    }
    s->md = pcre2_match_data_create_from_pattern(regex->code, NULL);
    if (s->md == NULL) {
        free(s);
        return NULL;
    }
    return s;
}

void p2l_scratch_free(p2l_scratch *scratch) {
    if (scratch == NULL) {
        return;
    }
    if (scratch->md != NULL) {
        pcre2_match_data_free(scratch->md);
    }
    free(scratch);
}

int p2l_match(const p2l_regex *regex, const uint8_t *subject, size_t subject_len,
              size_t start_offset, uint32_t options, p2l_scratch *scratch,
              p2l_span *spans, size_t span_capacity, size_t *span_count,
              p2l_error *error) {
    clear_error(error);
    if (span_count != NULL) {
        *span_count = 0;
    }

    if (regex == NULL || regex->code == NULL) {
        return P2L_ERR_NULLARG;
    }
    if (subject == NULL && subject_len > 0) {
        return P2L_ERR_NULLARG;
    }

    static const uint8_t empty = 0;
    const uint8_t *subj = (subject == NULL) ? &empty : subject;

    pcre2_match_data *md = NULL;
    pcre2_match_data *temp = NULL;
    if (scratch != NULL && scratch->md != NULL) {
        md = scratch->md;
    } else {
        temp = pcre2_match_data_create_from_pattern(regex->code, NULL);
        if (temp == NULL) {
            if (error != NULL) {
                error->code = P2L_ERR_NOMEMORY;
                snprintf(error->message, sizeof(error->message),
                         "out of memory (match data)");
            }
            return P2L_ERR_NOMEMORY;
        }
        md = temp;
    }

    int rc = pcre2_match(regex->code, (PCRE2_SPTR)subj, subject_len,
                         start_offset, translate_match_options(options),
                         md, regex->mcontext);

    int result;
    if (rc >= 0) {
        size_t groups = (size_t)regex->capture_count + 1;
        if (span_count != NULL) {
            *span_count = groups;
        }
        if (spans != NULL) {
            if (span_capacity < groups) {
                result = P2L_ERR_SHORTBUF;
                goto done;
            }
            PCRE2_SIZE *ov = pcre2_get_ovector_pointer(md);
            for (size_t i = 0; i < groups; i++) {
                spans[i].start = (size_t)ov[2 * i];
                spans[i].end = (size_t)ov[2 * i + 1];
            }
        }
        result = (rc > 0) ? rc : (int)groups;
    } else if (rc == PCRE2_ERROR_NOMATCH) {
        result = 0;
    } else {
        result = map_match_error(rc, error);
    }

done:
    if (temp != NULL) {
        pcre2_match_data_free(temp);
    }
    return result;
}

/* Advance one code unit from pos, skipping UTF-8 continuation bytes when the
   pattern is in UTF mode. Mirrors the Go-side empty-match advancement. */
static size_t advance_one(const uint8_t *subject, size_t subject_len,
                          size_t pos, int utf) {
    size_t nb = pos + 1;
    if (utf) {
        while (nb < subject_len && (subject[nb] & 0xC0) == 0x80) {
            nb++;
        }
    }
    return nb;
}

int p2l_match_all(const p2l_regex *regex, const uint8_t *subject,
                  size_t subject_len, size_t start_offset, uint32_t in_options,
                  p2l_scratch *scratch, p2l_span *spans, size_t span_capacity,
                  size_t max_matches, size_t *out_match_count,
                  size_t *out_next_start, uint32_t *out_next_options,
                  p2l_error *error) {
    clear_error(error);
    if (out_match_count != NULL) {
        *out_match_count = 0;
    }
    if (out_next_start != NULL) {
        *out_next_start = subject_len;
    }
    if (out_next_options != NULL) {
        *out_next_options = 0;
    }

    if (regex == NULL || regex->code == NULL) {
        return P2L_ERR_NULLARG;
    }
    if (subject == NULL && subject_len > 0) {
        return P2L_ERR_NULLARG;
    }
    if (spans == NULL || max_matches == 0) {
        return P2L_ERR_NULLARG;
    }

    static const uint8_t empty = 0;
    const uint8_t *subj = (subject == NULL) ? &empty : subject;

    /* The batched iterator requires its own match data; the caller's pooled
       scratch supplies it (a temporary is created if absent). */
    pcre2_match_data *md = NULL;
    pcre2_match_data *temp = NULL;
    if (scratch != NULL && scratch->md != NULL) {
        md = scratch->md;
    } else {
        temp = pcre2_match_data_create_from_pattern(regex->code, NULL);
        if (temp == NULL) {
            if (error != NULL) {
                error->code = P2L_ERR_NOMEMORY;
                snprintf(error->message, sizeof(error->message),
                         "out of memory (match data)");
            }
            return P2L_ERR_NOMEMORY;
        }
        md = temp;
    }

    const size_t stride = (size_t)regex->capture_count + 1;
    size_t count = 0;
    size_t pos = start_offset;
    uint32_t opts = in_options;
    int rc_more = 0; /* 0 = exhausted, 1 = buffer full (resume) */

    /* In UTF mode PCRE2 validates the whole subject on every pcre2_match call
       unless PCRE2_NO_UTF_CHECK is set. Validating once per call and skipping the
       check on subsequent iterations turns an O(n*matches) cost into O(n),
       which is the dominant win for many small matches over a large subject. */
    const int utf = regex->utf;
    int validated = 0;

    for (;;) {
        if (count >= max_matches || (count + 1) * stride > span_capacity) {
            /* Buffer full: hand back the resume cursor and options. */
            if (out_next_start != NULL) {
                *out_next_start = pos;
            }
            if (out_next_options != NULL) {
                *out_next_options = opts;
            }
            rc_more = 1;
            break;
        }

        uint32_t mopts = translate_match_options(opts);
        if (utf && validated) {
            mopts |= PCRE2_NO_UTF_CHECK;
        }
        int rc = pcre2_match(regex->code, (PCRE2_SPTR)subj, subject_len, pos,
                             mopts, md, regex->mcontext);
        validated = 1; /* subject proven valid (or rejected) by the first call */

        if (rc == PCRE2_ERROR_NOMATCH) {
            if (opts == 0) {
                break; /* genuinely no more matches */
            }
            /* Empty-anchored retry failed: advance one code unit, plain search. */
            pos = advance_one(subj, subject_len, pos, regex->utf);
            opts = 0;
            if (pos > subject_len) {
                break;
            }
            continue;
        }
        if (rc < 0) {
            int mapped = map_match_error(rc, error);
            if (out_match_count != NULL) {
                *out_match_count = count;
            }
            if (temp != NULL) {
                pcre2_match_data_free(temp);
            }
            return mapped;
        }

        PCRE2_SIZE *ov = pcre2_get_ovector_pointer(md);
        p2l_span *dst = spans + count * stride;
        for (size_t i = 0; i < stride; i++) {
            dst[i].start = (size_t)ov[2 * i];
            dst[i].end = (size_t)ov[2 * i + 1];
        }
        count++;

        size_t s0 = (size_t)ov[0];
        size_t e0 = (size_t)ov[1];
        pos = e0;
        opts = 0;
        if (s0 == e0) { /* empty match: avoid an infinite loop at the same spot */
            if (e0 >= subject_len) {
                break;
            }
            opts = P2L_MOPT_NOTEMPTY_ATSTART | P2L_MOPT_ANCHORED;
        }
    }

    if (out_match_count != NULL) {
        *out_match_count = count;
    }
    if (temp != NULL) {
        pcre2_match_data_free(temp);
    }
    return rc_more;
}

size_t p2l_capture_count(const p2l_regex *regex) {
    if (regex == NULL) {
        return 0;
    }
    return (size_t)regex->capture_count + 1; /* include group 0 */
}

size_t p2l_name_count(const p2l_regex *regex) {
    if (regex == NULL) {
        return 0;
    }
    return (size_t)regex->name_count;
}

int p2l_group_name(const p2l_regex *regex, size_t index,
                   char *name_buf, size_t name_buf_len, uint32_t *out_number) {
    if (regex == NULL) {
        return P2L_ERR_NULLARG;
    }
    if (index >= regex->name_count) {
        return P2L_ERR_BADOPTION;
    }
    PCRE2_SPTR entry = regex->name_table + index * regex->name_entry_size;
    /* For the 8-bit library the group number occupies the first two code units
       (bytes), most significant first, followed by the NUL-terminated name. */
    uint32_t num = ((uint32_t)entry[0] << 8) | (uint32_t)entry[1];
    const char *name = (const char *)(entry + 2);
    size_t len = strlen(name);
    if (out_number != NULL) {
        *out_number = num;
    }
    if (name_buf != NULL && name_buf_len > 0) {
        size_t n = (len < name_buf_len - 1) ? len : name_buf_len - 1;
        memcpy(name_buf, name, n);
        name_buf[n] = '\0';
    }
    return (int)len;
}

const char *p2l_version(void) {
    return "PCRE2-Lite (PCRE2 "
           P2L_STRINGIFY(PCRE2_MAJOR) "." P2L_STRINGIFY(PCRE2_MINOR)
           ", 8-bit, JIT disabled)";
}
